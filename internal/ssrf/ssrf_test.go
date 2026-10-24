package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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
				if isPrivateIP(nil) != tc.wantPriv {
					t.Errorf("isPrivateIP(nil) = %v, want %v", !tc.wantPriv, tc.wantPriv)
				}
				return
			}
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			got := isPrivateIP(ip)
			if got != tc.wantPriv {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tc.ip, got, tc.wantPriv)
			}
		})
	}
}

// useTestLookup resolves only explicitly configured names and IP literals. An
// unexpected hostname must fail locally rather than falling back to real DNS.
// These tests must stay serial because the resolver seam is package-global.
func useTestLookup(t *testing.T, hosts map[string][]net.IP) {
	t.Helper()
	t.Cleanup(setLookupIPForTest(func(host string) ([]net.IP, error) {
		if ips, ok := hosts[host]; ok {
			return ips, nil
		}
		if ip := net.ParseIP(host); ip != nil {
			return []net.IP{ip}, nil
		}
		return nil, fmt.Errorf("unexpected test DNS lookup: %s", host)
	}))
}

// newClientWithTestDial preserves the client's production SSRF checks while
// replacing only the underlying network dialer with a controlled test seam.
func newClientWithTestDial(t *testing.T, dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	t.Helper()
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	transport.DialContext = dial
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = original
		transport.CloseIdleConnections()
	})
	client := NewSSRFSafeClient(5 * time.Second)
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func TestCheckURL(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{
		"public.example.test":  {net.ParseIP("203.0.113.10")},
		"private.example.test": {net.ParseIP("10.0.0.1")},
		"mixed.example.test":   {net.ParseIP("203.0.113.10"), net.ParseIP("10.0.0.1")},
	})
	testCases := []struct {
		name    string
		url     string
		wantErr string
	}{
		{"private IP 10.x", "http://10.0.0.1/", "private/internal IP"},
		{"private IP 192.168.x", "http://192.168.1.1/", "private/internal IP"},
		{"cloud metadata IP", "http://169.254.169.254/latest/meta-data/", "private/internal IP"},
		{"loopback IP", "http://127.0.0.1/", "private/internal IP"},
		{"IPv6 loopback", "http://[::1]/", "private/internal IP"},
		{"public domain", "http://public.example.test/", ""},
		{"HTTPS public domain", "https://public.example.test/", ""},
		{"public IP", "http://203.0.113.10/", ""},
		{"private domain", "http://private.example.test/", "private/internal IP"},
		{"mixed public and private addresses", "http://mixed.example.test/", "private/internal IP"},
		{"unknown domain", "http://unknown.example.test/", "failed to resolve hostname"},
		{"ftp scheme rejected", "ftp://public.example.test/", "non-http(s) scheme"},
		{"file scheme rejected", "file:///etc/passwd", "non-http(s) scheme"},
		{"empty URL", "", "non-http(s) scheme"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckURL(tc.url)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("CheckURL(%q) unexpected error: %v", tc.url, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("CheckURL(%q) error = %v, want %q", tc.url, err, tc.wantErr)
			}
		})
	}
}

func TestNewSSRFSafeClient_BlocksPrivateIP(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{
		"private.example.test": {net.ParseIP("10.0.0.1")},
	})
	var dials atomic.Int32
	client := newClientWithTestDial(t, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected underlying dial")
	})

	// Exercise the production wrapped dialer directly: private-host rejection
	// must happen before the underlying dialer, without issuing an HTTP request.
	transport := client.Transport.(*http.Transport)
	_, err := transport.DialContext(t.Context(), "tcp", "private.example.test:80")
	if err == nil || !strings.Contains(err.Error(), "resolves to private/internal IP") {
		t.Fatalf("expected private-IP rejection, got %v", err)
	}
	if got := dials.Load(); got != 0 {
		t.Errorf("underlying dialer called %d times for private destination", got)
	}
}

func TestNewSSRFSafeClient_AllowsPublicIP(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{
		"public.example.test": {net.ParseIP("203.0.113.10")},
	})
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { clientConn.Close() })
	t.Cleanup(func() { serverConn.Close() })
	calls := 0
	client := newClientWithTestDial(t, func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls++
		if ctx.Err() != nil || network != "tcp" || addr != "203.0.113.10:80" {
			t.Errorf("underlying dialer arguments differ: %s %s", network, addr)
		}
		return clientConn, nil
	})

	// Public-host authorization is a dialer contract. A net.Pipe proves that
	// the original dialer is called without requiring a public HTTP destination.
	transport := client.Transport.(*http.Transport)
	conn, err := transport.DialContext(t.Context(), "tcp", "public.example.test:80")
	if err != nil {
		t.Fatalf("expected public-IP dial to succeed, got %v", err)
	}
	if conn != clientConn || calls != 1 {
		t.Errorf("underlying dialer result not preserved: conn = %v, calls = %d", conn, calls)
	}
}

