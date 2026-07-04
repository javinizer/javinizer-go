package desktop

import (
	"net/http"
	"strings"
)

// redirectorHTML is the loading page served at the Wails origin. It immediately
// navigates the webview to the running API server (target substituted in).
//
// We navigate to the real server origin instead of reverse-proxying through
// Wails' AssetServer because Wails returns 501 Not Implemented for WebSocket
// upgrades (see assetserver.go), and the Web UI uses /ws/progress for
// real-time progress. Loading the true origin lets REST (relative URLs) and
// the WebSocket (derived from location.origin) both work with zero frontend
// changes.
//
//nolint:unused // referenced only by app.go, which is //go:build desktop
const redirectorHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Javinizer</title>
<style>
html,body{margin:0;height:100%;display:flex;align-items:center;justify-content:center;
font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f5f5f7;color:#333}
.s{display:flex;flex-direction:column;align-items:center;gap:16px}
.spin{width:36px;height:36px;border:3px solid #d1d1d6;border-top-color:#007aff;border-radius:50%;
animation:r .8s linear infinite}
@keyframes r{to{transform:rotate(360deg)}}
p{font-size:14px;color:#666;margin:0}
</style>
<script>window.location.replace("__TARGET__");</script>
</head>
<body><div class="s"><div class="spin"></div><p>Starting Javinizer…</p></div></body>
</html>`

// newRedirectorHandler serves the loading page for GET / and 404 for anything
// else. After the redirect, the webview lives at the real server origin and the
// Wails AssetServer is never hit again.
//
//nolint:unused // referenced only by app.go, which is //go:build desktop
func newRedirectorHandler(target string) http.Handler {
	// target is always "http://127.0.0.1:%d" with a kernel-assigned int port
	// (see ServerInstance.BaseURL / StartServer). It contains no untrusted
	// input, so substituting it into the JS string literal is safe. If
	// BaseURL() ever accepts external input, this becomes an injection sink
	// and must be escaped (e.g. via net/url or a JSON string encoder).
	body := strings.Replace(redirectorHTML, "__TARGET__", target, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(body))
	})
}
