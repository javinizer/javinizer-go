package actresssync

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const intentHeader = "X-Requested-With"
const intentValue = "javinizer-offline-name-sync"
const sessionCookieName = "javinizer_session"

// principal uses only the context established by RequireTokenOrSession. A
// client header, cookie, query value, or JSON field never supplies the identity.
func principal(c *gin.Context) string {
	switch c.GetString("auth_method") {
	case "token":
		if id := c.GetString("token_id"); id != "" {
			return "token:" + id
		}
	case "session":
		// The global middleware supports ?session= for image tags. This API
		// never needs it and must not accept URL-only session authority.
		cookie, _ := c.Cookie(sessionCookieName)
		if strings.TrimSpace(cookie) == "" && strings.TrimSpace(c.GetHeader("X-Session-ID")) == "" {
			return ""
		}
		if username := c.GetString("auth_username"); username != "" {
			return "session:" + username
		}
	}
	return ""
}

func (h *handler) authenticated(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if principal(c) == "" {
		fail(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "authenticated token or non-URL session required")
		return
	}
	c.Next()
}

// writeIntent is CSRF protection, not authentication. Browsers cannot send the
// required non-simple header cross-origin without a successful preflight. Still
// check the actual Origin here: CORS response headers alone do not stop writes.
func (h *handler) writeIntent(c *gin.Context) {
	if values := c.Request.Header.Values(intentHeader); len(values) != 1 || values[0] != intentValue {
		fail(c, http.StatusForbidden, "SYNC_WRITE_INTENT_REQUIRED", "offline name sync intent header required")
		return
	}
	if values := c.Request.Header.Values("Origin"); len(values) > 1 || (len(values) == 1 && !h.allowedOrigin(values[0], c.Request)) {
		fail(c, http.StatusForbidden, "SYNC_ORIGIN_REJECTED", "request origin is not allowed")
		return
	}
	// CLI clients and same-origin fetches may omit Origin. A contradictory
	// browser fetch-metadata signal is not an origin-less CLI request.
	if c.GetHeader("Origin") == "" && strings.EqualFold(c.GetHeader("Sec-Fetch-Site"), "cross-site") {
		fail(c, http.StatusForbidden, "SYNC_ORIGIN_REJECTED", "request origin is not allowed")
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(c, http.StatusUnsupportedMediaType, "SYNC_JSON_REQUIRED", "Content-Type must be application/json")
		return
	}
	c.Next()
}

func (h *handler) allowedOrigin(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	if err != nil || origin == "null" || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || strings.ContainsAny(origin, " #\t\r\n") {
		return false
	}
	// Compare scheme, hostname and effective port. Do not trust forwarded
	// headers: a TLS-terminating proxy's public origin must be configured.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	target, err := url.Parse(scheme + "://" + r.Host)
	if err == nil && strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Hostname(), target.Hostname()) && originPort(parsed) == originPort(target) {
		return true
	}
	if h.origins != nil {
		for _, configured := range h.origins() {
			if configured != "*" && configured == origin {
				return true
			}
		}
	}
	return false
}

func originPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "http" {
		return "80"
	}
	if u.Scheme == "https" {
		return "443"
	}
	return ""
}
