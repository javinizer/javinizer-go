package core

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestSetPosterHeaderTimeout_WithDialContext(t *testing.T) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
	}
	client := &http.Client{Transport: transport}
	setPosterHeaderTimeout(client, 10*time.Second)
	if transport.ResponseHeaderTimeout != 10*time.Second {
		t.Errorf("expected ResponseHeaderTimeout=10s, got %v", transport.ResponseHeaderTimeout)
	}
	if transport.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("expected TLSHandshakeTimeout=10s, got %v", transport.TLSHandshakeTimeout)
	}
	if transport.DialContext == nil {
		t.Error("DialContext should not be nil after wrapping")
	}
	ctx := context.Background()
	_, _ = transport.DialContext(ctx, "tcp", "localhost:1")
}

func TestSetPosterHeaderTimeout_NilDialContext(t *testing.T) {
	transport := &http.Transport{}
	client := &http.Client{Transport: transport}
	setPosterHeaderTimeout(client, 10*time.Second)
	if transport.DialContext == nil {
		t.Error("DialContext should be set when originally nil")
	}
}
