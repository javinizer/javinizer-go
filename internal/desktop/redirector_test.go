package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedirectorHTML_ContainsTargetPlaceholder(t *testing.T) {
	if !strings.Contains(redirectorHTML, "__TARGET__") {
		t.Fatal("redirectorHTML must contain __TARGET__ placeholder for substitution")
	}
	if !strings.Contains(redirectorHTML, "window.location.replace") {
		t.Fatal("redirectorHTML must perform window.location.replace to navigate to the server")
	}
}

func TestNewRedirectorHandler_TargetSubstituted(t *testing.T) {
	target := "http://127.0.0.1:54321"
	h := newRedirectorHandler(target)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), target) {
		t.Errorf("response body does not contain target %q", target)
	}
	if strings.Contains(string(body), "__TARGET__") {
		t.Error("response body still contains unsubstituted __TARGET__ placeholder")
	}
}

func TestNewRedirectorHandler_RootAndIndexHTML(t *testing.T) {
	h := newRedirectorHandler("http://127.0.0.1:1")

	for _, path := range []string{"/", "/index.html"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("path %q: status = %d, want %d", path, w.Code, http.StatusOK)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("path %q: Content-Type = %q, want text/html", path, ct)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("path %q: Cache-Control = %q, want no-cache", path, cc)
		}
	}
}

func TestNewRedirectorHandler_NonRootReturns404(t *testing.T) {
	h := newRedirectorHandler("http://127.0.0.1:1")

	for _, path := range []string{"/api/v1/foo", "/_app/immutable/x.js", "/favicon.ico"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("path %q: status = %d, want %d (only / and /index.html are served)", path, w.Code, http.StatusNotFound)
		}
	}
}

func TestNewRedirectorHandler_NonRootPostReturns404(t *testing.T) {
	h := newRedirectorHandler("http://127.0.0.1:1")

	// The handler is path-based (it serves the redirector for "/" and
	// "/index.html" regardless of method, since the webview only issues GET).
	// Any non-root path — including POSTs — falls through to 404.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/foo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("POST /api/v1/foo: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
