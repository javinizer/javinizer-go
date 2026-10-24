package ssrffixture

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

var bridged sync.Map

// NewServer creates a local fixture and bridges its synthetic 8.8.8.8 endpoint.
// Tests must remain serial and finish background HTTP work before cleanup.
func NewServer(t testing.TB, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	original := http.DefaultTransport
	previous, ok := original.(*http.Transport)
	if !ok {
		server.Close()
		t.Fatal("fixture bridge requires HTTP transport")
	}
	transport := previous.Clone()
	dial := previous.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	_, previousBridged := bridged.Load(previous)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	synthetic := net.JoinHostPort("8.8.8.8", port)
	local := server.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == synthetic {
			return dial(ctx, network, local)
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if host == "8.8.8.8" && !previousBridged {
			return nil, fmt.Errorf("unregistered synthetic fixture endpoint: %s", addr)
		}
		return dial(ctx, network, addr)
	}
	bridged.Store(transport, true)
	http.DefaultTransport = transport
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		http.DefaultTransport = original
		bridged.Delete(transport)
		server.Close()
	})
	return server
}
