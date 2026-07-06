package desktop

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckDesktopWSOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		want   bool
	}{
		{"empty origin (non-browser client)", "", true},
		{"macOS wails scheme", "wails://wails.localhost", true},
		{"Windows wails.localhost http", "http://wails.localhost", true},
		{"Linux wails.localhost https", "https://wails.localhost", true},
		{"localhost", "http://localhost:5173", true},
		{"loopback ipv4", "http://127.0.0.1:8080", true},
		{"loopback ipv6", "http://[::1]:8080", true},
		{"external site rejected", "https://evil.example.com", false},
		{"malformed origin rejected", "://not-a-url", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			got := checkDesktopWSOrigin(req)
			if got != tc.want {
				t.Errorf("checkDesktopWSOrigin(origin=%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}

func TestDesktopWSUpgrader_CheckOriginAcceptsWails(t *testing.T) {
	u := desktopWSUpgrader()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "wails://wails.localhost")
	if !u.CheckOrigin(req) {
		t.Fatal("upgrader CheckOrigin must accept the macOS wails:// webview origin")
	}
}
