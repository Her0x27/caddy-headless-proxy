package headlessproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// toJSONString converts a value to a JSON string
func toJSONString(v interface{}) string {
	bytes, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(bytes)
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

// parseInt parses an integer from a string
func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// parseBool parses a boolean from a string
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

// detectContentType detects the content type of a response
func detectContentType(content []byte) string {
	// First try standard detection
	contentType := http.DetectContentType(content)
	
	// If it's text/plain, try to be more specific
	if contentType == "text/plain; charset=utf-8" {
		// Check for JSON
		if len(content) > 0 && (content[0] == '{' || content[0] == '[') {
			return "application/json; charset=utf-8"
		}
		
		// Check for XML
		if len(content) > 0 && content[0] == '<' {
			if strings.Contains(string(content[:100]), "<html") {
				return "text/html; charset=utf-8"
			}
			return "application/xml; charset=utf-8"
		}
		
		// Check for CSS
		if strings.Contains(string(content[:100]), "{") && 
		   (strings.Contains(string(content[:100]), "body") || 
		    strings.Contains(string(content[:100]), "font") || 
		    strings.Contains(string(content[:100]), "margin")) {
			return "text/css; charset=utf-8"
		}
		
		// Check for JavaScript
		if strings.Contains(string(content[:100]), "function") || 
		   strings.Contains(string(content[:100]), "var ") || 
		   strings.Contains(string(content[:100]), "const ") || 
		   strings.Contains(string(content[:100]), "let ") {
			return "application/javascript; charset=utf-8"
		}
	}
	
	return contentType
}

// formatDuration formats a duration in seconds as a human-readable string
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	} else if seconds < 3600 {
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	} else {
		return fmt.Sprintf("%dh %dm %ds", seconds/3600, (seconds%3600)/60, seconds%60)
	}
}

// sanitizeHeaders removes sensitive headers
func sanitizeHeaders(headers http.Header) http.Header {
	result := make(http.Header)
	for k, v := range headers {
		// Skip sensitive headers
		if strings.EqualFold(k, "Authorization") || 
		   strings.EqualFold(k, "Cookie") || 
		   strings.EqualFold(k, "Set-Cookie") {
			continue
		}
		result[k] = v
	}
	return result
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// isHealthyStatusCode checks if a status code indicates a healthy response
func isHealthyStatusCode(statusCode int) bool {
	return statusCode >= 200 && statusCode < 400
}
