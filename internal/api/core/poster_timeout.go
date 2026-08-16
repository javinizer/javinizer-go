package core

import (
	"context"
	"net"
	"net/http"
	"time"
)

func setPosterHeaderTimeout(client *http.Client, idleTimeout time.Duration) {
	if t, ok := client.Transport.(*http.Transport); ok && idleTimeout > 0 {
		t.ResponseHeaderTimeout = idleTimeout
		t.TLSHandshakeTimeout = idleTimeout
		origDial := t.DialContext
		t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialCtx, cancel := context.WithTimeout(ctx, idleTimeout)
			defer cancel()
			return origDial(dialCtx, network, addr)
		}
	}
}
