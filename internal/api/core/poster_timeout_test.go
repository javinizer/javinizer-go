package core

import (
	"net/http"
	"testing"
	"time"
)

func TestSetPosterHeaderTimeout_WithTimeout(t *testing.T) {
	transport := &http.Transport{}
	client := &http.Client{Transport: transport}
	setPosterHeaderTimeout(client, 60*time.Second)
	if transport.ResponseHeaderTimeout != 60*time.Second {
		t.Errorf("expected ResponseHeaderTimeout=60s, got %v", transport.ResponseHeaderTimeout)
	}
}

func TestSetPosterHeaderTimeout_ZeroTimeout(t *testing.T) {
	transport := &http.Transport{}
	client := &http.Client{Transport: transport}
	setPosterHeaderTimeout(client, 0)
	if transport.ResponseHeaderTimeout != 0 {
		t.Errorf("expected ResponseHeaderTimeout=0, got %v", transport.ResponseHeaderTimeout)
	}
}

func TestSetPosterHeaderTimeout_NilTransport(t *testing.T) {
	client := &http.Client{Transport: nil}
	setPosterHeaderTimeout(client, 60*time.Second)
}
