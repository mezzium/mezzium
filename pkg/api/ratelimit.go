package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

func isRateLimited(resp *http.Response, body []byte) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode == 429 {
		return true
	}
	if strings.TrimSpace(resp.Header.Get("Retry-After")) != "" {
		return true
	}
	if strings.EqualFold(resp.Header.Get("X-RateLimit-Remaining"), "0") {
		return true
	}
	var j map[string]any
	if len(body) > 0 && json.Unmarshal(body, &j) == nil {
		if errObj, ok := j["error"].(map[string]any); ok {
			if s, ok := errObj["message"].(string); ok && looksLikeRL(s) {
				return true
			}
		}
		if s, ok := j["message"].(string); ok && looksLikeRL(s) {
			return true
		}
	}
	return false
}

func looksLikeRL(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "rate limit") ||
		strings.Contains(s, "too many request") ||
		strings.Contains(s, "too many requests")
}
