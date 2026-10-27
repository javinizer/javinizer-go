package metadatafetch

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchCancellationAfterSuccessfulResolutionNeverDials(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := networkClient(t, networkOptions())
	var lookups, dials atomic.Int32
	c.resolve = func(context.Context, string) ([]net.IP, error) {
		lookups.Add(1)
		cancel()
		return []net.IP{net.ParseIP("203.0.113.17")}, nil
	}
	c.dial = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial after canceled resolution")
	}

	result, err := c.Fetch(ctx, "http://localhost/metadata")
	boundaryZeroResult(t, result)
	if !errors.Is(err, context.Canceled) || lookups.Load() != 1 || dials.Load() != 0 || len(c.slots) != 0 {
		t.Fatalf("err=%v lookups=%d dials=%d occupied=%d", err, lookups.Load(), dials.Load(), len(c.slots))
	}
}

func TestFetchCancellationAfterFailedDialStopsRemainingCandidates(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := networkClient(t, networkOptions())
	c.resolve = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.17"), net.ParseIP("203.0.113.18")}, nil
	}
	var dials atomic.Int32
	c.dial = func(_ context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if network != "tcp" || address != "203.0.113.17:80" {
			t.Errorf("unexpected candidate: %s %s", network, address)
		}
		cancel()
		return nil, errors.New("first candidate failed")
	}

	result, err := c.Fetch(ctx, "http://localhost/metadata")
	boundaryZeroResult(t, result)
	if !errors.Is(err, context.Canceled) || dials.Load() != 1 || len(c.slots) != 0 {
		t.Fatalf("err=%v dials=%d occupied=%d", err, dials.Load(), len(c.slots))
	}
}

func TestFetchSocketDeadlinePreservesTimeoutWithLiveContext(t *testing.T) {
	c := networkClient(t, networkOptions())
	var dials atomic.Int32
	c.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dials.Add(1)
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		// An already-expired socket deadline produces a real net.Error without
		// waiting for (or fabricating) a context deadline.
		if err := client.SetReadDeadline(time.Unix(1, 0)); err != nil {
			return nil, err
		}
		var b [1]byte
		n, err := client.Read(b[:])
		if n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) || ctx.Err() != nil {
			t.Errorf("socket read n=%d err=%v context=%v", n, err, ctx.Err())
		}
		return nil, err
	}

	result, err := c.Fetch(t.Context(), "http://localhost/metadata")
	boundaryZeroResult(t, result)
	if !errors.Is(err, context.DeadlineExceeded) || t.Context().Err() != nil || dials.Load() != 1 || len(c.slots) != 0 {
		t.Fatalf("err=%v parent=%v dials=%d occupied=%d", err, t.Context().Err(), dials.Load(), len(c.slots))
	}
}

type cancelOnCloseConn struct {
	net.Conn
	once   sync.Once
	cancel context.CancelFunc
}

func (c *cancelOnCloseConn) Close() error {
	c.once.Do(c.cancel)
	return c.Conn.Close()
}

func TestFetchRedirectCleanupCancellationPreventsNextHop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options := networkOptions()
	options.Limits.MaxConcurrent = 1
	c := networkClient(t, options)
	var lookups, dials atomic.Int32
	c.resolve = func(context.Context, string) ([]net.IP, error) {
		lookups.Add(1)
		return []net.IP{net.ParseIP("203.0.113.17")}, nil
	}
	served := make(chan struct{})
	var bodyBytes atomic.Int32
	c.dial = func(context.Context, string, string) (net.Conn, error) {
		if dials.Add(1) != 1 {
			return nil, errors.New("unexpected dial after redirect cleanup canceled parent")
		}
		raw := pipeResponse(t, func(conn net.Conn) {
			defer close(served)
			req, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				t.Error(err)
				return
			}
			if !req.Close {
				t.Error("request permits connection reuse")
			}
			if _, err := io.WriteString(conn, "HTTP/1.1 302 Found\r\nConnection: keep-alive\r\nLocation: /next\r\nContent-Length: 4\r\n\r\n"); err != nil {
				t.Error(err)
				return
			}
			// Write separately so header read-ahead cannot consume body bytes.
			n, err := io.WriteString(conn, "body")
			bodyBytes.Store(int32(n))
			if err == nil {
				t.Error("redirect body was read instead of closing its socket")
			}
		})
		return &cancelOnCloseConn{Conn: raw, cancel: cancel}, nil
	}

	result, err := c.Fetch(ctx, "http://localhost/metadata")
	boundaryZeroResult(t, result)
	if !errors.Is(err, context.Canceled) || lookups.Load() != 1 || dials.Load() != 1 || len(c.slots) != 0 {
		t.Fatalf("err=%v lookups=%d dials=%d occupied=%d", err, lookups.Load(), dials.Load(), len(c.slots))
	}
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("redirect peer did not exit after socket cleanup")
	}
	if bodyBytes.Load() != 0 {
		t.Fatalf("redirect body drained %d bytes", bodyBytes.Load())
	}
}
