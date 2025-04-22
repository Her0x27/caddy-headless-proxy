package headlessproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/rod/lib/utils"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(HeadlessProxy{})
	httpcaddyfile.RegisterHandlerDirective("headless_proxy", parseCaddyfile)
}

// HeadlessProxy implements a reverse proxy that uses a headless browser
// to fetch and process content from the target server.
type HeadlessProxy struct {
	// The URL to proxy to
	Upstream string `json:"upstream,omitempty"`

	// Timeout for browser operations in seconds
	Timeout int `json:"timeout,omitempty"`

	// UserAgent to use for the headless browser
	UserAgent string `json:"user_agent,omitempty"`

	// Whether to enable JavaScript
	EnableJS bool `json:"enable_js,omitempty"`

	// Whether to forward cookies
	ForwardCookies bool `json:"forward_cookies,omitempty"`

	// Headers to forward to the target
	ForwardHeaders []string `json:"forward_headers,omitempty"`

	// Cache TTL in seconds (0 means no caching)
	CacheTTL int `json:"cache_ttl,omitempty"`

	// Maximum browser instances to keep in the pool
	MaxBrowsers int `json:"max_browsers,omitempty"`

	// Resource optimization options
	OptimizeResources bool `json:"optimize_resources,omitempty"`

	// Whether to compress images
	CompressImages bool `json:"compress_images,omitempty"`

	// Whether to minify HTML, CSS, and JS
	MinifyContent bool `json:"minify_content,omitempty"`

	// Browser pool
	browserPool     []*rod.Browser
	browserPoolLock sync.Mutex

	// Cache for responses
	cache     map[string]cacheEntry
	cacheLock sync.RWMutex

	logger *zap.Logger
}

// cacheEntry represents a cached response
type cacheEntry struct {
	Content    []byte
	Headers    http.Header
	StatusCode int
	Expiration time.Time
}

// CaddyModule returns the Caddy module information.
func (HeadlessProxy) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.headless_proxy",
		New: func() caddy.Module { return new(HeadlessProxy) },
	}
}

// Provision sets up the module.
func (h *HeadlessProxy) Provision(ctx caddy.Context) error {
	// Set default values
	if h.Timeout <= 0 {
		h.Timeout = 30
	}

	if h.UserAgent == "" {
		h.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	}

	// Enable JS by default
	if !h.EnableJS {
		h.EnableJS = true
	}

	// Set default max browsers
	if h.MaxBrowsers <= 0 {
		h.MaxBrowsers = 5
	}

	// Initialize cache if caching is enabled
	if h.CacheTTL > 0 {
		h.cache = make(map[string]cacheEntry)
	}

	// Get a logger
	h.logger = ctx.Logger().Named("headless_proxy").With(
		zap.String("upstream", h.Upstream),
	)

	// Initialize browser pool
	h.browserPool = make([]*rod.Browser, 0, h.MaxBrowsers)
	h.initBrowserPool()

	h.logger.Info("headless proxy module initialized",
		zap.Int("max_browsers", h.MaxBrowsers),
		zap.Int("cache_ttl", h.CacheTTL),
		zap.Bool("optimize_resources", h.OptimizeResources),
	)
	return nil
}

// initBrowserPool initializes the browser pool with one browser
func (h *HeadlessProxy) initBrowserPool() {
	// Start with one browser in the pool
	browser := h.createBrowser()
	if browser != nil {
		h.browserPool = append(h.browserPool, browser)
		h.logger.Info("initial browser added to pool")
	}
}

// createBrowser creates a new browser instance
func (h *HeadlessProxy) createBrowser() *rod.Browser {
	launcherURL := launcher.New().
		Headless(true).
		NoSandbox(true).
		Devtools(false).
		Env("--disable-gpu").
		Env("--disable-dev-shm-usage").
		Env("--disable-setuid-sandbox").
		Env("--no-first-run").
		Env("--no-zygote").
		Env("--single-process").
		Env("--disable-extensions").
		MustLaunch()

	browser := rod.New().
		ControlURL(launcherURL).
		Timeout(time.Duration(h.Timeout) * time.Second)

	err := browser.Connect()
	if err != nil {
		h.logger.Error("failed to connect to browser", zap.Error(err))
		return nil
	}

	return browser
}