func TestNewSSRFSafeClient_BlocksRedirectToPrivateIP(t *testing.T) {
	var requests atomic.Int32
	redirectServer := httptest.NewUnstartedServer(nil)
	redirectServer.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Host != redirectServer.Listener.Addr().String() || r.URL.Path != "/redirect" {
			t.Errorf("unexpected fixture request: %s %s", r.Host, r.URL.Path)
		}
		http.Redirect(w, r, "http://private.redirect.test/", http.StatusFound)
	})
	redirectServer.Start()
	t.Cleanup(redirectServer.Close)
	fixtureURL, err := url.Parse(redirectServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the allowed first hop at the SSRF resolver seam while keeping
	// the actual HTTP destination on loopback. Public-host allow/reject policy
	// is tested independently above, so no public HTTP target is required here.
	useTestLookup(t, map[string][]net.IP{
		fixtureURL.Hostname():   {net.ParseIP("203.0.113.10")},
		"private.redirect.test": {net.ParseIP("192.168.1.1")},
	})

	var dials atomic.Int32
	client := newClientWithTestDial(t, func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		if network != "tcp" || addr != net.JoinHostPort("203.0.113.10", fixtureURL.Port()) {
			return nil, fmt.Errorf("unexpected test dial: %s %s", network, addr)
		}
		// Only the local fixture is reachable, even if SSRF validation regresses.
		return (&net.Dialer{}).DialContext(ctx, network, fixtureURL.Host)
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, redirectServer.URL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(req)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "redirect to private/internal IP") {
		t.Fatalf("expected private-IP redirect rejection, got %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("fixture handled %d requests, want 1", got)
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("underlying dialer called %d times, want only the initial fixture request", got)
	}
}

func TestCheckRedirect_BlocksPrivateIP(t *testing.T) {
	useTestLookup(t, nil)
	req := &http.Request{Header: http.Header{}}
	req.URL, _ = url.Parse("http://192.168.1.1/secret")

	err := checkRedirect(req, []*http.Request{{}})
	if err == nil || !strings.Contains(err.Error(), "resolves to private/internal IP") {
		t.Errorf("expected private-IP redirect rejection, got %v", err)
	}
}

func TestCheckRedirect_TooManyRedirects(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"public.example.test": {net.ParseIP("203.0.113.10")}})
	req := &http.Request{Header: http.Header{}}
	req.URL, _ = url.Parse("http://public.example.test/redirect")

	err := checkRedirect(req, make([]*http.Request, 10))
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Errorf("expected redirect-limit rejection, got %v", err)
	}
}

func TestCheckRedirect_AllowsPublicIP(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"public.example.test": {net.ParseIP("203.0.113.10")}})
	req := &http.Request{Header: http.Header{}}
	req.URL, _ = url.Parse("http://public.example.test/page")

	if err := checkRedirect(req, []*http.Request{{}}); err != nil {
		t.Errorf("expected no error for public-IP redirect, got %v", err)
	}
}

func TestWrapTransportWithSSRFCheck(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"private.example.test": {net.ParseIP("10.0.0.1")}})
	transport := &http.Transport{}
	WrapTransportWithSSRFCheck(transport)

	_, err := transport.DialContext(t.Context(), "tcp", "private.example.test:80")
	if err == nil || !strings.Contains(err.Error(), "resolves to private/internal IP") {
		t.Errorf("expected private-IP rejection, got %v", err)
	}
}

func TestWrapTransportWithSSRFCheck_PublicIP(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"public.example.test": {net.ParseIP("203.0.113.10")}})
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { clientConn.Close() })
	t.Cleanup(func() { serverConn.Close() })
	calls := 0
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls++
		if ctx.Err() != nil || network != "tcp" || addr != "203.0.113.10:80" {
			t.Errorf("underlying dialer arguments differ: %s %s", network, addr)
		}
		return clientConn, nil
	}}
	WrapTransportWithSSRFCheck(transport)

	conn, err := transport.DialContext(t.Context(), "tcp", "public.example.test:80")
	if err != nil {
		t.Fatalf("expected public-IP dial to succeed, got %v", err)
	}
	if conn != clientConn || calls != 1 {
		t.Errorf("underlying dialer result not preserved: conn = %v, calls = %d", conn, calls)
	}
}

func TestWrapTransportWithSSRFCheck_InvalidAddress(t *testing.T) {
	transport := &http.Transport{}
	WrapTransportWithSSRFCheck(transport)

	_, err := transport.DialContext(t.Context(), "tcp", "invalid-no-port")
	if err == nil {
		t.Error("expected error for invalid address, got nil")
	}
}

func TestCheckURL_EmptyHostname(t *testing.T) {
	err := CheckURL("http:///path")
	if err == nil {
		t.Error("expected error for empty hostname, got nil")
	}
}

func TestCheckURL_FailedResolve(t *testing.T) {
	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
		return nil, fmt.Errorf("DNS failure")
	})
	defer cleanup()

	err := CheckURL("http://nonexistent.invalid/")
	if err == nil {
		t.Error("expected error for failed DNS resolution, got nil")
	}
}
