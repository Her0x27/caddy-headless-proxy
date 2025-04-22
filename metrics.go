package headlessproxy

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all the prometheus metrics for the headless proxy
type Metrics struct {
	// Request metrics
	requestsTotal      *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	requestSize        *prometheus.HistogramVec
	responseSize       *prometheus.HistogramVec
	responseStatusCode *prometheus.CounterVec

	// Cache metrics
	cacheHits   prometheus.Counter
	cacheMisses prometheus.Counter

	// Browser metrics
	browserPoolSize      prometheus.Gauge
	browserCreatedTotal  prometheus.Counter
	browserClosedTotal   prometheus.Counter
	browserRenderTime    prometheus.Histogram
	browserErrorsTotal   *prometheus.CounterVec
	browserResourcesUsed *prometheus.GaugeVec

	// Resource optimization metrics
	optimizationSavings prometheus.Counter

	once sync.Once
}

// initMetrics initializes all prometheus metrics
func (h *HeadlessProxy) initMetrics() {
	h.metrics.once.Do(func() {
		// Request metrics
		h.metrics.requestsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_requests_total",
				Help: "Total number of requests processed by the headless proxy",
			},
			[]string{"method", "status"},
		)

		h.metrics.requestDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "caddy_headless_proxy_request_duration_seconds",
				Help:    "Duration of requests processed by the headless proxy",
				Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
			},
			[]string{"method", "status"},
		)

		h.metrics.requestSize = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "caddy_headless_proxy_request_size_bytes",
				Help:    "Size of requests processed by the headless proxy",
				Buckets: prometheus.ExponentialBuckets(10, 10, 8),
			},
			[]string{"method"},
		)

		h.metrics.responseSize = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "caddy_headless_proxy_response_size_bytes",
				Help:    "Size of responses processed by the headless proxy",
				Buckets: prometheus.ExponentialBuckets(10, 10, 8),
			},
			[]string{"method", "status"},
		)

		h.metrics.responseStatusCode = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_response_status_code_total",
				Help: "Total number of response status codes",
			},
			[]string{"status_code"},
		)

		// Cache metrics
		h.metrics.cacheHits = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_cache_hits_total",
				Help: "Total number of cache hits",
			},
		)

		h.metrics.cacheMisses = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_cache_misses_total",
				Help: "Total number of cache misses",
			},
		)

		// Browser metrics
		h.metrics.browserPoolSize = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "caddy_headless_proxy_browser_pool_size",
				Help: "Current size of the browser pool",
			},
		)

		h.metrics.browserCreatedTotal = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_browser_created_total",
				Help: "Total number of browsers created",
			},
		)

		h.metrics.browserClosedTotal = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_browser_closed_total",
				Help: "Total number of browsers closed",
			},
		)

		h.metrics.browserRenderTime = promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "caddy_headless_proxy_browser_render_time_seconds",
				Help:    "Time taken to render a page in the browser",
				Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
			},
		)

		h.metrics.browserErrorsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_browser_errors_total",
				Help: "Total number of browser errors",
			},
			[]string{"error_type"},
		)

		h.metrics.browserResourcesUsed = promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "caddy_headless_proxy_browser_resources_used",
				Help: "Resources used by the browser",
			},
			[]string{"resource_type"},
		)

		// Resource optimization metrics
		h.metrics.optimizationSavings = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "caddy_headless_proxy_optimization_savings_bytes",
				Help: "Total bytes saved by resource optimization",
			},
		)
	})
}