// getBrowser gets a browser from the pool or creates a new one if needed
func (h *HeadlessProxy) getBrowser() *rod.Browser {
	h.browserPoolLock.Lock()
	defer h.browserPoolLock.Unlock()

	// If there are browsers in the pool, use one
	if len(h.browserPool) > 0 {
		browser := h.browserPool[len(h.browserPool)-1]
		h.browserPool = h.browserPool[:len(h.browserPool)-1]
		return browser
	}

	// Otherwise create a new browser
	return h.createBrowser()
}

// returnBrowser returns a browser to the pool or closes it if the pool is full
func (h *HeadlessProxy) returnBrowser(browser *rod.Browser) {
	h.browserPoolLock.Lock()
	defer h.browserPoolLock.Unlock()

	// If the pool is full, close the browser
	if len(h.browserPool) >= h.MaxBrowsers {
		go func() {
			err := browser.Close()
			if err != nil {
				h.logger.Error("failed to close browser", zap.Error(err))
			}
		}()
		return
	}

	// Otherwise, return it to the pool
	h.browserPool = append(h.browserPool, browser)
}

// Cleanup cleans up the module's resources.
func (h *HeadlessProxy) Cleanup() error {
	h.browserPoolLock.Lock()
	defer h.browserPoolLock.Unlock()

	// Close all browsers in the pool
	for _, browser := range h.browserPool {
		err := browser.Close()
		if err != nil {
			h.logger.Error("failed to close browser during cleanup", zap.Error(err))
		}
	}

	h.browserPool = nil
	h.logger.Info("all browsers closed, cleanup complete")
	return nil
}

// Validate ensures the module's configuration is valid.
func (h *HeadlessProxy) Validate() error {
	if h.Upstream == "" {
		return fmt.Errorf("upstream URL is required")
	}

	// Validate upstream URL
	_, err := url.Parse(h.Upstream)
	if err != nil {
		return fmt.Errorf("invalid upstream URL: %v", err)
	}

	return nil
}

