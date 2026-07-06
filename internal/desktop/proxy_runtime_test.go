package desktop

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewReverseProxyHandler_DesktopRuntimeReturnsWSURL(t *testing.T) {
	// The proxy target mirrors ServerInstance.BaseURL: http://127.0.0.1:PORT.
	// /desktop/runtime must surface ws://localhost:PORT/ws/progress so the
	// frontend can connect directly (the Wails AssetServer 501s WS upgrades).
	h := newReverseProxyHandler("http://127.0.0.1:62041")

	req := httptest.NewRequest(http.MethodGet, "/desktop/runtime", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got struct {
		WSURL string `json:"ws_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := "ws://localhost:62041/ws/progress"
	if got.WSURL != want {
		t.Errorf("ws_url = %q, want %q", got.WSURL, want)
	}
}

func TestNewReverseProxyHandler_DesktopRuntimeNotForwarded(t *testing.T) {
	// /desktop/runtime must be answered by the proxy itself, never forwarded
	// to the API server (which does not know this route).
	forwarded := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)
	req := httptest.NewRequest(http.MethodGet, "/desktop/runtime", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if forwarded {
		t.Error("/desktop/runtime was forwarded to the backend; the proxy must short-circuit it")
	}
}

// TestNewReverseProxyHandler_NonRuntimePathsStillForward is a regression guard:
// wrapping the reverse proxy to serve /desktop/runtime must not break normal
// forwarding (the existing proxy tests cover this in detail; this asserts the
// wrapper passes an arbitrary API path through).
func TestNewReverseProxyHandler_NonRuntimePathsStillForward(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "backend-ok")
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d (path should be forwarded)", w.Code, http.StatusTeapot)
	}
}
