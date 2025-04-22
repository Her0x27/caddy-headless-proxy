package headlessproxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"go.uber.org/zap"
)

// Error types
var (
	ErrBrowserUnavailable = errors.New("browser unavailable")
	ErrPageCreationFailed = errors.New("page creation failed")
	ErrNavigationFailed   = errors.New("navigation failed")
	ErrTimeout            = errors.New("operation timed out")
	ErrRequestFailed      = errors.New("request failed")
	ErrResponseProcessing = errors.New("response processing failed")
)

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error       string `json:"error"`
	Status      int    `json:"status"`
	Description string `json:"description,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	Timestamp   string `json:"timestamp"`
}

// handleError handles an error and returns an appropriate HTTP response
func (h *HeadlessProxy) handleError(w http.ResponseWriter, r *http.Request, err error, status int) {
	// Generate a request ID if not present
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Log the error
	h.logger.Error("request error",
		zap.Error(err),
		zap.Int("status", status),
		zap.String("request_id", requestID),
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
	)

	// Increment error metrics
	errorType := "unknown"
	switch {
	case errors.Is(err, ErrBrowserUnavailable):
		errorType = "browser_unavailable"
	case errors.Is(err, ErrPageCreationFailed):
		errorType = "page_creation_failed"
	case errors.Is(err, ErrNavigationFailed):
		errorType = "navigation_failed"
	case errors.Is(err, ErrTimeout):
		errorType = "timeout"
	case errors.Is(err, ErrRequestFailed):
		errorType = "request_failed"
	case errors.Is(err, ErrResponseProcessing):
		errorType = "response_processing"
	case errors.Is(err, context.DeadlineExceeded):
		errorType = "deadline_exceeded"
		err = ErrTimeout
	}
	h.metrics.browserErrorsTotal.WithLabelValues(errorType).Inc()

	// Create error response
	errorResponse := ErrorResponse{
		Error:       errorType,
		Status:      status,
		Description: err.Error(),
		RequestID:   requestID,
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("X-Error-Type", errorType)
	w.WriteHeader(status)

	// Write error response
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		h.logger.Error("failed to encode error response", zap.Error(err))
	}
}

// handleBrowserError handles browser-specific errors
func (h *HeadlessProxy) handleBrowserError(browser *rod.Browser, err error) error {
	if err == nil {
		return nil
	}

	// Check for common browser errors
	errStr := err.Error()
	
	switch {
	case strings.Contains(errStr, "context deadline exceeded"):
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	case strings.Contains(errStr, "target closed"):
		return fmt.Errorf("%w: target closed", ErrNavigationFailed)
	case strings.Contains(errStr, "net::ERR"):
		return fmt.Errorf("%w: network error: %v", ErrNavigationFailed, err)
	case strings.Contains(errStr, "page crashed"):
		// Try to recover the browser
		if browser != nil {
			h.logger.Warn("page crashed, attempting to recover browser")
			h.recoverBrowser(browser)
		}
		return fmt.Errorf("%w: page crashed", ErrNavigationFailed)
	default:
		return err
	}
}

// recoverBrowser attempts to recover a browser after a crash
func (h *HeadlessProxy) recoverBrowser(browser *rod.Browser) {
	// Close all pages
	pages, err := browser.Pages()
	if err == nil {
		for _, page := range pages {
			_ = page.Close()
		}
	}

	// Try to create a test page
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		h.logger.Error("failed to create recovery page", zap.Error(err))
		return
	}
	
	// Try to execute a simple JavaScript
	_, err = page.Context(ctx).Eval("1+1")
	if err != nil {
		h.logger.Error("browser recovery failed", zap.Error(err))
	} else {
		h.logger.Info("browser recovered successfully")
	}
	
	_ = page.Close()
}
