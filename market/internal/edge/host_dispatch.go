package edge

import (
	"net/http"
	"strings"
)

// gatewayPath identifies host gateway requests for dispatch only.
// The Edge server still applies its separate, narrower allowedPath whitelist.
func gatewayPath(method, p string) bool {
	if strings.HasPrefix(p, "/v1beta/") || strings.HasPrefix(p, "/antigravity/v1beta/") {
		if method == http.MethodGet {
			return matchesPath(p, "/v1beta/models", "/v1beta/models/:model", "/antigravity/v1beta/models", "/antigravity/v1beta/models/:model")
		}
		return method == http.MethodPost && matchesPath(p, "/v1beta/models/*action", "/antigravity/v1beta/models/*action")
	}
	if strings.HasPrefix(p, "/backend-api/codex/") {
		switch method {
		case http.MethodGet:
			return matchesPath(p, "/backend-api/codex/:call_id")
		case http.MethodPost:
			return matchesPath(p, "/backend-api/codex/realtime/calls", "/backend-api/codex/responses", "/backend-api/codex/responses/*subpath", "/backend-api/codex/alpha/search")
		}
		return false
	}
	if strings.HasPrefix(p, "/antigravity/") {
		switch method {
		case http.MethodGet:
			return matchesPath(p, "/antigravity/models", "/antigravity/v1/models", "/antigravity/v1/usage")
		case http.MethodPost:
			return matchesPath(p, "/antigravity/v1/messages", "/antigravity/v1/messages/count_tokens")
		}
		return false
	}
	for _, prefix := range []string{"/api/v3", "/v3", "/v1", ""} {
		base := prefix + "/contents/generations/tasks"
		if p == base {
			return method == http.MethodPost
		}
		if matchesPath(p, base+"/:task_id") {
			return method == http.MethodGet || method == http.MethodDelete
		}
	}
	v1 := strings.HasPrefix(p, "/v1/")
	if v1 {
		p = strings.TrimPrefix(p, "/v1")
	}
	switch method {
	case http.MethodPost:
		if v1 && matchesPath(p, "/messages", "/systemone", "/live", "/images/batches", "/images/batches/:id/cancel") {
			return true
		}
		return matchesPath(p, "/messages/count_tokens", "/responses", "/responses/*subpath", "/alpha/search", "/chat/completions", "/embeddings",
			"/images/generations", "/images/edits", "/images/generations/async", "/images/edits/async",
			"/videos", "/videos/generations", "/videos/edits", "/videos/extensions", "/tts", "/stt", "/custom-voices", "/web_search", "/x_search")
	case http.MethodGet:
		if v1 && matchesPath(p, "/usage", "/live/:call_id", "/images/batches", "/images/batches/:id", "/images/batches/:id/items", "/images/batches/:id/items/:custom_id/content", "/images/batches/:id/download") {
			return true
		}
		return matchesPath(p, "/models", "/models/:model", "/responses", "/realtime", "/images/tasks/:task_id",
			"/videos/:request_id", "/videos/:request_id/content", "/videos/generations/:request_id", "/videos/generations/:request_id/content",
			"/videos/edits/:request_id", "/videos/edits/:request_id/content", "/videos/extensions/:request_id", "/videos/extensions/:request_id/content",
			"/custom-voices", "/custom-voices/:voice_id", "/custom-voices/:voice_id/audio")
	case http.MethodPatch:
		return matchesPath(p, "/custom-voices/:voice_id")
	case http.MethodDelete:
		return matchesPath(p, "/custom-voices/:voice_id") || v1 && matchesPath(p, "/images/batches/:id", "/images/batches/:id/outputs")
	}
	return false
}

// matchesPath accepts literal segments, one-segment parameters and a trailing wildcard.
func matchesPath(path string, patterns ...string) bool {
	segments := strings.Split(path, "/")
	for _, pattern := range patterns {
		parts := strings.Split(pattern, "/")
		matched := true
		for i, part := range parts {
			if i >= len(segments) {
				matched = false
				break
			}
			if strings.HasPrefix(part, "*") {
				return i == len(parts)-1
			}
			if part != segments[i] && !(strings.HasPrefix(part, ":") && segments[i] != "") {
				matched = false
				break
			}
		}
		if matched && len(parts) == len(segments) {
			return true
		}
	}
	return false
}

// IsBuyerGatewayRequest routes tm- keys on host gateway endpoints to Edge.
// Matching a gateway route does not grant access: ServeHTTP applies allowedPath.
func IsBuyerGatewayRequest(r *http.Request) bool {
	if r == nil || r.URL == nil || !gatewayPath(r.Method, r.URL.Path) {
		return false
	}
	key, _ := extractKey(r)
	return key != ""
}
