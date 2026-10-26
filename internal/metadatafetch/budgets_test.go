package metadatafetch

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/imagevalidate"
)

func TestBudgetsCancelEachBlockingNetworkPhase(t *testing.T) {
	for _, phase := range []string{"dns", "connect", "socks", "tls", "headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			options := networkOptions()
			options.Limits.RequestTimeout = 150 * time.Millisecond
			options.Limits.ConnectTimeout = 50 * time.Millisecond
			options.Limits.HeaderTimeout = 50 * time.Millisecond
			options.Limits.TLSHandshakeTimeout = 50 * time.Millisecond
			if phase == "socks" {
				options.Route = SOCKS5
				options.ProxyURL = "socks5://proxy.test:1080"
			}
			c := networkClient(t, options)
			closed := make(chan struct{})
			if phase == "dns" {
				c.resolve = func(ctx context.Context, _ string) ([]net.IP, error) { <-ctx.Done(); return nil, ctx.Err() }
			} else if phase == "connect" {
				c.dial = func(ctx context.Context, _, _ string) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }
			} else {
				c.dial = func(_ context.Context, _, _ string) (net.Conn, error) {
					return pipeResponse(t, func(conn net.Conn) {
						defer close(closed)
						if phase == "socks" || phase == "tls" {
							io.Copy(io.Discard, conn)
							return
						}
						if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
							t.Error(err)
							return
						}
						if phase == "body" {
							io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\na")
						}
						io.Copy(io.Discard, conn)
					}), nil
				}
			}
			rawURL := "http://localhost/"
			if phase == "tls" {
				rawURL = "https://localhost/"
			}
			result, err := c.Fetch(t.Context(), rawURL)
			if !errors.Is(err, context.DeadlineExceeded) || result.Bytes != nil || len(c.slots) != 0 {
				t.Fatalf("result=%+v err=%v occupied=%d", result, err, len(c.slots))
			}
			if phase == "socks" || phase == "tls" || phase == "headers" || phase == "body" {
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("timed out connection was not closed")
				}
			}
		})
	}
}

