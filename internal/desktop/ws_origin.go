package desktop

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// desktopWSUpgrader returns a websocket.Upgrader whose CheckOrigin accepts the
// desktop webview origins.
//
// The Wails AssetServer answers "Upgrade: websocket" requests with 501 before
// any handler or middleware runs (pkg/assetserver/assetserver.go), so the
// reverse proxy cannot carry WS upgrades. The frontend therefore connects to
// /ws/progress directly at 127.0.0.1:PORT instead of through the proxy. That
// connection is cross-origin — the webview loads from wails:// (macOS) or
// http(s)://wails.localhost (Windows/Linux) — so the standard same-origin
// CheckOrigin configured by apiserver.NewServer would reject it. This override
// accepts the desktop webview origins.
//
// Relaxing the origin check is safe here: the server binds to 127.0.0.1 only
// (never exposed to the network) and /ws/progress still requires a valid
// session ID, which the frontend passes as a ?session= query parameter (the
// browser cannot set custom headers on a WebSocket). A malicious site would
// need both the random ephemeral port and a valid session to do anything.
func desktopWSUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: checkDesktopWSOrigin,
	}
}

func checkDesktopWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// macOS WKWebView loads the SPA from a custom wails:// scheme.
	if strings.EqualFold(u.Scheme, "wails") {
		return true
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1", "wails.localhost":
		return true
	}
	return false
}
