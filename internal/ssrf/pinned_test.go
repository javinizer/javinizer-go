package ssrf

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestIsBlockedIPUnparseableSlice(t *testing.T) {
	// A non-nil but malformed net.IP (wrong length) fails closed.
	if !IsBlockedIP(net.IP{1, 2, 3}) {
		t.Error("malformed IP slice must be blocked")
	}
}

func TestIsBlockedIPExtendedRanges(t *testing.T) {
	blocked := []string{
		"100.64.0.1",      // CGNAT (cloud metadata, Tailscale)
		"100.100.100.200", // Alibaba metadata
		"192.0.0.9",       // IETF protocol assignments
		"198.18.0.5",      // benchmarking
		"240.0.0.1",       // reserved
		"192.31.196.1",    // AS112
		"192.88.99.10",    // 6to4 relay anycast
		"fec0::1",         // deprecated site-local
		"224.0.0.1",       // multicast
	}
	for _, tc := range blocked {
		if !IsBlockedIP(net.ParseIP(tc)) {
			t.Errorf("IsBlockedIP(%s) = false, want true", tc)
		}
	}
	public := []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"}
	for _, tc := range public {
		if IsBlockedIP(net.ParseIP(tc)) {
			t.Errorf("IsBlockedIP(%s) = true, want false", tc)
		}
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCheckTargetTypedErrors(t *testing.T) {
	var blocked *BlockedTargetError
	if err := CheckTarget(context.Background(), mustURL(t, "ftp://8.8.8.8/x")); !errors.As(err, &blocked) {
		t.Errorf("scheme rejection not typed BlockedTargetError: %v", err)
	}
	if err := CheckTarget(context.Background(), mustURL(t, "https://127.0.0.1/x")); !errors.As(err, &blocked) {
		t.Errorf("loopback not typed BlockedTargetError: %v", err)
	}
	cleanup := SetLookupIPForTest(func(string) ([]net.IP, error) { return nil, errors.New("dns down") })
	defer cleanup()
	var unverifiable *UnverifiableHostError
	if err := CheckTarget(context.Background(), mustURL(t, "https://resolving.example.invalid/x")); !errors.As(err, &unverifiable) {
		t.Errorf("dns failure not typed UnverifiableHostError: %v", err)
	}
}

type dialCall struct {
	network string
	addr    string
}

func TestPinnedDialResolvesOnceAndFailsOver(t *testing.T) {
	lookups := 0
	cleanup := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		lookups++
		return []net.IP{net.ParseIP("93.184.216.31"), net.ParseIP("93.184.216.32")}, nil
	})
	defer cleanup()
	var calls []dialCall
	fallback := func(_ context.Context, network, addr string) (net.Conn, error) {
		calls = append(calls, dialCall{network, addr})
		if addr == "93.184.216.31:443" {
			return nil, errors.New("refused")
		}
		clientSide, serverSide := net.Pipe()
		_ = serverSide.Close()
		return clientSide, nil
	}
	conn, err := dialPinned(context.Background(), "tcp", "example.test:443", fallback, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if lookups != 1 {
		t.Errorf("lookups = %d, want exactly 1 (no connect-time re-resolution)", lookups)
	}
	if len(calls) != 2 || calls[0].addr != "93.184.216.31:443" || calls[1].addr != "93.184.216.32:443" {
		t.Errorf("failover dialed = %v", calls)
	}
}

func TestPinnedDialBlocksPrivateAnswerWithoutDialing(t *testing.T) {
	cleanup := SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	})
	defer cleanup()
	dialed := false
	_, err := dialPinned(context.Background(), "tcp", "metadata.test:443", func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}, false)
	var blocked *BlockedTargetError
	if !errors.As(err, &blocked) {
		t.Errorf("blocked dial not typed BlockedTargetError: %v", err)
	}
	if dialed {
		t.Error("connection attempted for blocked target")
	}
}

func TestAllowHostForTestBypassesGuard(t *testing.T) {
	undo := AllowHostForTest("127.0.0.1")
	defer undo()
	if err := CheckTarget(context.Background(), mustURL(t, "http://127.0.0.1:9/x")); err != nil {
		t.Errorf("allowed host still blocked: %v", err)
	}
}

func TestShimSurface(t *testing.T) {
	client := NewSSRFSafeClient(2 * time.Second)
	if client.Timeout != 2*time.Second || client.CheckRedirect == nil {
		t.Error("shim lost timeout/redirect policy")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("shim transport is not *http.Transport")
	}
	if transport.Proxy != nil {
		t.Error("shim must stay direct-only (legacy behavior)")
	}
	base := &http.Transport{}
	if WrapTransportWithSSRFCheck(base).DialContext == nil {
		t.Error("wrap did not install pinned dial")
	}
}
