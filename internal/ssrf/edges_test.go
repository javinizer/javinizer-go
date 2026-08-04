package ssrf

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestResolvePublicIPSLiteralAndEmpty(t *testing.T) {
	if _, err := resolvePublicIPs("169.254.169.254"); err == nil {
		t.Error("blocked literal should error")
	}
	var blocked *BlockedTargetError
	if _, err := resolvePublicIPs("169.254.169.254"); !errors.As(err, &blocked) {
		t.Errorf("literal block not typed: %v", err)
	}
	cleanup := SetLookupIPForTest(func(string) ([]net.IP, error) { return nil, nil })
	defer cleanup()
	var unverifiable *UnverifiableHostError
	if _, err := resolvePublicIPs("empty-answer.test"); !errors.As(err, &unverifiable) {
		t.Errorf("empty answer not typed UnverifiableHostError: %v", err)
	}
}

func TestResolvePublicIPsAllowsTestHostWithLiteral(t *testing.T) {
	undo := AllowHostForTest("127.0.0.2")
	defer undo()
	ips, err := resolvePublicIPs("127.0.0.2")
	if err != nil {
		t.Fatalf("allowed host: %v", err)
	}
	if len(ips) == 0 {
		t.Fatal("no ips for allowed literal")
	}
}

func TestDialPinnedBadAddress(t *testing.T) {
	if _, err := dialPinned(context.Background(), "tcp", "no-port", nil); err == nil {
		t.Error("missing port must error")
	}
}

func TestCheckTargetNilAndEmpty(t *testing.T) {
	if err := CheckTarget(context.Background(), nil); err == nil {
		t.Error("nil URL must error")
	}
	if err := CheckTarget(context.Background(), mustURL(t, "https:///nohost")); err == nil {
		t.Error("empty hostname must error")
	}
}

func TestHostIPLiteralZones(t *testing.T) {
	if HostIPLiteral("fe80::1%eth0") == nil {
		t.Error("zoned v6 literal")
	}
	if HostIPLiteral("[::1]") == nil {
		t.Error("bracketed literal")
	}
	if HostIPLiteral("example.com") != nil {
		t.Error("hostname must not parse as literal")
	}
}
