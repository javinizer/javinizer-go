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

func TestRewriteSessionCookies_StripsSecureAndDomain(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", (&http.Cookie{
		Name:   "javinizer_session",
		Value:  "abc123",
		Secure: true,
		Domain: "example.com",
	}).String())

	if err := rewriteSessionCookies(resp); err != nil {
		t.Fatalf("rewriteSessionCookies returned error: %v", err)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Secure {
		t.Errorf("Secure attribute not stripped")
	}
	if c.Domain != "" {
		t.Errorf("Domain attribute not stripped: got %q", c.Domain)
	}
	if c.Value != "abc123" {
		t.Errorf("cookie value changed: got %q, want %q", c.Value, "abc123")
	}
	raw := resp.Header.Get("Set-Cookie")
	if strings.Contains(raw, "Secure") {
		t.Errorf("rewritten Set-Cookie still contains Secure: %q", raw)
	}
	if strings.Contains(raw, "Domain") {
		t.Errorf("rewritten Set-Cookie still contains Domain: %q", raw)
	}
}

func TestRewriteSessionCookies_NoOpWithoutCookies(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}

	if err := rewriteSessionCookies(resp); err != nil {
		t.Fatalf("rewriteSessionCookies returned error: %v", err)
	}

	if len(resp.Cookies()) != 0 {
		t.Errorf("expected no cookies after no-op rewrite, got %d", len(resp.Cookies()))
	}
	if got := resp.Header.Get("Set-Cookie"); got != "" {
		t.Errorf("Set-Cookie header should be absent after no-op, got %q", got)
	}
}

func TestRewriteSessionCookies_HandlesMultipleCookies(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", (&http.Cookie{
		Name:   "javinizer_session",
		Value:  "sid1",
		Secure: true,
		Domain: "example.com",
	}).String())
	resp.Header.Add("Set-Cookie", (&http.Cookie{
		Name:   "prefs",
		Value:  "dark",
		Secure: true,
		Domain: "api.example.com",
	}).String())

	if err := rewriteSessionCookies(resp); err != nil {
		t.Fatalf("rewriteSessionCookies returned error: %v", err)
	}

	cookies := resp.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
	for _, c := range cookies {
		if c.Secure {
			t.Errorf("cookie %q: Secure attribute not stripped", c.Name)
		}
		if c.Domain != "" {
			t.Errorf("cookie %q: Domain attribute not stripped: %q", c.Name, c.Domain)
		}
	}
	if got := len(resp.Header.Values("Set-Cookie")); got != 2 {
		t.Errorf("expected 2 Set-Cookie headers after rewrite, got %d", got)
	}
}

func TestNewReverseProxyHandler_RewritesSessionCookiesFromBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:   "javinizer_session",
			Value:  "proxied-sid",
			Secure: true,
			Domain: "example.com",
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	h := newReverseProxyHandler(backend.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	req.Host = "wails.localhost"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 Set-Cookie in proxied response, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Secure {
		t.Errorf("proxy did not strip Secure from backend Set-Cookie")
	}
	if c.Domain != "" {
		t.Errorf("proxy did not strip Domain from backend Set-Cookie: got %q", c.Domain)
	}
	if c.Value != "proxied-sid" {
		t.Errorf("cookie value changed: got %q, want %q", c.Value, "proxied-sid")
	}
}
