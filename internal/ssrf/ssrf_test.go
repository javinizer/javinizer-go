package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"nil IP", "", true},
		{"unspecified 0.0.0.0", "0.0.0.0", true},
		{"172.15.x not RFC1918", "172.15.0.1", false},
		{"172.32.x not RFC1918", "172.32.0.1", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ip == "" {
				if IsBlockedIP(nil) != tc.wantPriv {
					t.Errorf("IsBlockedIP(nil) = %v, want %v", !tc.wantPriv, tc.wantPriv)
				}
				return
			}
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			got := IsBlockedIP(ip)
			if got != tc.wantPriv {
				t.Errorf("IsBlockedIP(%s) = %v, want %v", tc.ip, got, tc.wantPriv)
			}
		})
	}
}

func TestCheckURL(t *testing.T) {
	// Resolver stub: no subtest may perform real DNS (offline CI must pass).
	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
		if host == "example.com" {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return nil, fmt.Errorf("test resolver: unexpected host %q", host)
	})
	defer cleanup()
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

	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
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

	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
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

	// The redirect origin is the local listener (allowlisted loopback); only
	// its 302 target is private. Asserts the actual block, not a dial error.
	undo := AllowHostForTest("127.0.0.1")
	defer undo()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, redirectServer.URL+"/go", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Error("expected error for redirect to private IP, got nil")
	} else if !strings.Contains(err.Error(), "SSRF blocked") {
		t.Errorf("expected SSRF block on the redirect target, got: %v", err)
	}
}

func TestCheckRedirect_BlocksPrivateIP(t *testing.T) {
	req := &http.Request{Header: http.Header{}}
	req.URL, _ = url.Parse("http://192.168.1.1/secret")
	via := []*http.Request{{}}

	err := checkRedirect(req, via)
	if err == nil {
		t.Error("expected error for redirect to private IP, got nil")
	}
}

func TestCheckRedirect_TooManyRedirects(t *testing.T) {
	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer cleanup()

	req := &http.Request{Header: http.Header{}}
	req.URL, _ = url.Parse("http://example.com/redirect")
	via := make([]*http.Request, 10)

	err := checkRedirect(req, via)
	if err == nil {
		t.Error("expected error for too many redirects, got nil")
	}
}

func TestCheckRedirect_AllowsPublicIP(t *testing.T) {
	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer cleanup()

	req := &http.Request{Header: http.Header{}}
	req.URL, _ = url.Parse("http://example.com/page")
	via := []*http.Request{{}}

	err := checkRedirect(req, via)
	if err != nil {
		t.Errorf("expected no error for public IP redirect, got: %v", err)
	}
}

func TestWrapTransportWithSSRFCheckClearsUnpinnableTLSDialers(t *testing.T) {
	transport := &http.Transport{
		DialTLSContext: func(context.Context, string, string) (net.Conn, error) { return nil, context.DeadlineExceeded },
		DialTLS:        func(string, string) (net.Conn, error) { return nil, context.DeadlineExceeded }, //nolint:staticcheck // cleared intentionally
	}
	WrapTransportWithSSRFCheck(transport)
	if transport.DialTLSContext != nil {
		t.Error("DialTLSContext must be cleared by the pinning wrapper")
	}
	if transport.DialTLS != nil { //nolint:staticcheck // verifying the clear
		t.Error("DialTLS must be cleared by the pinning wrapper")
	}
	if transport.DialContext == nil {
		t.Error("DialContext must be installed by the pinning wrapper")
	}
}

func TestWrapTransportWithSSRFCheck(t *testing.T) {
	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	})
	defer cleanup()

	transport := &http.Transport{}
	WrapTransportWithSSRFCheck(transport)

	_, err := transport.DialContext(t.Context(), "tcp", "private.example.com:80")
	if err == nil {
		t.Error("expected error for private IP in WrapTransportWithSSRFCheck, got nil")
	}
}

