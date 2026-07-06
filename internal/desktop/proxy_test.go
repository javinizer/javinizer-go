package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewReverseProxyHandler_ForwardsGETRoot(t *testing.T) {
	var gotMethod, gotPath, gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHost = r.Host
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "<html>spa-index</html>")
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "wails.localhost"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if gotMethod != http.MethodGet {
		t.Errorf("backend method = %q, want GET", gotMethod)
	}
	if gotPath != "/" {
		t.Errorf("backend path = %q, want /", gotPath)
	}
	if gotHost != backend.Listener.Addr().String() {
		t.Errorf("backend Host = %q, want %q (Host must be rewritten to the target)", gotHost, backend.Listener.Addr().String())
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), "spa-index") {
		t.Errorf("body = %q, want it to contain the backend response", string(body))
	}
}

func TestNewReverseProxyHandler_ForwardsPOSTAPIWithBody(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"hunter2"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "wails.localhost"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if gotMethod != http.MethodPost {
		t.Errorf("backend method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/auth/login" {
		t.Errorf("backend path = %q, want /api/v1/auth/login", gotPath)
	}
	if gotBody != `{"password":"hunter2"}` {
		t.Errorf("backend body = %q, want the forwarded JSON body", gotBody)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("body = %q, want the backend JSON response", string(body))
	}
}

func TestNewReverseProxyHandler_ForwardsPathsAndQuery(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, r.URL.Path+"?"+r.URL.RawQuery)
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)

	cases := []struct {
		name string
		path string
	}{
		{"spa asset", "/_app/immutable/entry.start.x.js"},
		{"auth status", "/api/v1/auth/status"},
		{"ws path", "/ws/progress"},
		{"deep api path", "/api/v1/movies/IPX-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := tc.path + "?foo=bar&baz=1"
			req := httptest.NewRequest(http.MethodGet, full, nil)
			req.Host = "wails.localhost"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
			body, _ := io.ReadAll(w.Result().Body)
			if string(body) != full {
				t.Errorf("path/query not forwarded verbatim: got %q, want %q", string(body), full)
			}
		})
	}
}

func TestNewReverseProxyHandler_PassesBackendStatusThrough(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"Not Found"}`)
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/no-such-route", nil)
	req.Host = "wails.localhost"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (backend status must pass through)", w.Code, http.StatusNotFound)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), "Not Found") {
		t.Errorf("body = %q, want the backend 404 body", string(body))
	}
}

func TestNewReverseProxyHandler_ForwardsHeaders(t *testing.T) {
	var gotAuth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Host = "wails.localhost"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if gotAuth != "Bearer test-token" {
		t.Errorf("backend Authorization = %q, want the forwarded token", gotAuth)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestNewReverseProxyHandler_InvalidTargetReturns502(t *testing.T) {
	h := newReverseProxyHandler("://not-a-url")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (invalid target must fail closed)", w.Code, http.StatusBadGateway)
	}
}