func TestEarlierDeadlineAndPreCanceledRequests(t *testing.T) {
	c := networkClient(t, networkOptions())
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	want, _ := ctx.Deadline()
	c.resolve = func(ctx context.Context, _ string) ([]net.IP, error) {
		got, ok := ctx.Deadline()
		if !ok || got.After(want) {
			t.Errorf("DNS extends deadline: %v > %v", got, want)
		}
		return []net.IP{net.ParseIP("203.0.113.17")}, nil
	}
	var dials atomic.Int32
	c.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dials.Add(1)
		got, ok := ctx.Deadline()
		if !ok || got.After(want) {
			t.Errorf("connect extends deadline: %v > %v", got, want)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if _, err := c.Fetch(ctx, "http://localhost/"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	cancelled, cancelNow := context.WithCancel(t.Context())
	cancelNow()
	c.resolve = func(context.Context, string) ([]net.IP, error) { t.Error("pre-canceled DNS"); return nil, nil }
	if _, err := c.Fetch(cancelled, "http://localhost/"); !errors.Is(err, context.Canceled) || dials.Load() != 1 {
		t.Fatalf("err=%v dials=%d", err, dials.Load())
	}
}

func TestFailedFirstAddressRetainsSharedBudgetForFailover(t *testing.T) {
	options := networkOptions()
	options.Limits.ConnectTimeout = 120 * time.Millisecond
	c := networkClient(t, options)
	c.resolve = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.17"), net.ParseIP("203.0.113.18")}, nil
	}
	var calls atomic.Int32
	var firstDeadline time.Time
	c.dial = func(ctx context.Context, _, address string) (net.Conn, error) {
		call := calls.Add(1)
		if call == 1 {
			if address != "203.0.113.17:80" {
				t.Errorf("first target=%q", address)
			}
			firstDeadline, _ = ctx.Deadline()
			<-ctx.Done()
			return nil, ctx.Err()
		}
		deadline, _ := ctx.Deadline()
		if call != 2 || address != "203.0.113.18:80" || !deadline.After(firstDeadline) {
			t.Errorf("invalid failover: %d %s %s", call, address, deadline)
		}
		return pipeResponse(t, func(conn net.Conn) {
			http.ReadRequest(bufio.NewReader(conn))
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}), nil
	}
	result, err := c.Fetch(t.Context(), "http://localhost/")
	if err != nil || string(result.Bytes) != "ok" || calls.Load() != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestConnectBudgetDoesNotOwnResponseBody(t *testing.T) {
	options := networkOptions()
	options.Limits.ConnectTimeout = 10 * time.Millisecond
	c := networkClient(t, options)
	c.dial = func(_ context.Context, _, _ string) (net.Conn, error) {
		return pipeResponse(t, func(conn net.Conn) {
			http.ReadRequest(bufio.NewReader(conn))
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n")
			timer := time.NewTimer(40 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
			io.WriteString(conn, "ok")
		}), nil
	}
	result, err := c.Fetch(t.Context(), "http://localhost/")
	if err != nil || string(result.Bytes) != "ok" {
		t.Fatalf("body killed by dial budget: %+v %v", result, err)
	}
}

func TestRedirectChainDoesNotRestartRequestBudget(t *testing.T) {
	options := networkOptions()
	options.Limits.RequestTimeout = 60 * time.Millisecond
	options.Redirects.MaxHops = 20
	c := networkClient(t, options)
	var calls atomic.Int32
	c.dial = func(_ context.Context, _, _ string) (net.Conn, error) {
		calls.Add(1)
		return pipeResponse(t, func(conn net.Conn) {
			http.ReadRequest(bufio.NewReader(conn))
			timer := time.NewTimer(20 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
			io.WriteString(conn, "HTTP/1.1 302 Found\r\nLocation: /again\r\nContent-Length: 0\r\n\r\n")
		}), nil
	}
	result, err := c.Fetch(t.Context(), "http://localhost/")
	if !errors.Is(err, context.DeadlineExceeded) || result.Bytes != nil || calls.Load() > 4 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestConcurrentAdmissionAndCancellationRelease(t *testing.T) {
	options := networkOptions()
	options.Limits.MaxConcurrent = 1
	c := networkClient(t, options)
	started := make(chan struct{})
	var calls atomic.Int32
	c.dial = func(_ context.Context, _, _ string) (net.Conn, error) {
		call := calls.Add(1)
		return pipeResponse(t, func(conn net.Conn) {
			http.ReadRequest(bufio.NewReader(conn))
			if call == 1 {
				close(started)
				io.Copy(io.Discard, conn)
				return
			}
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}), nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := c.Fetch(ctx, "http://localhost/"); first <- err }()
	<-started
	result, err := c.Fetch(t.Context(), "http://localhost/")
	if !errors.Is(err, ErrBusy) || result.Bytes != nil || calls.Load() != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	result, err = c.Fetch(t.Context(), "http://localhost/")
	if err != nil || string(result.Bytes) != "ok" || len(c.slots) != 0 || calls.Load() != 2 {
		t.Fatalf("slot not released: %+v %v", result, err)
	}
}

func TestFetchImageComposesFullValidationAndReleasesSlot(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 2, 3))
	im.Set(1, 1, color.RGBA{R: 255, A: 255})
	var pngBytes, jpegBytes bytes.Buffer
	if err := png.Encode(&pngBytes, im); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegBytes, im, nil); err != nil {
		t.Fatal(err)
	}
	webp, err := os.ReadFile("../imagevalidate/testdata/gopher.webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		body   []byte
		limits imagevalidate.Limits
		want   error
	}{
		{"png", pngBytes.Bytes(), imagevalidate.Limits{MaxEncodedBytes: 1 << 20, MaxDimension: 1000, MaxPixels: 1 << 20}, nil},
		{"jpeg", jpegBytes.Bytes(), imagevalidate.Limits{MaxEncodedBytes: 1 << 20, MaxDimension: 1000, MaxPixels: 1 << 20}, nil},
		{"webp", webp, imagevalidate.Limits{MaxEncodedBytes: 1 << 20, MaxDimension: 1000, MaxPixels: 1 << 20}, nil},
		{"disguised HTML", []byte("<html>not an image</html>"), imagevalidate.Limits{MaxEncodedBytes: 1024, MaxDimension: 10, MaxPixels: 100}, ErrImage},
		{"truncated png", pngBytes.Bytes()[:pngBytes.Len()-10], imagevalidate.Limits{MaxEncodedBytes: 1024, MaxDimension: 10, MaxPixels: 100}, ErrImage},
		{"dimension", pngBytes.Bytes(), imagevalidate.Limits{MaxEncodedBytes: 1024, MaxDimension: 2, MaxPixels: 100}, ErrImage},
		{"pixels", pngBytes.Bytes(), imagevalidate.Limits{MaxEncodedBytes: 1024, MaxDimension: 10, MaxPixels: 5}, ErrImage},
		{"image encoded bytes", pngBytes.Bytes(), imagevalidate.Limits{MaxEncodedBytes: 5, MaxDimension: 10, MaxPixels: 100}, ErrBodyLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := networkOptions()
			options.Limits.MaxBodyBytes = 1 << 20
			options.Limits.MaxConcurrent = 1
			c := networkClient(t, options)
			c.dial = func(_ context.Context, _, _ string) (net.Conn, error) {
				return pipeResponse(t, func(conn net.Conn) {
					http.ReadRequest(bufio.NewReader(conn))
					io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: "+strconv.Itoa(len(tc.body))+"\r\n\r\n")
					conn.Write(tc.body)
				}), nil
			}
			result, err := c.FetchImage(t.Context(), "http://localhost/image", tc.limits)
			if !errors.Is(err, tc.want) || len(c.slots) != 0 {
				t.Fatalf("err=%v want=%v occupied=%d", err, tc.want, len(c.slots))
			}
			if tc.want == nil && !bytes.Equal(result.Bytes, tc.body) || tc.want != nil && result.Bytes != nil {
				t.Fatalf("unexpected image receipt: %+v", result)
			}
		})
	}
	c := networkClient(t, networkOptions())
	if _, err := c.FetchImage(t.Context(), "http://localhost/image", imagevalidate.Limits{}); !errors.Is(err, ErrOptions) {
		t.Fatalf("bad image limits: %v", err)
	}
}
