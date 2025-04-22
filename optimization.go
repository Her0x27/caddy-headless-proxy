package headlessproxy

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/json"
	"github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
)

// ResourceOptimizer handles resource optimization
type ResourceOptimizer struct {
	minifier *minify.M
	proxy    *HeadlessProxy
}

// NewResourceOptimizer creates a new resource optimizer
func NewResourceOptimizer(proxy *HeadlessProxy) *ResourceOptimizer {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("text/javascript", js.Minify)
	m.AddFunc("application/javascript", js.Minify)
	m.AddFunc("application/json", json.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)
	m.AddFuncRegexp(regexp.MustCompile("^text/x-[a-z]+$"), html.Minify)
	m.AddFuncRegexp(regexp.MustCompile("^application/xml$"), xml.Minify)

	return &ResourceOptimizer{
		minifier: m,
		proxy:    proxy,
	}
}

// OptimizePage optimizes the page content
func (o *ResourceOptimizer) OptimizePage(page *rod.Page) error {
	startTime := time.Now()

	// Get original page size
	originalHTML, err := page.HTML()
	if err != nil {
		return fmt.Errorf("failed to get original HTML: %v", err)
	}
	originalSize := len(originalHTML)

	// Remove unnecessary elements and scripts
	optimizeScript := `
	(() => {
		// Remove unnecessary elements
		const removeSelectors = [
			'script:not([type="application/ld+json"]):not([type="application/json"]):not([type="text/javascript"])',
			'link[rel="preload"]',
			'style:empty',
			'link[rel="prefetch"]',
			'meta[name="robots"]',
			'meta[name="googlebot"]',
			'meta[name="generator"]',
			'[style="display:none"]:not(noscript)',
			'[hidden]',
			'iframe[style*="display: none"]',
		];

		// Remove elements by selector
		let removedElements = 0;
		removeSelectors.forEach(selector => {
			try {
				document.querySelectorAll(selector).forEach(el => {
					el.remove();
					removedElements++;
				});
			} catch (e) {
				// Ignore errors
			}
		});

		// Remove HTML comments
		const removeComments = (node) => {
			const childNodes = Array.from(node.childNodes);
			childNodes.forEach(child => {
				if (child.nodeType === 8) { // Comment node
					node.removeChild(child);
										removedElements++;
				} else if (child.nodeType === 1) { // Element node
					removeComments(child);
				}
			});
		};
		removeComments(document);

		// Minify inline CSS
		document.querySelectorAll('style').forEach(style => {
			if (style.textContent) {
				const original = style.textContent;
				style.textContent = style.textContent
					.replace(/\s+/g, ' ')
					.replace(/\/\*[\s\S]*?\*\//g, '')
					.replace(/\s*([{}:;,])\s*/g, '$1')
					.replace(/;\s*}/g, '}');
				
				if (original.length > style.textContent.length) {
					removedElements++;
				}
			}
		});

		// Minify inline attributes
		document.querySelectorAll('[style]').forEach(el => {
			if (el.getAttribute('style')) {
				const original = el.getAttribute('style');
				const minified = el.getAttribute('style')
					.replace(/\s+/g, ' ')
					.replace(/:\s+/g, ':')
					.replace(/;\s+/g, ';')
					.trim();
				
				el.setAttribute('style', minified);
				
				if (original.length > minified.length) {
					removedElements++;
				}
			}
		});

		// Remove data-* attributes except for a few important ones
		document.querySelectorAll('*').forEach(el => {
			Array.from(el.attributes).forEach(attr => {
				if (attr.name.startsWith('data-') && 
					!['data-id', 'data-src', 'data-href', 'data-url', 'data-target'].includes(attr.name)) {
					el.removeAttribute(attr.name);
					removedElements++;
				}
			});
		});

		// Optimize images if enabled
		const optimizeImages = ${o.proxy.CompressImages};
		if (optimizeImages) {
			document.querySelectorAll('img').forEach(img => {
				// Add loading="lazy" to images
				if (!img.hasAttribute('loading')) {
					img.setAttribute('loading', 'lazy');
				}
				
				// Remove srcset for simplicity
				if (img.hasAttribute('srcset')) {
					img.removeAttribute('srcset');
					removedElements++;
				}
				
				// Ensure alt attribute exists
				if (!img.hasAttribute('alt')) {
					img.setAttribute('alt', '');
				}
				
				// Add width and height if missing to prevent layout shifts
				if (!img.hasAttribute('width') && img.width > 0) {
					img.setAttribute('width', img.width.toString());
				}
				if (!img.hasAttribute('height') && img.height > 0) {
					img.setAttribute('height', img.height.toString());
				}
			});
		}

		// Optimize links
		document.querySelectorAll('a').forEach(link => {
			// Remove unnecessary rel attributes
			if (link.getAttribute('rel') === 'noopener noreferrer' && 
				(!link.target || link.target !== '_blank')) {
				link.removeAttribute('rel');
				removedElements++;
			}
		});

		// Optimize meta tags
		const metaElements = document.querySelectorAll('meta');
		const metaNames = new Set();
		metaElements.forEach(meta => {
			const name = meta.getAttribute('name');
			if (name) {
				if (metaNames.has(name)) {
					// Remove duplicate meta tags
					meta.remove();
					removedElements++;
				} else {
					metaNames.add(name);
				}
			}
		});

		return {
			removedElements: removedElements
		};
	})();
	`

	var result map[string]interface{}
	err = page.Eval(optimizeScript).Unmarshal(&result)
	if err != nil {
		return fmt.Errorf("failed to execute optimization script: %v", err)
	}

	// Get optimized HTML
	optimizedHTML, err := page.HTML()
	if err != nil {
		return fmt.Errorf("failed to get optimized HTML: %v", err)
	}

	// Minify HTML if enabled
	if o.proxy.MinifyContent {
		var minifiedHTML bytes.Buffer
		err = o.minifier.Minify("text/html", &minifiedHTML, strings.NewReader(optimizedHTML))
		if err != nil {
			o.proxy.logger.Warn("failed to minify HTML", zap.Error(err))
		} else {
			// Inject the minified HTML back into the page
			injectScript := fmt.Sprintf(`
				document.open();
				document.write(%s);
				document.close();
			`, toJSONString(minifiedHTML.String()))
			
			err = page.Eval(injectScript).Err()
			if err != nil {
				return fmt.Errorf("failed to inject minified HTML: %v", err)
			}
			
			// Get final HTML
			optimizedHTML, err = page.HTML()
			if err != nil {
				return fmt.Errorf("failed to get final HTML: %v", err)
			}
		}
	}

	// Calculate savings
	optimizedSize := len(optimizedHTML)
	savings := originalSize - optimizedSize
	
	if savings > 0 {
		o.proxy.metrics.optimizationSavings.Add(float64(savings))
		o.proxy.logger.Info("page optimized",
			zap.Int("original_size", originalSize),
			zap.Int("optimized_size", optimizedSize),
			zap.Int("savings", savings),
			zap.Float64("savings_percent", float64(savings)/float64(originalSize)*100),
			zap.Duration("optimization_time", time.Since(startTime)),
		)
	}

	return nil
}

// OptimizeResponse optimizes a response based on content type
func (o *ResourceOptimizer) OptimizeResponse(contentType string, content []byte) ([]byte, error) {
	if !o.proxy.MinifyContent {
		return content, nil
	}

	// Skip binary content
	if !isTextContentType(contentType) {
		return content, nil
	}

	var output bytes.Buffer
	r := bytes.NewReader(content)
	err := o.minifier.Minify(contentType, &output, r)
	if err != nil {
		return content, err
	}

	savings := len(content) - output.Len()
	if savings > 0 {
		o.proxy.metrics.optimizationSavings.Add(float64(savings))
	}

	return output.Bytes(), nil
}

// isTextContentType checks if a content type is text-based
func isTextContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "javascript") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "html") ||
		strings.Contains(contentType, "css")
}

     
