package ssrffixture

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestBridgeOnlyDialsRegisteredLocalFixtures(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	marker := errors.New("local dial probe")
	var dials []string
	base := &http.Transport{DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
		dials = append(dials, addr)
		return nil, marker
	}}
	http.DefaultTransport = base
	t.Run("nested fixtures", func(t *testing.T) {
		first := NewServer(t, http.NotFoundHandler())
		second := NewServer(t, http.NotFoundHandler())
		transport := http.DefaultTransport.(*http.Transport)
		for _, local := range []string{first.Listener.Addr().String(), second.Listener.Addr().String()} {
			_, port, err := net.SplitHostPort(local)
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.DialContext(t.Context(), "tcp", net.JoinHostPort("8.8.8.8", port))
			if !errors.Is(err, marker) || dials[len(dials)-1] != local {
				t.Fatalf("bridge err=%v dials=%v want=%s", err, dials, local)
			}
		}
		_, err := transport.DialContext(t.Context(), "tcp", "8.8.8.8:0")
		if err == nil || len(dials) != 2 {
			t.Fatalf("unregistered endpoint escaped bridge: %v %v", err, dials)
		}
	})
	if http.DefaultTransport != base {
		t.Fatal("fixture cleanup did not restore transport")
	}
}
