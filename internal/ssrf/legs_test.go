package ssrf

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestNewPinnedDialTransportRejectsTLSDialers(t *testing.T) {
	tlsDialer := &http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) { return nil, nil }}
	if _, err := NewPinnedDialTransport(tlsDialer); err == nil {
		t.Error("DialTLSContext transport must be rejected: it bypasses DialContext pinning for HTTPS")
	}
	legacy := &http.Transport{}
	legacy.DialTLS = func(network, addr string) (net.Conn, error) { return nil, nil }
	if _, err := NewPinnedDialTransport(legacy); err == nil {
		t.Error("DialTLS transport must be rejected")
	}
}

func TestNewPinnedDialTransportExoticDefault(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) { return nil, errors.New("nope") })
	if _, err := NewPinnedDialTransport(nil); err == nil {
		t.Error("exotic http.DefaultTransport must fail closed")
	}
	// The client shim must still return a client with hardened transport.
	client := NewSSRFSafeClient(0)
	if client == nil || client.Transport == nil {
		t.Fatal("shim must never construct a client without its guard")
	}
	if _, ok := client.Transport.(*http.Transport); !ok {
		t.Fatalf("shim transport %T is not pinned *http.Transport", client.Transport)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBlockedTargetErrorFormats(t *testing.T) {
	if got := (&BlockedTargetError{Target: "x.test"}).Error(); !contains(got, "x.test") {
		t.Errorf("empty-reason message = %q", got)
	}
	if got := (&BlockedTargetError{Target: "x.test", Reason: "loopback"}).Error(); !contains(got, "loopback") {
		t.Errorf("reason message = %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestIsBlockedHostLiterals(t *testing.T) {
	if !IsBlockedHost("10.0.0.5") {
		t.Error("rfc1918 literal must be blocked")
	}
	if IsBlockedHost("8.8.8.8") {
		t.Error("public literal must be allowed")
	}
	if !IsBlockedHost("sub.localhost") {
		t.Error("*.localhost must be blocked")
	}
}

func TestAllowedHostLookupFailurePropagatesRaw(t *testing.T) {
	undo := AllowHostForTest("probe.invalid")
	defer undo()
	cleanup := SetLookupIPForTest(func(string) ([]net.IP, error) { return nil, errors.New("lookup boom") })
	defer cleanup()
	_, err := resolvePublicIPs("probe.invalid")
	if err == nil || err.Error() != "lookup boom" {
		t.Errorf("allowed-host lookup error must propagate raw, got %v", err)
	}
}