// getCacheKey generates a cache key for a request
func (h *HeadlessProxy) getCacheKey(r *http.Request) string {
	// Create a hash of the request details
	hasher := sha256.New()
	hasher.Write([]byte(r.Method))
	hasher.Write([]byte(r.URL.Path))
	hasher.Write([]byte(r.URL.RawQuery))

	// Include relevant headers in the cache key
	for _, header := range h.ForwardHeaders {
		if value := r.Header.Get(header); value != "" {
			hasher.Write([]byte(header + ":" + value))
		}
	}

	// Include cookies if they're being forwarded
	if h.ForwardCookies {
		for _, cookie := range r.Cookies() {
			hasher.Write([]byte(cookie.Name + "=" + cookie.Value))
		}
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// getCachedResponse retrieves a cached response if available
func (h *HeadlessProxy) getCachedResponse(r *http.Request) ([]byte, http.Header, int, bool) {
	if h.CacheTTL <= 0 {
		return nil, nil, 0, false
	}

	key := h.getCacheKey(r)
	h.cacheLock.RLock()
	defer h.cacheLock.RUnlock()

	if entry, ok := h.cache[key]; ok {
		if time.Now().Before(entry.Expiration) {
			return entry.Content, entry.Headers, entry.StatusCode, true
		}
		// Remove expired entry
		delete(h.cache, key)
	}
	return nil, nil, 0, false
}

// setCachedResponse caches a response
func (h *HeadlessProxy) setCachedResponse(r *http.Request, content []byte, headers http.Header, statusCode int) {
	if h.CacheTTL <= 0 {
		return
	}

	key := h.getCacheKey(r)
	h.cacheLock.Lock()
	defer h.cacheLock.Unlock()

	// Clean up old entries if cache is getting too large (more than 1000 entries)
	if len(h.cache) > 1000 {
		now := time.Now()
		for k, v := range h.cache {
			if now.After(v.Expiration) {
				delete(h.cache, k)
			}
		}
	}

	h.cache[key] = cacheEntry{
		Content:    content,
		Headers:    headers,
		StatusCode: statusCode,
		Expiration: time.Now().Add(time.Duration(h.CacheTTL) * time.Second),
	}
}

// ServeHTTP implements the caddyhttp.MiddlewareHandler interface.
func (h *HeadlessProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	requestStart := time.Now()

	// Check cache first
	if content, headers, statusCode, found := h.getCachedResponse(r); found {
		h.logger.Info("serving cached response",
			zap.String("path", r.URL.Path),
			zap.Int("status", statusCode),
			zap.Duration("response_time", time.Since(requestStart)),
		)

		for key, values := range headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.WriteHeader(statusCode)
		_, err := w.Write(content)
		return err
	}

	// Get a browser from the pool
	browser := h.getBrowser()
	if browser == nil {
		return fmt.Errorf("failed to get browser from pool")
	}

	// Make sure to return the browser to the pool when done
	defer h.returnBrowser(browser)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.Timeout)*time.Second)
	defer cancel()

	// Create the target URL by combining the upstream with the request path
	targetURL := h.Upstream
	if !strings.HasSuffix(targetURL, "/") && !strings.HasPrefix(r.URL.Path, "/") {
		targetURL += "/"
	}
	targetURL += r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	h.logger.Info("proxying request",
		zap.String("method", r.Method),
		zap.String("url", targetURL),
	)

	// Create a new browser page
	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return fmt.Errorf("failed to create page: %v", err)
	}
	defer func() {
		err := page.Close()
		if err != nil {
			h.logger.Error("failed to close page", zap.Error(err))
		}
	}()

	// Set user agent
	err = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: h.UserAgent,
	})
	if err != nil {
		return fmt.Errorf("failed to set user agent: %v", err)
	}

	// Disable JavaScript if needed
	if !h.EnableJS {
		err = page.EvalOnNewDocument(`
			Object.defineProperty(window, 'navigator', {
				value: new Proxy(navigator, {
					has: (target, key) => (key === 'plugins' || key === 'languages') ? false : key in target,
					get: (target, key) => 
						key === 'plugins' ? { length: 0 } : 
						key === 'languages' ? ['en-US'] : 
						key === 'userAgent' ? '` + h.UserAgent + `' : 
						target[key]
				})
			});
		`)
		if err != nil {
			return fmt.Errorf("failed to set JavaScript settings: %v", err)
		}
	}

	// Forward cookies if enabled
	if h.ForwardCookies {
		cookies := r.Cookies()
		for _, cookie := range cookies {
			err = page.SetCookies(&proto.NetworkCookieParam{
				Name:   cookie.Name,
				Value:  cookie.Value,
				Domain: cookie.Domain,
				Path:   cookie.Path,
			})
			if err != nil {
				h.logger.Error("failed to set cookie", zap.Error(err))
			}
		}
	}

	// Set up response capture
	var responseStatusCode int = http.StatusOK
	responseHeaders := make(http.Header)
	var responseContent []byte

	// Handle different HTTP methods
	switch r.Method {
	case http.MethodGet:
		// Navigate to the page
		router := page.HijackRequests()
		defer router.Stop()

		// Intercept requests to modify headers
				// Intercept requests to modify headers
		router.MustAdd("*", func(ctx *rod.Hijack) {
			// Add forwarded headers
			for _, header := range h.ForwardHeaders {
				if value := r.Header.Get(header); value != "" {
					ctx.Request.SetHeader(header, value)
				}
			}
			
			// Continue with the request
			ctx.ContinueRequest(&proto.FetchContinueRequest{})
		})
		
		go router.Run()

		// Navigate to the page
		err = page.Context(ctx).Navigate(targetURL)
		if err != nil {
			return fmt.Errorf("failed to navigate to %s: %v", targetURL, err)
		}

		// Wait for the page to load
		err = page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
		if err != nil {
			return fmt.Errorf("failed to wait for navigation: %v", err)
		}

		// Wait for network to be idle
		err = page.WaitIdle(time.Second * 2)
		if err != nil {
			h.logger.Warn("timeout waiting for network idle", zap.Error(err))
		}

		// Get the page content
		if h.OptimizeResources {
			// Optimize the page content
			err = h.optimizePage(page)
			if err != nil {
				h.logger.Error("failed to optimize page", zap.Error(err))
			}
		}

		// Get the final HTML content
		content, err := page.HTML()
		if err != nil {
			return fmt.Errorf("failed to get page HTML: %v", err)
		}
		responseContent = []byte(content)

		// Set content type header
		responseHeaders.Set("Content-Type", "text/html; charset=utf-8")

	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %v", err)
		}

		// Prepare headers
		headers := map[string]string{
			"Content-Type": r.Header.Get("Content-Type"),
		}

		// Add forwarded headers
		for _, header := range h.ForwardHeaders {
			if value := r.Header.Get(header); value != "" {
				headers[header] = value
			}
		}

		// Execute fetch API to make the request
		fetchScript := fmt.Sprintf(`
			(async () => {
				try {
					const response = await fetch('%s', {
						method: '%s',
						headers: %s,
						body: %s,
						credentials: 'include',
						redirect: 'follow'
					});
					
					const text = await response.text();
					const headers = {};
					response.headers.forEach((value, key) => {
						headers[key] = value;
					});
					
					return {
						status: response.status,
						statusText: response.statusText,
						headers: headers,
						body: text
					};
				} catch (error) {
					return {
						error: error.toString(),
						status: 500
					};
				}
			})()
		`, targetURL, r.Method, toJSONString(headers), toJSONString(string(body)))

		var result map[string]interface{}
		err = page.Eval(fetchScript).Unmarshal(&result)
		if err != nil {
			return fmt.Errorf("failed to execute fetch: %v", err)
		}

		// Check for errors
		if errorMsg, ok := result["error"].(string); ok {
			h.logger.Error("fetch API error", zap.String("error", errorMsg))
			w.WriteHeader(http.StatusBadGateway)
			_, err = w.Write([]byte("Error communicating with upstream server"))
			return err
		}

		// Set response headers
		if headersMap, ok := result["headers"].(map[string]interface{}); ok {
			for key, value := range headersMap {
				responseHeaders.Set(key, fmt.Sprintf("%v", value))
			}
		}

		// Set status code
		if status, ok := result["status"].(float64); ok {
			responseStatusCode = int(status)
		}

		// Set response body
		if body, ok := result["body"].(string); ok {
			responseContent = []byte(body)
		}

	default:
		// For other methods, return method not allowed
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, err := w.Write([]byte("Method not allowed"))
		return err
	}

	// Get cookies from the page and set them in the response
	if h.ForwardCookies {
		pageCookies, err := page.Cookies([]string{})
		if err == nil {
			for _, cookie := range pageCookies {
				cookieStr := fmt.Sprintf("%s=%s", cookie.Name, cookie.Value)
				if cookie.Path != "" {
					cookieStr += "; Path=" + cookie.Path
				}
				if cookie.Domain != "" {
					cookieStr += "; Domain=" + cookie.Domain
				}
				if cookie.Expires != 0 {
					expTime := time.Unix(int64(cookie.Expires), 0)
					cookieStr += "; Expires=" + expTime.Format(time.RFC1123)
				}
				if cookie.Secure {
					cookieStr += "; Secure"
				}
				if cookie.HTTPOnly {
					cookieStr += "; HttpOnly"
				}
				responseHeaders.Add("Set-Cookie", cookieStr)
			}
		}
	}

	// Cache the response
	h.setCachedResponse(r, responseContent, responseHeaders, responseStatusCode)

	// Set headers in the response
	for key, values := range responseHeaders {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(responseStatusCode)

	// Write the content to the response
	_, err = w.Write(responseContent)
	if err != nil {
		return fmt.Errorf("failed to write response: %v", err)
	}

	h.logger.Info("request completed",
		zap.Int("status", responseStatusCode),
		zap.Int("content_length", len(responseContent)),
		zap.Duration("response_time", time.Since(requestStart)),
	)

	return nil
}

