package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
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
// is invoked. Instead, the frontend fetches GET /desktop/runtime (handled
// below) to learn the WS URL, then connects directly to
// ws://localhost:PORT/ws/progress?session=SID. See ws_origin.go for the
// upgrader override that accepts the cross-origin webview.
//
//nolint:unused // referenced only by app.go, which is //go:build desktop
func newReverseProxyHandler(target string, saveFile SaveFileFunc) http.Handler {
	parsed, err := url.Parse(target)
	if err != nil {
		// target is always "http://127.0.0.1:%d" (see ServerInstance.BaseURL),
		// so a parse failure is a programming bug — fail closed with 502.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "desktop: invalid proxy target", http.StatusBadGateway)
		})
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(parsed)
			req.SetXForwarded()
			// Route by the API server's host so gin sees the expected Host.
			req.Out.Host = parsed.Host
		},
		ModifyResponse: rewriteSessionCookies,
	}
	// Derive the WS URL from the parsed target so it stays consistent with
	// the proxy destination and doesn't depend on target's exact string
	// prefix. The webview connects directly (the Wails AssetServer 501s WS
	// upgrades); localhost (not 127.0.0.1) so the upgrader's CheckOrigin and
	// the browser's same-origin policy are happy.
	wsURL := (&url.URL{Scheme: "ws", Host: "localhost:" + parsed.Port(), Path: "/ws/progress"}).String()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/desktop/runtime" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"ws_url": wsURL})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/desktop/save-file" {
			handleSaveFile(w, r, saveFile)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// rewriteSessionCookies rewrites Set-Cookie headers from the API server so the
// browser stores them against the webview's origin. The API server may set
// Secure (when X-Forwarded-Proto is https) and a Domain attribute that targets
// 127.0.0.1:PORT; neither applies to the Wails internal origin the webview
// loads from, so WKWebView drops the cookie and the session is lost. Stripping
// Secure and Domain makes the cookie default to the proxy's host (the webview
// origin) so it is stored and sent on subsequent same-origin requests.
//
//nolint:unused // referenced only by newReverseProxyHandler, which is //go:build desktop
func rewriteSessionCookies(resp *http.Response) error {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return nil
	}
	resp.Header.Del("Set-Cookie")
	for _, c := range cookies {
		c.Secure = false
		c.Domain = ""
		resp.Header.Add("Set-Cookie", c.String())
	}
	return nil
}

// SaveFileFunc shows a native "save file" dialog with filename as the
// default name and, if the user confirms, writes content to the chosen path.
// It returns the path actually written, or ("", nil) when the user cancels.
// The desktop build wires this to wailsruntime.SaveFileDialog (save_dialog.go):
// anchor[download] blob downloads are silently dropped by the desktop webviews
// (on macOS Wails wires no WKDownloadDelegate), so exports routed through the
// frontend go to this native path instead.
type SaveFileFunc func(ctx context.Context, filename string, content []byte) (string, error)

//nolint:unused // reached only via newReverseProxyHandler, which is //go:build desktop
type saveFileRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

//nolint:unused // reached only via newReverseProxyHandler, which is //go:build desktop
type saveFileResponse struct {
	Saved bool   `json:"saved"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// maxSaveFileBytes caps export payloads; replacement lists and actress
// exports are a few hundred KB at most.
//
//nolint:unused // reached only via newReverseProxyHandler, which is //go:build desktop
const maxSaveFileBytes = 32 << 20

// handleSaveFile backs POST /desktop/save-file: the webview asks the desktop
// shell to persist an export through a native save dialog. The filename comes
// from the webview, so it is validated down to a bare name before the dialog
// or any write can see it.
//
//nolint:unused // reached only via newReverseProxyHandler, which is //go:build desktop
func handleSaveFile(w http.ResponseWriter, r *http.Request, saveFile SaveFileFunc) {
	writeErr := func(status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(saveFileResponse{Error: msg})
	}
	if saveFile == nil {
		writeErr(http.StatusNotFound, "desktop: save-file is not available")
		return
	}
	var req saveFileRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSaveFileBytes)).Decode(&req); err != nil {
		writeErr(http.StatusBadRequest, fmt.Sprintf("desktop: invalid save-file request: %v", err))
		return
	}
	if err := validateSaveFilename(req.Filename); err != nil {
		writeErr(http.StatusBadRequest, fmt.Sprintf("desktop: invalid filename: %v", err))
		return
	}
	path, err := saveFile(r.Context(), req.Filename, []byte(req.Content))
	if err != nil {
		writeErr(http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(saveFileResponse{Saved: path != "", Path: path})
}

// validateSaveFilename rejects empty names and anything with path components:
// the webview controls this string, and neither the dialog nor the write may
// escape to an attacker-chosen path.
//
//nolint:unused // reached only via newReverseProxyHandler, which is //go:build desktop
func validateSaveFilename(name string) error {
	if name == "" {
		return fmt.Errorf("filename must not be empty")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return fmt.Errorf("filename must not contain path components: %q", name)
	}
	return nil
}
