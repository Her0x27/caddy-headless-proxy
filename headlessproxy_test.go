package headlessproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHeadlessProxyBasic(t *testing.T) {
	// Start a test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Test Page</h1></body></html>"))
	}))
	defer ts.Close()

	// Create a new HeadlessProxy instance
	hp := &HeadlessProxy{
		Upstream:    ts.URL,
		Timeout:     30,
		EnableJS:    true,
		MaxBrowsers: 1,
		UserAgent:   "Test User Agent",
		logger:      zap.NewNop(),
	}

	// Initialize the proxy
	ctx, cancel := caddy.NewContext(caddy.Context{})
	defer cancel()
	err := hp.Provision(ctx)
	require.NoError(t, err)
	defer hp.Cleanup()

	// Create a test request
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	w := httptest.NewRecorder()

	// Create a next handler that should not be called
	nextHandler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		t.Error("Next handler should not be called")
		return nil
	})

	// Serve the request
	err = hp.ServeHTTP(w, req, nextHandler)
	require.NoError(t, err)

	// Check the response
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, w.Body.String(), "Test Page")
}

func TestHeadlessProxyCaching(t *testing.T) {
	// Start a test server with a counter to verify caching
	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf("<html><body><h1>Test Page %d</h1></body></html>", requestCount)))
	}))
	defer ts.Close()

	// Create a new HeadlessProxy instance with caching enabled
	hp := &HeadlessProxy{
		Upstream:    ts.URL,
		Timeout:     30,
		EnableJS:    true,
		MaxBrowsers: 1,
		CacheTTL:    60,
		UserAgent:   "Test User Agent",
		logger:      zap.NewNop(),
	}

	// Initialize the proxy
	ctx, cancel := caddy.NewContext(caddy.Context{})
	defer cancel()
	err := hp.Provision(ctx)
	require.NoError(t, err)
	defer hp.Cleanup()

	// Create a next handler
	nextHandler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return nil
	})

	// First request
	req1 := httptest.NewRequest("GET", "http://example.com/", nil)
	w1 := httptest.NewRecorder()
	err = hp.ServeHTTP(w1, req1, nextHandler)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, w1.Result().StatusCode)
	assert.Contains(t, w1.Body.String(), "Test Page 1")

	// Second request should be cached
	req2 := httptest.NewRequest("GET", "http://example.com/", nil)
	w2 := httptest.NewRecorder()
	err = hp.ServeHTTP(w2, req2, nextHandler)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, w2.Result().StatusCode)
	assert.Contains(t, w2.Body.String(), "Test Page 1") // Still 1, not 2
	assert.Equal(t, 1, requestCount)                    // Server only called once
}

func TestHeadlessProxyOptimization(t *testing.T) {
	// Start a test server with unoptimized HTML
	ts := httptest.NewServer(http.HandlerFunc(func(w http.Response
                                                 
