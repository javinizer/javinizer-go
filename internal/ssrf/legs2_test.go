package ssrf

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestCheckURLInvalidURL(t *testing.T) {
	if err := CheckURL("://bad url"); err == nil {
		t.Error("invalid URL must error")
	}
}

func TestResolvePublicIPsAllowedHostCorners(t *testing.T) {
	undo := AllowHostForTest("empty-answer.example")
	undo2 := AllowHostForTest("literal-bypass.example")
	defer undo2()
	defer undo()
	cleanup := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		if host == "empty-answer.example" {
			return nil, nil
		}
		return nil, errors.New("nope")
	})
	defer cleanup()
	if _, err := resolvePublicIPs(context.Background(), "empty-answer.example"); err == nil {
		t.Error("empty answers must error")
	}
	// Allowed host with failing lookup still surfaces the lookup error.
	if _, err := resolvePublicIPs(context.Background(), "literal-bypass.example"); err == nil {
		t.Error("lookup failure must propagate")
	}
}

func TestResolvePublicIPsAllowedLiteralFallsBackToParse(t *testing.T) {
	undo := AllowHostForTest("127.0.0.5")
	defer undo()
	cleanup := SetLookupIPForTest(func(string) ([]net.IP, error) { return nil, nil })
	defer cleanup()
	ips, err := resolvePublicIPs(context.Background(), "127.0.0.5")
	if err != nil || len(ips) != 1 {
		t.Fatalf("literal fallback failed: %v %v", ips, err)
	}
}

func TestDialPinnedPublicLiteralAndNilBase(t *testing.T) {
	var dialed string
	fallback := func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = addr
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
	if _, err := dialPinned(context.Background(), "tcp", "8.8.8.8:443", fallback, false); err != nil {
		t.Fatal(err)
	}
	if dialed != "8.8.8.8:443" {
		t.Errorf("dialed %q, want the literal (no lookup happened)", dialed)
	}
	if _, err := NewPinnedDialTransport(nil); err != nil {
		t.Fatalf("nil base should pin the default transport: %v", err)
	}
}
