package headlessproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// HealthStatus represents the health status of the headless proxy
type HealthStatus struct {
	Status         string            `json:"status"`
	Uptime         string            `json:"uptime"`
	BrowserPool    BrowserPoolStatus `json:"browser_pool"`
	CacheStatus    CacheStatus       `json:"cache"`
	SystemResources SystemResources   `json:"system_resources"`
	Version        string            `json:"version"`
	Timestamp      string            `json:"timestamp"`
}

// BrowserPoolStatus represents the status of the browser pool
type BrowserPoolStatus struct {
	Size          int  `json:"size"`
	MaxSize       int  `json:"max_size"`
	HealthyCount  int  `json:"healthy_count"`
	UnhealthyCount int `json:"unhealthy_count"`
}

// CacheStatus represents the status of the cache
type CacheStatus struct {
	Enabled    bool  `json:"enabled"`
	Size       int   `json:"size"`
	HitRate    float64 `json:"hit_rate"`
	TTL        int   `json:"ttl"`
}

// SystemResources represents the system resources
type SystemResources struct {
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	GoRoutines  int     `json:"go_routines"`
}

// RegisterHealthHandler registers a health check handler
func (h *HeadlessProxy) RegisterHealthHandler(mux *http.ServeMux) {
	mux.HandleFunc("/_health/headless-proxy", h.handleHealthCheck)
}

// handleHealthCheck handles health check requests
func (h *HeadlessProxy) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	status := h.getHealthStatus()

	w.Header().Set("Content-Type", "application/json")
	if status.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(status)
}

// getHealthStatus returns the current health status
func (h *HeadlessProxy) getHealthStatus() HealthStatus {
	h.browserPoolLock.Lock()
	poolSize := len(h.browserPool)
	h.browserPoolLock.Unlock()

	// Check browser health
	healthyCount, unhealthyCount := h.checkBrowsersHealth()

	// Calculate cache hit rate
	var hitRate float64 = 0
	if h.metrics.cacheHits.Value()+h.metrics.cacheMisses.Value() > 0 {
		hitRate = float64(h.metrics.cacheHits.Value()) / float64(h.metrics.cacheHits.Value()+h.metrics.cacheMisses.Value())
	}

	// Get cache size
	h.cacheLock.RLock()
	cacheSize := len(h.cache)
	h.cacheLock.RUnlock()

	// Determine overall status
	status := "healthy"
	if unhealthyCount > 0 && healthyCount == 0 {
		status = "unhealthy"
	} else if unhealthyCount > 0 {
		status = "degraded"
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return HealthStatus{
		Status:  status,
		Uptime:  time.Since(h.startTime).String(),
		BrowserPool: BrowserPoolStatus{
			Size:          poolSize,
			MaxSize:       h.MaxBrowsers,
			HealthyCount:  healthyCount,
			UnhealthyCount: unhealthyCount,
		},
		CacheStatus: CacheStatus{
			Enabled: h.CacheTTL > 0,
			Size:    cacheSize,
			HitRate: hitRate,
			TTL:     h.CacheTTL,
		},
		SystemResources: SystemResources{
			CPUUsage:    0, // Would need additional library to get CPU usage
			MemoryUsage: float64(memStats.Alloc) / 1024 / 1024,
			GoRoutines:  runtime.NumGoroutine(),
		},
		Version:   "1.0.0",
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// checkBrowsersHealth checks the health of all browsers in the pool
func (h *HeadlessProxy) checkBrowsersHealth() (int, int) {
	h.browserPoolLock.Lock()
	defer h.browserPoolLock.Unlock()

	healthyCount := 0
	unhealthyCount := 0

	for i, browser := range h.browserPool {
		healthy := h.isBrowserHealthy(browser)
		if healthy {
			healthyCount++
		} else {
			unhealthyCount++
			// Replace unhealthy browser
			h.logger.Warn("replacing unhealthy browser in pool", zap.Int("index", i))
			_ = browser.Close()
			h.metrics.browserClosedTotal.Inc()
			
			newBrowser := h.createBrowser()
			if newBrowser != nil {
				h.browserPool[i] = newBrowser
				h.metrics.browserCreatedTotal.Inc()
			}
		}
	}

	return healthyCount, unhealthyCount
}

// isBrowserHealthy checks if a browser is healthy
func (h *HeadlessProxy) isBrowserHealthy(browser *rod.Browser) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to create a blank page
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return false
	}
	defer page.Close()

	// Try to execute a simple JavaScript
	_, err = page.Context(ctx).Eval("1+1")
	return err == nil
}
