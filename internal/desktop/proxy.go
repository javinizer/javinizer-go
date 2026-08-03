package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
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
func newReverseProxyHandler(target string, choosePath ChooseSavePathFunc) http.Handler {
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
			handleSaveFile(w, r, choosePath)
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

// ChooseSavePathFunc shows a native "save file" dialog with filename as the
// default name and returns the chosen destination path, or ("", nil) when
// the user cancels. It deliberately does NOT write the file: handleSaveFile
// streams the request body to the returned path so multi-hundred-MB exports
// never sit in memory as an extra copy. The desktop build wires this to
// wailsruntime.SaveFileDialog (save_dialog.go): anchor[download] blob
// downloads are silently dropped by the desktop webviews (on macOS Wails
// wires no WKDownloadDelegate), so exports routed through the frontend go to
// this native path instead.
type ChooseSavePathFunc func(ctx context.Context, filename string) (string, error)

//nolint:unused // reached only via handleSaveFileLimit, which is //go:build desktop
type saveFileResponse struct {
	Saved bool   `json:"saved"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// maxSaveFileBytes caps streamed export payloads. The actress export API
// explicitly supports catalogs with 100k+ records (internal/api/actress),
// which pretty-printed can reach hundreds of MB; the body is streamed to disk
// so this is a correctness backstop against unbounded writes, not a memory
// ceiling.
//
//nolint:unused // reached only via newReverseProxyHandler, which is //go:build desktop
const maxSaveFileBytes = 1 << 30

// handleSaveFile backs POST /desktop/save-file?filename=<name>: the webview
// asks the desktop shell to persist an export through a native save dialog.
// The filename travels in the query (not the body) because the dialog needs
// it up front and the body is only consumed once a destination is chosen.
//
//nolint:unused // reached only via newReverseProxyHandler, which is //go:build desktop
func handleSaveFile(w http.ResponseWriter, r *http.Request, choosePath ChooseSavePathFunc) {
	handleSaveFileLimit(w, r, choosePath, maxSaveFileBytes)
}

// handleSaveFileLimit is handleSaveFile with an injectable size limit so
// tests can exercise the overflow path without a 1 GiB body.
//
//nolint:unused // reached only via handleSaveFile
func handleSaveFileLimit(w http.ResponseWriter, r *http.Request, choosePath ChooseSavePathFunc, maxBytes int64) {
	handleSaveFileFS(w, r, choosePath, maxBytes, saveFileDiskFS)
}

// saveFileFS indirection lets tests simulate filesystem failures (write,
// finalize, rename, remove) without fault-injecting the real OS layer. The
// handler writes to a createTemp sibling and swaps it into place with
// rename so a user's previous export survives a failed one (O_TRUNC on the
// destination would clobber it before the body has streamed).
//
//nolint:unused // saveFileDiskFS reached only via handleSaveFileLimit, which is //go:build desktop
type saveFileFS struct {
	createTemp func(dir, pattern string) (w io.WriteCloser, name string, err error)
	rename     func(oldPath, newPath string) error
	remove     func(name string) error
}

//nolint:unused // reached only via handleSaveFileLimit, which is //go:build desktop
var saveFileDiskFS = saveFileFS{
	createTemp: func(dir, pattern string) (io.WriteCloser, string, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, "", err
		}
		return f, f.Name(), nil
	},
	rename: replaceFile,
	remove: os.Remove,
}

// handleSaveFileFS is handleSaveFileLimit with the filesystem seam applied.
//
//nolint:unused // reached only via handleSaveFileLimit
func handleSaveFileFS(w http.ResponseWriter, r *http.Request, choosePath ChooseSavePathFunc, maxBytes int64, fs saveFileFS) {
	writeErr := func(status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(saveFileResponse{Error: msg})
	}
	if choosePath == nil {
		writeErr(http.StatusNotFound, "desktop: save-file is not available")
		return
	}
	filename := r.URL.Query().Get("filename")
	if err := validateSaveFilename(filename); err != nil {
		writeErr(http.StatusBadRequest, fmt.Sprintf("desktop: invalid filename: %v", err))
		return
	}
	path, err := choosePath(r.Context(), filename)
	if err != nil {
		writeErr(http.StatusInternalServerError, err.Error())
		return
	}
	if path == "" {
		// User cancelled the dialog: nothing read, nothing written.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(saveFileResponse{Saved: false})
		return
	}

	// Write to a temporary sibling, never the destination directly: if the
	// stream fails midway (oversized body, disk error, cancelled request) the
	// user's previous export stays byte-for-byte intact; the destination is
	// only swapped in by rename after copy, flush, and close all succeed.
	dir := filepath.Dir(path)
	tmp, tmpName, err := fs.createTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		writeErr(http.StatusInternalServerError, fmt.Sprintf("desktop: failed to create temp file in %s: %v", dir, err))
		return
	}
	_, copyErr := io.Copy(tmp, http.MaxBytesReader(w, r.Body, maxBytes))
	// Flush before close so a power loss between close and rename cannot
	// strand a zero-filled destination on write-back filesystems. Sync is
	// best-effort on writers that implement it (all real *os.File handles).
	var syncErr error
	if s, ok := tmp.(interface{ Sync() error }); ok {
		syncErr = s.Sync()
	}
	finErr := errors.Join(syncErr, tmp.Close())
	if copyErr != nil || finErr != nil {
		// Never leave a partial temp behind; surface it when even that fails
		// (e.g. locked file on Windows) instead of vanishing silently.
		cleanupNote := ""
		if removeErr := fs.remove(tmpName); removeErr != nil {
			cleanupNote = fmt.Sprintf("; also failed to remove partial file %s: %v", tmpName, removeErr)
		}
		// A finalize failure alongside a copy failure would otherwise vanish.
		if copyErr != nil && finErr != nil {
			cleanupNote = fmt.Sprintf("; also failed to finalize %s: %v%s", path, finErr, cleanupNote)
		}
		switch {
		case copyErr != nil && errors.As(copyErr, new(*http.MaxBytesError)):
			writeErr(http.StatusRequestEntityTooLarge, fmt.Sprintf("desktop: export exceeds %d-byte limit%s", maxBytes, cleanupNote))
		case copyErr != nil:
			writeErr(http.StatusInternalServerError, fmt.Sprintf("desktop: failed to write %s: %v%s", path, copyErr, cleanupNote))
		default:
			writeErr(http.StatusInternalServerError, fmt.Sprintf("desktop: failed to finalize %s: %v%s", path, finErr, cleanupNote))
		}
		return
	}
	if err := fs.rename(tmpName, path); err != nil {
		note := ""
		if removeErr := fs.remove(tmpName); removeErr != nil {
			note = fmt.Sprintf("; also failed to remove partial file %s: %v", tmpName, removeErr)
		}
		writeErr(http.StatusInternalServerError, fmt.Sprintf("desktop: failed to move export into place at %s: %v%s", path, err, note))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(saveFileResponse{Saved: true, Path: path})
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
	for _, c := range name {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("filename must not contain control characters: %q", name)
		}
	}
	return nil
}