func TestWrapTransportWithSSRFCheck_PublicIP(t *testing.T) {
	cleanup := setLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer cleanup()

	transport := &http.Transport{}
	WrapTransportWithSSRFCheck(transport)

	conn, err := transport.DialContext(t.Context(), "tcp", "example.com:80")
	if err != nil {
		t.Logf("DialContext returned error (may be expected in test env): %v", err)
	} else {
		conn.Close()
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

func TestUnverifiableHostErrorText(t *testing.T) {
	e := &UnverifiableHostError{Host: "h.test", Err: fmt.Errorf("dns down")}
	s := e.Error()
	if !strings.Contains(s, "SSRF unverifiable") || !strings.Contains(s, "h.test") || !strings.Contains(s, "dns down") {
		t.Fatalf("unexpected error text: %s", s)
	}
	inner := errors.New("inner cause")
	wrapped := &UnverifiableHostError{Host: "h.test", Err: inner}
	if !errors.Is(wrapped, inner) {
		t.Fatal("Unwrap must expose the inner resolver error")
	}
}
func TestDialContextFuncAdaptsLegacyDial(t *testing.T) {
	// DialContext wins when present.
	ctxSpy := 0
	legacyCalled := false
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { ctxSpy++; return nil, errors.New("ctx") },
		Dial:        func(string, string) (net.Conn, error) { legacyCalled = true; return nil, nil }, //nolint:staticcheck // fixture for the legacy path
	}
	_, err := DialContextFunc(transport)(context.Background(), "tcp", "x:443")
	require.ErrorContains(t, err, "ctx")
	assert.False(t, legacyCalled, "DialContext takes precedence")

	// Legacy-only transports are ADAPTED (not discarded).
	sentinel := errors.New("legacy dialer routed it")
	legacy := &http.Transport{Dial: func(network, addr string) (net.Conn, error) { //nolint:staticcheck // fixture
		assert.Equal(t, "tcp", network)
		assert.Equal(t, "host.example:443", addr)
		return nil, sentinel
	}}
	_, err = DialContextFunc(legacy)(context.Background(), "tcp", "host.example:443")
	require.ErrorIs(t, err, sentinel)

	// Legacy dials that outlive the request context are abandoned with the
	// context error (legacy Dial has no cancellation).
	settle := make(chan struct{})
	t.Cleanup(func() { close(settle) })
	slow := &http.Transport{Dial: func(string, string) (net.Conn, error) { //nolint:staticcheck // fixture
		<-settle // blocks until the test ends; the adapter abandons it first
		return nil, errors.New("abandoned dial finally settled")
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = DialContextFunc(slow)(ctx, "tcp", "never:443")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)

	// Neither: a default dialer is returned.
	assert.NotNil(t, DialContextFunc(&http.Transport{}))
}

// A canceled caller abandons the legacy dial; when it completes LATE with a
// live connection, that connection must be closed instead of leaking an fd.
func TestDialContextFuncClosesLateLegacyResults(t *testing.T) {
	connA, connB := net.Pipe()
	defer func() { _ = connB.Close() }()
	release := make(chan struct{})
	legacy := &http.Transport{Dial: func(string, string) (net.Conn, error) { //nolint:staticcheck // fixture
		<-release
		return connA, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := DialContextFunc(legacy)(ctx, "tcp", "late.example:443")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(release)
	require.Eventually(t, func() bool {
		_, werr := connA.Write([]byte{0})
		return werr != nil
	}, 2*time.Second, 5*time.Millisecond, "late legacy result must be closed once abandoned")
}

// NewPinnedDialTransport must honor a legacy-only Dial hook (not discard it
// for a default dialer), and the hook receives the PINNED address.
func TestNewPinnedDialTransportHonorsLegacyDial(t *testing.T) {
	cleanup := SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer cleanup()
	sentinel := errors.New("legacy dialed")
	var dialed string
	base := &http.Transport{Dial: func(network, addr string) (net.Conn, error) { //nolint:staticcheck // exercising the legacy seam
		dialed = addr
		return nil, sentinel
	}}
	wrapped, err := NewPinnedDialTransport(base)
	require.NoError(t, err)
	_, err = wrapped.DialContext(context.Background(), "tcp", "media.example:443")
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, "93.184.216.34:443", dialed)
}

// Live transports must keep their markers; dead ones must be released by the
// runtime cleanup -- never evicted while alive, never retained after death.
func TestRemoteDNSRegistryReleasesCollected(t *testing.T) {
	MarkRemoteDNSTransport(nil) // no-op guard
	live := &http.Transport{}
	MarkRemoteDNSTransport(live)
	MarkRemoteDNSTransport(live) // idempotent re-mark
	assert.False(t, TransportResolvesRemotely(nil))
	defer func() { _ = live }() // keep alive past this test

	var dead weak.Pointer[http.Transport]
	func() {
		tmp := &http.Transport{}
		MarkRemoteDNSTransport(tmp)
		dead = weak.Make(tmp)
		require.True(t, TransportResolvesRemotely(tmp))
	}()
	require.Eventually(t, func() bool {
		runtime.GC()
		return dead.Value() == nil && !RemoteDNSHasWeakEntryForTest(dead)
	}, 5*time.Second, 20*time.Millisecond, "collected transports release their marker")
	assert.True(t, TransportResolvesRemotely(live), "live markers are never evicted")
}
