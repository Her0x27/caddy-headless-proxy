package headlessproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// BrowserMonitor monitors browser resource usage
type BrowserMonitor struct {
	proxy *HeadlessProxy
}

// NewBrowserMonitor creates a new browser monitor
func NewBrowserMonitor(proxy *HeadlessProxy) *BrowserMonitor {
	return &BrowserMonitor{
		proxy: proxy,
	}
}

// StartMonitoring starts monitoring browser resources
func (m *BrowserMonitor) StartMonitoring(ctx context.Context) {
	go m.monitorLoop(ctx)
}

// monitorLoop periodically monitors browser resources
func (m *BrowserMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.collectBrowserMetrics()
		}
	}
}

// collectBrowserMetrics collects metrics from all browsers in the pool
func (m *BrowserMonitor) collectBrowserMetrics() {
	m.proxy.browserPoolLock.Lock()
	browsers := make([]*rod.Browser, len(m.proxy.browserPool))
	copy(browsers, m.proxy.browserPool)
	m.proxy.browserPoolLock.Unlock()

	m.proxy.metrics.browserPoolSize.Set(float64(len(browsers)))

	for _, browser := range browsers {
		go m.collectMetricsFromBrowser(browser)
	}
}

// collectMetricsFromBrowser collects metrics from a single browser
func (m *BrowserMonitor) collectMetricsFromBrowser(browser *rod.Browser) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a page for monitoring
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		m.proxy.logger.Error("failed to create monitoring page", zap.Error(err))
		m.proxy.metrics.browserErrorsTotal.WithLabelValues("create_page").Inc()
		return
	}
	defer page.Close()

	// Collect memory info
	memoryInfo, err := page.Browser().Call(ctx, "System.getProcessInfo")
	if err != nil {
		m.proxy.logger.Error("failed to get process info", zap.Error(err))
		m.proxy.metrics.browserErrorsTotal.WithLabelValues("get_process_info").Inc()
		return
	}

	var processInfo map[string]interface{}
	if err := json.Unmarshal(memoryInfo, &processInfo); err != nil {
		m.proxy.logger.Error("failed to parse process info", zap.Error(err))
		return
	}

	if result, ok := processInfo["result"].(map[string]interface{}); ok {
		// Extract memory usage
		if memoryInfo, ok := result["memoryInfo"].(map[string]interface{}); ok {
			if jsHeapSizeLimit, ok := memoryInfo["jsHeapSizeLimit"].(float64); ok {
				m.proxy.metrics.browserResourcesUsed.WithLabelValues("js_heap_size_limit").Set(jsHeapSizeLimit)
			}
			if totalJSHeapSize, ok := memoryInfo["totalJSHeapSize"].(float64); ok {
				m.proxy.metrics.browserResourcesUsed.WithLabelValues("total_js_heap_size").Set(totalJSHeapSize)
			}
			if usedJSHeapSize, ok := memoryInfo["usedJSHeapSize"].(float64); ok {
				m.proxy.metrics.browserResourcesUsed.WithLabelValues("used_js_heap_size").Set(usedJSHeapSize)
			}
		}

		// Extract CPU usage
		if cpuTime, ok := result["cpuTime"].(map[string]interface{}); ok {
			if user, ok := cpuTime["user"].(float64); ok {
				m.proxy.metrics.browserResourcesUsed.WithLabelValues("cpu_user").Set(user)
			}
			if system, ok := cpuTime["system"].(float64); ok {
				m.proxy.metrics.browserResourcesUsed.WithLabelValues("cpu_system").Set(system)
			}
		}
	}
}

// MonitorPagePerformance collects performance metrics from a page
func (m *BrowserMonitor) MonitorPagePerformance(page *rod.Page) (map[string]interface{}, error) {
	script := `
	(() => {
		const performance = window.performance;
		if (!performance) {
			return { error: "Performance API not available" };
		}

		const timing = performance.timing;
		const navigation = performance.navigation;
		const memory = performance.memory;

		// Get resource timing data
		const resources = performance.getEntriesByType('resource').map(resource => ({
			name: resource.name,
			entryType: resource.entryType,
			startTime: resource.startTime,
			duration: resource.duration,
			initiatorType: resource.initiatorType,
			transferSize: resource.transferSize,
			encodedBodySize: resource.encodedBodySize,
			decodedBodySize: resource.decodedBodySize
		}));

		// Calculate key metrics
		const loadTime = timing.loadEventEnd - timing.navigationStart;
		const domContentLoaded = timing.domContentLoadedEventEnd - timing.navigationStart;
		const firstPaint = performance.getEntriesByName('first-paint')[0]?.startTime || 0;
		const firstContentfulPaint = performance.getEntriesByName('first-contentful-paint')[0]?.startTime || 0;

		return {
			navigation: {
				type: navigation.type,
				redirectCount: navigation.redirectCount
			},
			timing: {
				navigationStart: timing.navigationStart,
				unloadEventStart: timing.unloadEventStart,
				unloadEventEnd: timing.unloadEventEnd,
				redirectStart: timing.redirectStart,
				redirectEnd: timing.redirectEnd,
				fetchStart: timing.fetchStart,
				domainLookupStart: timing.domainLookupStart,
				domainLookupEnd: timing.domainLookupEnd,
				connectStart: timing.connectStart,
				connectEnd: timing.connectEnd,
				secureConnectionStart: timing.secureConnectionStart,
				requestStart: timing.requestStart,
				responseStart: timing.responseStart,
				responseEnd: timing.responseEnd,
				domLoading: timing.domLoading,
				domInteractive: timing.domInteractive,
				domContentLoadedEventStart: timing.domContentLoadedEventStart,
				domContentLoadedEventEnd: timing.domContentLoadedEventEnd,
				domComplete: timing.domComplete,
				loadEventStart: timing.loadEventStart,
				loadEventEnd: timing.loadEventEnd
			},
			memory: memory ? {
				jsHeapSizeLimit: memory.jsHeapSizeLimit,
				totalJSHeapSize: memory.totalJSHeapSize,
				usedJSHeapSize: memory.usedJSHeapSize
			} : null,
			metrics: {
				loadTime,
				domContentLoaded,
				firstPaint,
				firstContentfulPaint
			},
			resources: resources.slice(0, 50) // Limit to first 50 resources
		};
	})();
	`

	var result map[string]interface{}
	err := page.Eval(script).Unmarshal(&result)
	if err != nil {
		return nil, fmt.Errorf("failed to collect performance metrics: %v", err)
	}

	return result, nil
}
