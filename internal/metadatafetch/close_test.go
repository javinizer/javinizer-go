package metadatafetch

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The fixture deliberately ignores Connection: close. Go 1.27 can auto-drain
// an early-closed response unless Request.Close itself is true, independently
// of Transport.DisableKeepAlives. Write headers separately to distinguish a
// forbidden drain from the transport's normal bounded header read-ahead.
func TestRejectedResponsesCloseWithoutReadingBody(t *testing.T) {
	for _, tc := range []struct {
		name, status, extra string
		maxHops             int
		want                error
	}{
		{"non-success", "500 Server Error", "", 0, ErrResponse},
		{"redirect disabled", "302 Found", "Location: /next\r\n", 0, ErrRedirect},
		{"redirect invalid", "302 Found", "Location: file:///private\r\n", 1, ErrRedirect},
		{"oversized length", "200 OK", "", 0, ErrBodyLimit},
		{"unexpected encoding", "200 OK", "Content-Encoding: gzip\r\n", 0, ErrResponse},
		{"unsolicited partial", "206 Partial Content", "", 0, ErrResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := networkOptions()
			options.Limits.MaxBodyBytes = 4
			options.Limits.MaxConcurrent = 1
			options.Redirects.MaxHops = tc.maxHops
			c := networkClient(t, options)
			consumed := make(chan int, 1)
			c.dial = func(context.Context, string, string) (net.Conn, error) {
				return pipeResponse(t, func(conn net.Conn) {
					req, err := http.ReadRequest(bufio.NewReader(conn))
					if err != nil {
						t.Error(err)
						return
					}
					if !req.Close {
						t.Error("outbound request did not explicitly close")
					}
					io.WriteString(conn, "HTTP/1.1 "+tc.status+"\r\nConnection: keep-alive\r\nContent-Length: 200000\r\n"+tc.extra+"\r\n")
					n, _ := io.WriteString(conn, strings.Repeat("x", 200000))
					consumed <- n
				}), nil
			}
			result, err := c.Fetch(t.Context(), "http://localhost/")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			boundaryZeroResult(t, result)
			select {
			case n := <-consumed:
				if n != 0 {
					t.Fatalf("rejected body drained %d bytes beyond zero body-read budget", n)
				}
			case <-time.After(time.Second):
				t.Fatal("rejected connection not closed")
			}
			if len(c.slots) != 0 {
				t.Fatal("concurrency slot retained after rejection")
			}
		})
	}
}

func TestAllowedRedirectClosesUnconsumedBodyBeforeNextHop(t *testing.T) {
	options := networkOptions()
	options.Limits.MaxBodyBytes = 4
	options.Limits.MaxConcurrent = 1
	c := networkClient(t, options)
	consumed := make(chan int, 1)
	firstClosed := make(chan struct{})
	var calls atomic.Int32
	c.dial = func(context.Context, string, string) (net.Conn, error) {
		call := calls.Add(1)
		if call > 1 {
			select {
			case <-firstClosed:
			default:
				t.Error("next hop started before first socket closed")
			}
		}
		raw := pipeResponse(t, func(conn net.Conn) {
			http.ReadRequest(bufio.NewReader(conn))
			if call == 1 {
				io.WriteString(conn, "HTTP/1.1 302 Found\r\nConnection: keep-alive\r\nLocation: /final\r\nContent-Length: 200000\r\n\r\n")
				n, _ := io.WriteString(conn, strings.Repeat("x", 200000))
				consumed <- n
				return
			}
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		})
		if call == 1 {
			return &closeObservedConn{Conn: raw, closed: firstClosed}, nil
		}
		return raw, nil
	}
	result, err := c.Fetch(t.Context(), "http://localhost/")
	if err != nil || string(result.Bytes) != "ok" || calls.Load() != 2 || len(c.slots) != 0 {
		t.Fatalf("result=%+v err=%v calls=%d occupied=%d", result, err, calls.Load(), len(c.slots))
	}
	select {
	case n := <-consumed:
		if n != 0 {
			t.Fatalf("redirect drained %d bytes", n)
		}
	case <-time.After(time.Second):
		t.Fatal("redirect connection not closed")
	}
}

func TestChunkedOverflowDoesNotDrainRemainder(t *testing.T) {
	options := networkOptions()
	options.Limits.MaxBodyBytes = 4
	options.Limits.MaxConcurrent = 1
	c := networkClient(t, options)
	consumed := make(chan int, 1)
	c.dial = func(context.Context, string, string) (net.Conn, error) {
		return pipeResponse(t, func(conn net.Conn) {
			http.ReadRequest(bufio.NewReader(conn))
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nConnection: keep-alive\r\nTransfer-Encoding: chunked\r\n\r\n")
			io.WriteString(conn, strconv.FormatInt(200000, 16)+"\r\n")
			// Feed exactly the documented overflow probe, then measure only the
			// remainder. The final block must not be read by an implicit drain.
			io.WriteString(conn, "12345")
			n, _ := io.WriteString(conn, strings.Repeat("x", 199995))
			consumed <- n
		}), nil
	}
	result, err := c.Fetch(t.Context(), "http://localhost/")
	if !errors.Is(err, ErrBodyLimit) || len(c.slots) != 0 {
		t.Fatalf("err=%v occupied=%d", err, len(c.slots))
	}
	boundaryZeroResult(t, result)
	select {
	case n := <-consumed:
		if n != 0 {
			t.Fatalf("overflow drained %d additional payload bytes", n)
		}
	case <-time.After(time.Second):
		t.Fatal("overflow connection not closed")
	}
}