// optimizePage optimizes the page content by removing unnecessary elements and minifying content
func (h *HeadlessProxy) optimizePage(page *rod.Page) error {
	// Remove unnecessary elements and scripts
	optimizeScript := `
	(() => {
		// Remove unnecessary elements
		const removeSelectors = [
			'script:not([type="application/ld+json"])',  // Remove most scripts
			'link[rel="preload"]',                       // Remove preload links
			'style:empty',                               // Remove empty style tags
			'link[rel="prefetch"]',                      // Remove prefetch links
			'meta[name="robots"]',                       // Remove robots meta
			'meta[name="googlebot"]',                    // Remove googlebot meta
			'meta[name="generator"]',                    // Remove generator meta
			'[style="display:none"]:not(noscript)',      // Remove hidden elements
			'[hidden]',                                  // Remove hidden elements
			'iframe[style*="display: none"]',            // Remove hidden iframes
			'comment()',                                 // Remove HTML comments
		];

		// Remove elements by selector
		removeSelectors.forEach(selector => {
			try {
				document.querySelectorAll(selector).forEach(el => el.remove());
			} catch (e) {
				// Ignore errors for comment() selector
			}
		});

		// Minify inline CSS
		document.querySelectorAll('style').forEach(style => {
			if (style.textContent) {
				style.textContent = style.textContent
					.replace(/\s+/g, ' ')
					.replace(/\/\*[\s\S]*?\*\//g, '')
					.replace(/\s*([{}:;,])\s*/g, '$1')
					.replace(/;\s*}/g, '}');
			}
		});

		// Minify inline attributes
		document.querySelectorAll('[style]').forEach(el => {
			if (el.getAttribute('style')) {
				el.setAttribute('style', 
					el.getAttribute('style')
						.replace(/\s+/g, ' ')
						.replace(/:\s+/g, ':')
						.replace(/;\s+/g, ';')
						.trim()
				);
			}
		});

		// Remove data-* attributes except for a few important ones
		document.querySelectorAll('*').forEach(el => {
			Array.from(el.attributes).forEach(attr => {
				if (attr.name.startsWith('data-') && 
					!['data-id', 'data-src', 'data-href'].includes(attr.name)) {
					el.removeAttribute(attr.name);
				}
			});
		});

		// Optimize images if enabled
		const optimizeImages = ${h.CompressImages};
		if (optimizeImages) {
			document.querySelectorAll('img').forEach(img => {
				// Add loading="lazy" to images
				if (!img.hasAttribute('loading')) {
					img.setAttribute('loading', 'lazy');
				}
				
				// Remove srcset for simplicity
				img.removeAttribute('srcset');
				
				// Ensure alt attribute exists
				if (!img.hasAttribute('alt')) {
					img.setAttribute('alt', '');
				}
			});
		}

		return true;
	})();
	`

	_, err := page.Eval(optimizeScript)
	return err
}

