package ssrf

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestIsPrivateIP(t *testing.T) {
	testCases := []struct {
		name     string
		ip       string
		wantPriv bool
	}{
		{"RFC1918 10.x", "10.0.0.1", true},
		{"RFC1918 172.16.x", "172.16.0.1", true},
		{"RFC1918 172.31.x upper bound", "172.31.255.255", true},
		{"RFC1918 192.168.x", "192.168.1.1", true},
		{"link-local cloud metadata", "169.254.169.254", true},
		{"loopback", "127.0.0.1", true},
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"IPv6 loopback", "::1", true},
		{"IPv6 link-local", "fe80::1", true},
		{"nil IP", "", false},
		{"unspecified 0.0.0.0", "0.0.0.0", true},
		{"172.15.x not RFC1918", "172.15.0.1", false},
		{"172.32.x not RFC1918", "172.32.0.1", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ip == "" {
				if IsPrivateIP(nil) != tc.wantPriv {
					t.Errorf("IsPrivateIP(nil) = %v, want %v", !tc.wantPriv, tc.wantPriv)
				}
				return
			}
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			got := IsPrivateIP(ip)
			if got != tc.wantPriv {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tc.ip, got, tc.wantPriv)
			}
		})
	}
}

func TestCheckURL(t *testing.T) {
	testCases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"private IP 10.x", "http://10.0.0.1/", true},
		{"private IP 192.168.x", "http://192.168.1.1/", true},
		{"cloud metadata IP", "http://169.254.169.254/latest/meta-data/", true},
		{"loopback IP", "http://127.0.0.1/", true},
		{"public domain", "http://example.com/", false},
		{"public IP", "http://8.8.8.8/", false},
		{"ftp scheme rejected", "ftp://example.com/", true},
		{"file scheme rejected", "file:///etc/passwd", true},
		{"empty URL", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckURL(tc.url)
			if tc.wantErr && err == nil {
				t.Errorf("CheckURL(%q) expected error, got nil", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("CheckURL(%q) unexpected error: %v", tc.url, err)
			}
		})
	}
}

func TestNewSSRFSafeClient_BlocksPrivateIPRedirect(t *testing.T) {
	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("public"))
	}))
	defer publicServer.Close()

	client := NewSSRFSafeClient(5 * time.Second)

	cleanup := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		switch host {
		case "public.example.com":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		case "private.example.com":
			return []net.IP{net.ParseIP("10.0.0.1")}, nil
		default:
			return net.LookupIP(host)
		}
	})
	defer cleanup()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://private.example.com/", nil)
	if err == nil {
		_, err = client.Do(req)
	}
	if err == nil {
		t.Error("expected error for private IP, got nil")
	}
}

func TestNewSSRFSafeClient_BlocksRedirectToPrivateIP(t *testing.T) {
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://private.redirect.target/", http.StatusFound)
	}))
	defer redirectServer.Close()

	client := NewSSRFSafeClient(5 * time.Second)

	cleanup := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		switch host {
		case "public.example.com":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		case "private.redirect.target":
			return []net.IP{net.ParseIP("192.168.1.1")}, nil
		default:
			return net.LookupIP(host)
		}
	})
	defer cleanup()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://public.example.com/redirect", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Error("expected error for redirect to private IP, got nil")
	}
}

func TestCheckRedirect_BlocksPrivateIP(t *testing.T) {
	req := &http.Request{Header: http.Header{}}
	req.URL, _ = url.Parse("http://192.168.1.1/secret")
	via := []*http.Request{{}}

	err := CheckRedirect(req, via)
	if err == nil {
		t.Error("expected error for redirect to private IP, got nil")
	}
}
