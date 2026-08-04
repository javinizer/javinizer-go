package ssrf

import (
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
	if _, err := resolvePublicIPs("empty-answer.example"); err == nil {
		t.Error("empty answers must error")
	}
	// Allowed host with failing lookup still surfaces the lookup error.
	if _, err := resolvePublicIPs("literal-bypass.example"); err == nil {
		t.Error("lookup failure must propagate")
	}
}

func TestResolvePublicIPsAllowedLiteralFallsBackToParse(t *testing.T) {
	undo := AllowHostForTest("127.0.0.5")
	defer undo()
	cleanup := SetLookupIPForTest(func(string) ([]net.IP, error) { return nil, nil })
	defer cleanup()
	ips, err := resolvePublicIPs("127.0.0.5")
	if err != nil || len(ips) != 1 {
		t.Fatalf("literal fallback failed: %v %v", ips, err)
	}
}