// Helper function to convert a value to a JSON string
func toJSONString(v interface{}) string {
	bytes, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(bytes)
}

// parseCaddyfile parses the Caddyfile directive.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var hp HeadlessProxy

	for h.Next() {
		if !h.NextArg() {
			return nil, h.ArgErr()
		}
		hp.Upstream = h.Val()

		if h.NextArg() {
			return nil, h.ArgErr()
		}

		for h.NextBlock(0) {
			switch h.Val() {
			case "timeout":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.Timeout, err = h.IntVal()
				if err != nil {
					return nil, h.Errf("invalid timeout value: %v", err)
				}

			case "user_agent":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				hp.UserAgent = h.Val()

			case "enable_js":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.EnableJS, err = h.BoolVal()
				if err != nil {
					return nil, h.Errf("invalid enable_js value: %v", err)
				}

			case "forward_cookies":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.ForwardCookies, err = h.BoolVal()
				if err != nil {
					return nil, h.Errf("invalid forward_cookies value: %v", err)
				}

			case "forward_headers":
				var headers []string
				for h.NextArg() {
					headers = append(headers, h.Val())
				}
				hp.ForwardHeaders = headers

			case "cache_ttl":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.CacheTTL, err = h.IntVal()
				if err != nil {
					return nil, h.Errf("invalid cache_ttl value: %v", err)
				}

			case "max_browsers":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.MaxBrowsers, err = h.IntVal()
				if err != nil {
					return nil, h.Errf("invalid max_browsers value: %v", err)
				}

			case "optimize_resources":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.OptimizeResources, err = h.BoolVal()
				if err != nil {
					return nil, h.Errf("invalid optimize_resources value: %v", err)
				}

			case "compress_images":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.CompressImages, err = h.BoolVal()
				if err != nil {
					return nil, h.Errf("invalid compress_images value: %v", err)
				}

			case "minify_content":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				var err error
				hp.MinifyContent, err = h.BoolVal()
				if err != nil {
					return nil, h.Errf("invalid minify_content value: %v", err)
				}

			default:
				return nil, h.Errf("unknown subdirective: %s", h.Val())
			}
		}
	}

	return &hp, nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (h *HeadlessProxy) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if !d.NextArg() {
			return d.ArgErr()
		}
		h.Upstream = d.Val()

		if d.NextArg() {
			return d.ArgErr()
		}

		for d.NextBlock(0) {
			switch d.Val() {
			case "timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.Timeout, err = parseInt(d.Val())
				if err != nil {
					return fmt.Errorf("invalid timeout value: %v", err)
				}

			case "user_agent":
				if !d.NextArg() {
					return d.ArgErr()
				}
				h.UserAgent = d.Val()

			case "enable_js":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.EnableJS, err = parseBool(d.Val())
				if err != nil {
					return fmt.Errorf("invalid enable_js value: %v", err)
				}

			case "forward_cookies":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.ForwardCookies, err = parseBool(d.Val())
				if err != nil {
					return fmt.Errorf("invalid forward_cookies value: %v", err)
				}

			case "forward_headers":
				var headers []string
				for d.NextArg() {
					headers = append(headers, d.Val())
				}
				h.ForwardHeaders = headers

			case "cache_ttl":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.CacheTTL, err = parseInt(d.Val())
				if err != nil {
					return fmt.Errorf("invalid cache_ttl value: %v", err)
				}

			case "max_browsers":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.MaxBrowsers, err = parseInt(d.Val())
				if err != nil {
					return fmt.Errorf("invalid max_browsers value: %v", err)
				}

			case "optimize_resources":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.OptimizeResources, err = parseBool(d.Val())
				if err != nil {
					return fmt.Errorf("invalid optimize_resources value: %v", err)
				}

			case "compress_images":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.CompressImages, err = parseBool(d.Val())
				if err != nil {
					return fmt.Errorf("invalid compress_images value: %v", err)
				}

			case "minify_content":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				h.MinifyContent, err = parseBool(d.Val())
				if err != nil {
					return fmt.Errorf("invalid minify_content value: %v", err)
				}

			default:
				return fmt.Errorf("unknown subdirective: %s", d.Val())
			}
		}
	}
	return nil
}

// Helper function to parse int
func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// Helper function to parse bool
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s", s)
	}
}

// Interface guards
var (
	_ caddy.Provisioner           = (*HeadlessProxy)(nil)
	_ caddy.Validator             = (*HeadlessProxy)(nil)
	_ caddyhttp.MiddlewareHandler = (*HeadlessProxy)(nil)
	_ caddyfile.Unmarshaler       = (*HeadlessProxy)(nil)
)
			//
      
