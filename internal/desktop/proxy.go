package desktop

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// newReverseProxyHandler forwards every request from the Wails internal
// origin to the embedded API server at target, keeping the webview same-origin
// with the SPA and REST API so it never navigates to an external http:// URL.
// macOS WKWebView blocks navigation to external URLs, which was the root cause
// of the "Authentication Service Unavailable — Load failed" error: the old
// redirector page ran window.location.replace("http://127.0.0.1:PORT"), which
// never completed, so the SPA and /api/v1/auth/status requests never fired.
//
// The proxy covers GET / (SPA index via the API server's NoRoute handler),
// GET /_app/immutable/... (SPA assets), and GET/POST /api/v1/... (REST).
//
// WebSocket upgrades are NOT proxied: the Wails AssetServer answers any
// "Upgrade: websocket" request with 501 Not Implemented in its own ServeHTTP
// (pkg/assetserver/assetserver.go) before this handler — or any Middleware —
// is invoked. Real-time progress via /ws/progress is therefore a known desktop
// limitation; the REST API and SPA load normally. Restoring WS would require
// either a Wails-level fix or the frontend connecting directly to
// 127.0.0.1:PORT.
//
//nolint:unused // referenced only by app.go, which is //go:build desktop
func newReverseProxyHandler(target string) http.Handler {
	parsed, err := url.Parse(target)
	if err != nil {
		// target is always "http://127.0.0.1:%d" (see ServerInstance.BaseURL),
		// so a parse failure is a programming bug — fail closed with 502.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "desktop: invalid proxy target", http.StatusBadGateway)
		})
	}
	return &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(parsed)
			req.SetXForwarded()
			// Route by the API server's host so gin sees the expected Host.
			req.Out.Host = parsed.Host
		},
	}
}
