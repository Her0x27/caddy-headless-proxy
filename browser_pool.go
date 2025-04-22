package headlessproxy

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// cacheEntry represents a cached response
type cacheEntry struct {
	Content    []byte
	Headers    http.Header
	StatusCode int
	Expires    time.Time
}

// getCacheKey generates a cache key for a request
func (h *HeadlessProxy) getCacheKey(r *http.Request) string {
	// Only cache GET requests
	if r.Method != http.MethodGet {
		return ""
	}

	// Create a unique key based on the URL and selected headers
	key := r.URL.String()

	// Add important headers to the cache key
	headerKeys := []string{"Accept-Language", "User-Agent"}
	for _, headerKey := range headerKeys {
		if value := r.Header.Get(headerKey); value != "" {
			key += "|" + headerKey + ":" + value
		}
	}

	// Add cookies to the cache key if forwarding cookies
	if h.ForwardCookies {
		cookies := []string{}
		for _, cookie := range r.Cookies() {
			cookies = append(cookies, cookie.Name+"="+cookie.Value)
		}
		if len(cookies) > 0 {
			key += "|cookies:" + strings.Join(cookies, "&")
		}
	}

	// Hash the key to make it a reasonable length
	hasher := md5.New()
	hasher.Write([]byte(key))
	return hex.EncodeToString(hasher.Sum(nil))
}

// getCachedResponse gets a cached response if available
func (h *HeadlessProxy) getCachedResponse(r *http.Request) ([]byte, http.Header, int, bool) {
	// Skip cache if disabled
	if h.CacheTTL <= 0 {
		return nil, nil, 0, false
	}

	// Get cache key
	key := h.getCacheKey(r)
	if key == "" {
		return nil, nil, 0, false
	}

	h.cacheLock.RLock()
	defer h.cacheLock.RUnlock()

	// Check if entry exists and is not expired
	entry, ok := h.cache[key]
	if !ok || time.Now().After(entry.Expires) {
		if ok {
			// Entry exists but is expired
			h.logger.Debug("cache entry expired", 
				zap.String("key", key),
				zap.Time("expires", entry.Expires))
		}
		return nil, nil, 0, false
	}

	h.logger.Debug("cache hit", 
		zap.String("key", key),
		zap.Int("content_length", len(entry.Content)),
		zap.Time("expires", entry.Expires))

	return entry.Content, entry.Headers, entry.StatusCode, true
}

// setCachedResponse caches a response
func (h *HeadlessProxy) setCachedResponse(r *http.Request, content []byte, headers http.Header, statusCode int) {
	// Skip cache if disabled or not a successful response
	if h.CacheTTL <= 0 || statusCode < 200 || statusCode >= 300 {
		return
	}

	// Only cache GET requests
	if r.Method != http.MethodGet {
		return
	}

	// Get cache key
	key := h.getCacheKey(r)
	if key == "" {
		return
	}

	// Check cache control headers
	if cacheControl := headers.Get("Cache-Control"); cacheControl != "" {
		if strings.Contains(cacheControl, "no-store") || strings.Contains(cacheControl, "no-cache") {
			h.logger.Debug("skipping cache due to Cache-Control header", 
				zap.String("key", key),
				zap.String("cache_control", cacheControl))
			return
		}
	}

	// Copy headers to avoid modifying the original
	headersCopy := make(http.Header)
	for k, v := range headers {
		headersCopy[k] = v
	}

	// Create cache entry
	entry := cacheEntry{
		Content:    content,
		Headers:    headersCopy,
		StatusCode: statusCode,
		Expires:    time.Now().Add(time.Duration(h.CacheTTL) * time.Second),
	}

	h.cacheLock.Lock()
	defer h.cacheLock.Unlock()

	// Store in cache
	h.cache[key] = entry
	h.logger.Debug("cached response", 
		zap.String("key", key),
		zap.Int("content_length", len(content)),
		zap.Time("expires", entry.Expires))

	// Cleanup old entries if cache is too large (more than 1000 entries)
	if len(h.cache) > 1000 {
		h.cleanupCache()
	}
}

// cleanupCache removes expired entries from the cache
func (h *HeadlessProxy) cleanupCache() {
	now := time.Now()
	removed := 0

	for key, entry := range h.cache {
		if now.After(entry.Expires) {
			delete(h.cache, key)
			removed++
		}
	}

	h.logger.Info("cache cleanup completed", 
		zap.Int("removed", removed),
		zap.Int("remaining", len(h.cache)))
}
