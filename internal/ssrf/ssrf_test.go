package ssrf

import (
	"net"
	"testing"
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
