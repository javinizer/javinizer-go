package metadatafetch

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func boundaryOptions() Options {
	return Options{
		Route: Direct,
		Limits: Limits{
			RequestTimeout: 5 * time.Second, ConnectTimeout: time.Second,
			HeaderTimeout: time.Second, TLSHandshakeTimeout: time.Second,
			MaxHeaderBytes: 4096, MaxBodyBytes: 16, MaxConcurrent: 2,
		},
		Redirects: RedirectPolicy{MaxHops: 2},
	}
}

func TestBoundaryConstructorRejectsUnsafeOptions(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Options)
	}{
		{"unspecified route", func(o *Options) { o.Route = "" }},
		{"http proxy route", func(o *Options) { o.Route = "http" }},
		{"https proxy route", func(o *Options) { o.Route = "https" }},
		{"socks5h route", func(o *Options) { o.Route = "socks5h" }},
		{"browser route", func(o *Options) { o.Route = "browser" }},
		{"solver route", func(o *Options) { o.Route = "flaresolverr" }},
		{"unknown route", func(o *Options) { o.Route = "unknown" }},
		{"direct with proxy", func(o *Options) { o.ProxyURL = "socks5://proxy.test:1080" }},
		{"zero total", func(o *Options) { o.Limits.RequestTimeout = 0 }},
		{"negative total", func(o *Options) { o.Limits.RequestTimeout = -1 }},
		{"zero connect", func(o *Options) { o.Limits.ConnectTimeout = 0 }},
		{"zero headers timeout", func(o *Options) { o.Limits.HeaderTimeout = 0 }},
		{"zero TLS timeout", func(o *Options) { o.Limits.TLSHandshakeTimeout = 0 }},
		{"zero headers bytes", func(o *Options) { o.Limits.MaxHeaderBytes = 0 }},
		{"zero body bytes", func(o *Options) { o.Limits.MaxBodyBytes = 0 }},
		{"negative body bytes", func(o *Options) { o.Limits.MaxBodyBytes = -1 }},
		{"body overflow", func(o *Options) { o.Limits.MaxBodyBytes = math.MaxInt64 }},
		{"zero concurrency", func(o *Options) { o.Limits.MaxConcurrent = 0 }},
		{"excessive concurrency", func(o *Options) { o.Limits.MaxConcurrent = 1025 }},
		{"negative redirects", func(o *Options) { o.Redirects.MaxHops = -1 }},
		{"excessive redirects", func(o *Options) { o.Redirects.MaxHops = 21 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := boundaryOptions()
			test.change(&options)
			client, err := New(options)
			if client != nil || !errors.Is(err, ErrOptions) {
				t.Fatalf("client=%v err=%v", client, err)
			}
		})
	}
	for _, raw := range []string{
		"", "http://user:secret@proxy.test:1080", "https://proxy.test:1080", "socks5h://proxy.test:1080",
		"socks5://proxy.test", "socks5://proxy.test:0", "socks5://proxy.test:65536", "socks5://proxy.test:bad",
		"socks5://:1080", "socks5://proxy.test:1080/", "socks5://proxy.test:1080?token=secret",
		"socks5://proxy.test:1080?", "socks5://proxy.test:1080#secret", "socks5://:secret@proxy.test:1080",
		"socks5://" + strings.Repeat("u", 256) + ":secret@proxy.test:1080", "socks5://user:" + strings.Repeat("p", 256) + "@proxy.test:1080",
		"socks5://[fe80::1%25eth0]:1080", "socks5:proxy.test:1080", "%",
	} {
		t.Run("proxy "+raw, func(t *testing.T) {
			options := boundaryOptions()
			options.Route, options.ProxyURL = SOCKS5, raw
			client, err := New(options)
			if client != nil || !errors.Is(err, ErrOptions) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("client=%v err=%v", client, err)
			}
		})
	}
	for _, route := range []Route{Direct, SOCKS5} {
		options := boundaryOptions()
		options.Route = route
		if route == SOCKS5 {
			options.ProxyURL = "socks5://user:secret@proxy.test:1080"
		}
		if client, err := New(options); err != nil || client == nil {
			t.Fatalf("valid route %s: client=%v err=%v", route, client, err)
		}
	}
}

func TestBoundaryInvalidURLsNeverResolveOrDial(t *testing.T) {
	for _, raw := range []string{
		"", "%", "http:", "http://", "//localhost/path", "/relative", "file:///tmp/file", "ftp://localhost/file",
		"http://user:secret@localhost/", "http://localhost/#fragment", "http://localhost:0/", "http://localhost:65536/",
		"http://localhost:bad/", "http://localhost:/", "http://:80/", "http://[::1", "http://[fe80::1%25eth0]/",
		"http:opaque", "http://localhost/\nheader", "http://lócálhost/",
	} {
		t.Run(raw, func(t *testing.T) {
			client, err := New(boundaryOptions())
			if err != nil {
				t.Fatal(err)
			}
			client.resolve = func(context.Context, string) ([]net.IP, error) { t.Error("unexpected lookup"); return nil, nil }
			client.dial = func(context.Context, string, string) (net.Conn, error) {
				t.Error("unexpected dial")
				return nil, errors.New("unexpected")
			}
			result, err := client.Fetch(t.Context(), raw)
			boundaryZeroResult(t, result)
			if !errors.Is(err, ErrURL) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err=%v", err)
			}
		})
	}
	var nilClient *Client
	result, err := nilClient.Fetch(t.Context(), "http://localhost/")
	boundaryZeroResult(t, result)
	if !errors.Is(err, ErrOptions) {
		t.Fatalf("nil receiver err=%v", err)
	}
	client, _ := New(boundaryOptions())
	result, err = client.Fetch(nil, "http://localhost/")
	boundaryZeroResult(t, result)
	if !errors.Is(err, ErrOptions) {
		t.Fatalf("nil context err=%v", err)
	}
}

func TestBoundaryRejectedDNSAnswersNeverDial(t *testing.T) {
	public := net.ParseIP("203.0.113.7")
	tests := []struct {
		name string
		ips  []net.IP
		err  error
	}{
		{"empty", nil, nil}, {"nil IP", []net.IP{nil}, nil}, {"malformed IP", []net.IP{{1, 2, 3}}, nil},
		{"DNS failure", nil, errors.New("user:secret@resolver")},
	}
	for _, raw := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.1.2", "192.168.3.4", "fc00::1", "fe80::1", "169.254.1.2", "0.0.0.0", "::", "::ffff:127.0.0.1", "224.0.0.1", "239.1.2.3", "ff02::1", "255.255.255.255", "100.64.1.2", "100.127.255.255"} {
		ip := net.ParseIP(raw)
		tests = append(tests, struct {
			name string
			ips  []net.IP
			err  error
		}{raw, []net.IP{ip}, nil})
		tests = append(tests, struct {
			name string
			ips  []net.IP
			err  error
		}{"mixed " + raw, []net.IP{public, ip}, nil})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _ := New(boundaryOptions())
			lookups, dials := 0, 0
			client.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
				lookups++
				if host != "localhost" {
					t.Errorf("lookup host=%q", host)
				}
				return test.ips, test.err
			}
			client.dial = func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, errors.New("unexpected dial")
			}
			result, err := client.Fetch(t.Context(), "http://localhost/?token=secret")
			boundaryZeroResult(t, result)
			if !errors.Is(err, ErrTarget) || lookups != 1 || dials != 0 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err=%v lookups=%d dials=%d", err, lookups, dials)
			}
		})
	}
}

func TestBoundaryPrivateLiteralsNeverResolveOrDial(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "10.0.0.1", "169.254.169.254", "100.64.0.1"} {
		t.Run(host, func(t *testing.T) {
			client, _ := New(boundaryOptions())
			client.resolve = func(context.Context, string) ([]net.IP, error) { t.Error("unexpected lookup"); return nil, nil }
			client.dial = func(context.Context, string, string) (net.Conn, error) {
				t.Error("unexpected dial")
				return nil, errors.New("unexpected")
			}
			result, err := client.Fetch(t.Context(), "http://"+host+"/")
			boundaryZeroResult(t, result)
			if !errors.Is(err, ErrTarget) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestBoundaryProxyPolicySeparatesTargetAndEndpoint(t *testing.T) {
	for _, test := range []struct {
		name       string
		targetIP   string
		proxyIP    string
		wantLookup []string
		wantDial   []string
		wantErr    error
	}{
		{"private target", "10.0.0.1", "203.0.113.8", []string{"localhost"}, nil, ErrTarget},
		{"private proxy", "203.0.113.7", "127.0.0.1", []string{"localhost", "proxy.test"}, nil, ErrTarget},
		{"proxy failure no direct fallback", "203.0.113.7", "203.0.113.8", []string{"localhost", "proxy.test"}, []string{"203.0.113.8:1080"}, ErrConnect},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := boundaryOptions()
			options.Route, options.ProxyURL = SOCKS5, "socks5://user:secret@proxy.test:1080"
			client, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			var lookups, dials []string
			client.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
				lookups = append(lookups, host)
				switch host {
				case "localhost":
					return []net.IP{net.ParseIP(test.targetIP)}, nil
				case "proxy.test":
					return []net.IP{net.ParseIP(test.proxyIP)}, nil
				default:
					t.Errorf("unexpected lookup %q", host)
					return nil, errors.New("unexpected")
				}
			}
			client.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				dials = append(dials, address)
				return nil, errors.New("user:secret@proxy refused")
			}
			result, err := client.Fetch(t.Context(), "http://localhost/?token=secret")
			boundaryZeroResult(t, result)
			if !errors.Is(err, test.wantErr) || !reflect.DeepEqual(lookups, test.wantLookup) || !reflect.DeepEqual(dials, test.wantDial) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err=%v lookups=%v dials=%v", err, lookups, dials)
			}
		})
	}
}

type boundaryState struct {
	mu       sync.Mutex
	lookups  []string
	dials    []string
	requests []*http.Request
	servers  []net.Conn
	workers  sync.WaitGroup
}

func boundaryFixture(t *testing.T, options Options, responses ...string) (*Client, *boundaryState) {
	t.Helper()
	client, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	state := &boundaryState{}
	client.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
		state.mu.Lock()
		state.lookups = append(state.lookups, host)
		state.mu.Unlock()
		if !strings.EqualFold(host, "localhost") && !strings.EqualFold(host, "localhost.") {
			return nil, errors.New("unexpected DNS name")
		}
		return []net.IP{net.ParseIP("203.0.113.7")}, nil
	}
	client.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		state.mu.Lock()
		index := len(state.dials)
		state.dials = append(state.dials, address)
		if index >= len(responses) || address != "203.0.113.7:80" || network != "tcp" {
			state.mu.Unlock()
			return nil, fmt.Errorf("unexpected fixture dial %s/%s index %d", network, address, index)
		}
		peer, server := net.Pipe()
		state.servers = append(state.servers, server)
		state.workers.Add(1)
		state.mu.Unlock()
		go func() {
			defer state.workers.Done()
			defer server.Close()
			request, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				t.Errorf("fixture request: %v", err)
				return
			}
			defer request.Body.Close()
			state.mu.Lock()
			state.requests = append(state.requests, request)
			state.mu.Unlock()
			// The peer may intentionally close before consuming oversized bodies.
			_, _ = io.WriteString(server, responses[index])
		}()
		return peer, nil
	}
	t.Cleanup(func() {
		state.mu.Lock()
		for _, server := range state.servers {
			_ = server.Close()
		}
		state.mu.Unlock()
		state.workers.Wait()
	})
	return client, state
}

func boundaryZeroResult(t *testing.T, result Result) {
	t.Helper()
	if result.Bytes != nil || result.FinalURL != "" || result.SHA256 != [sha256.Size]byte{} {
		t.Fatalf("partial result returned: %+v", result)
	}
}

func TestBoundaryOwnedBytesDigestAndRequestIdentity(t *testing.T) {
	response := "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ndata"
	client, state := boundaryFixture(t, boundaryOptions(), response, response)
	first, err := client.Fetch(t.Context(), "http://localhost/path?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Bytes) != "data" || first.SHA256 != sha256.Sum256([]byte("data")) || first.FinalURL != "http://localhost/path?token=secret" {
		t.Fatalf("result=%+v", first)
	}
	first.Bytes[0] = 'X'
	second, err := client.Fetch(t.Context(), "http://localhost/path?token=secret")
	if err != nil || string(second.Bytes) != "data" {
		t.Fatalf("second result=%+v err=%v", second, err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.lookups) != 2 || len(state.dials) != 2 || len(state.requests) != 2 {
		t.Fatalf("lookups=%v dials=%v requests=%d", state.lookups, state.dials, len(state.requests))
	}
	for _, request := range state.requests {
		if request.Method != http.MethodGet || request.Host != "localhost" || request.Header.Get("Accept-Encoding") != "identity" || request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" || request.Header.Get("Referer") != "" {
			t.Fatalf("unexpected request: %+v", request)
		}
	}
}

func TestBoundaryResponseBudgetsAndFraming(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		want     string
		wantErr  error
	}{
		{"exact length", "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ndata", "data", nil},
		{"identity encoding", "HTTP/1.1 200 OK\r\nContent-Encoding: Identity\r\nContent-Length: 4\r\n\r\ndata", "data", nil},
		{"empty success", "HTTP/1.1 204 No Content\r\n\r\n", "", nil},
		{"unknown length exact", "HTTP/1.0 200 OK\r\n\r\ndata", "data", nil},
		{"unknown length overflow", "HTTP/1.0 200 OK\r\n\r\ndata!", "", ErrBodyLimit},
		{"oversized declared length", "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\ndata!", "", ErrBodyLimit},
		{"chunk exact", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\ndata\r\n0\r\n\r\n", "data", nil},
		{"chunk overflow", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\ndata!\r\n0\r\n\r\n", "", ErrBodyLimit},
		{"truncated length", "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ndat", "", ErrResponse},
		{"truncated chunk", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\ndat", "", ErrResponse},
		{"malformed chunk", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nnope\r\n", "", ErrResponse},
		{"gzip unsolicited", "HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"empty encoding", "HTTP/1.1 200 OK\r\nContent-Encoding:\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"multiple encodings", "HTTP/1.1 200 OK\r\nContent-Encoding: identity\r\nContent-Encoding: gzip\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"comma encodings", "HTTP/1.1 200 OK\r\nContent-Encoding: identity, gzip\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"server error", "HTTP/1.1 500 Internal Server Error\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"not found", "HTTP/1.1 404 Not Found\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"not modified", "HTTP/1.1 304 Not Modified\r\n\r\n", "", ErrResponse},
		{"unsolicited partial content", "HTTP/1.1 206 Partial Content\r\nContent-Range: bytes 0-3/8\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"ambiguous length", "HTTP/1.1 200 OK\r\nContent-Length: 3\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
		{"unsupported transfer encoding", "HTTP/1.1 200 OK\r\nTransfer-Encoding: gzip\r\n\r\ndata", "", ErrResponse},
		{"oversized header", "HTTP/1.1 200 OK\r\nX-Padding: " + strings.Repeat("x", 5000) + "\r\nContent-Length: 4\r\n\r\ndata", "", ErrResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := boundaryOptions()
			options.Limits.MaxBodyBytes = 4
			client, state := boundaryFixture(t, options, test.response)
			result, err := client.Fetch(t.Context(), "http://localhost/")
			if test.wantErr != nil {
				boundaryZeroResult(t, result)
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("err=%v want=%v", err, test.wantErr)
				}
			} else if err != nil || string(result.Bytes) != test.want || result.SHA256 != sha256.Sum256([]byte(test.want)) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if len(state.dials) != 1 || len(state.requests) != 1 {
				t.Fatalf("dials=%v requests=%d", state.dials, len(state.requests))
			}
		})
	}
}

func TestBoundaryRedirectAdmission(t *testing.T) {
	final := "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ndata"
	for _, test := range []struct {
		name       string
		location   string
		cross      bool
		wantURL    string
		wantErr    error
		wantDials  int
		wantLookup int
	}{
		{"relative", "/final", false, "http://localhost/final", nil, 2, 2},
		{"same origin", "http://localhost/final", false, "http://localhost/final", nil, 2, 2},
		{"cross origin denied", "http://localhost./final", false, "", ErrRedirect, 1, 1},
		{"cross origin opted in", "http://localhost./final", true, "http://localhost./final", nil, 2, 2},
		{"private redirect opted in", "http://127.0.0.1/final", true, "", ErrTarget, 1, 1},
		{"private redirect default", "http://127.0.0.1/final", false, "", ErrRedirect, 1, 1},
		{"unsupported scheme", "file:///tmp/file", true, "", ErrRedirect, 1, 1},
		{"userinfo", "http://user:secret@localhost/final", true, "", ErrRedirect, 1, 1},
		{"malformed", "http://[", true, "", ErrRedirect, 1, 1},
		{"fragment", "/final#fragment", false, "", ErrRedirect, 1, 1},
		{"empty location", "", false, "", ErrRedirect, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := boundaryOptions()
			options.Redirects.AllowCrossOrigin = test.cross
			redirect := "HTTP/1.1 302 Found\r\nLocation: " + test.location + "\r\nContent-Length: 0\r\n\r\n"
			client, state := boundaryFixture(t, options, redirect, final)
			result, err := client.Fetch(t.Context(), "http://localhost/start")
			if test.wantErr != nil {
				boundaryZeroResult(t, result)
				if !errors.Is(err, test.wantErr) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("err=%v want=%v", err, test.wantErr)
				}
			} else if err != nil || result.FinalURL != test.wantURL || string(result.Bytes) != "data" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if len(state.dials) != test.wantDials || len(state.lookups) != test.wantLookup {
				t.Fatalf("dials=%v lookups=%v", state.dials, state.lookups)
			}
			for _, request := range state.requests {
				if request.Header.Get("Referer") != "" {
					t.Errorf("forwarded referer=%q", request.Header.Get("Referer"))
				}
			}
		})
	}
}

func TestBoundaryRedirectCountsAndHeaderRules(t *testing.T) {
	redirect := "HTTP/1.1 302 Found\r\nLocation: /again\r\nContent-Length: 0\r\n\r\n"
	for _, max := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(max), func(t *testing.T) {
			options := boundaryOptions()
			options.Redirects.MaxHops = max
			client, state := boundaryFixture(t, options, redirect, redirect, redirect)
			result, err := client.Fetch(t.Context(), "http://localhost/")
			boundaryZeroResult(t, result)
			if !errors.Is(err, ErrRedirect) {
				t.Fatalf("err=%v", err)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if len(state.dials) != max+1 || len(state.lookups) != max+1 {
				t.Fatalf("dials=%v lookups=%v", state.dials, state.lookups)
			}
		})
	}
	for _, header := range []string{"", "Location: /one\r\nLocation: /two\r\n", "Location: /one\r\nContent-Encoding: gzip\r\n"} {
		t.Run(header, func(t *testing.T) {
			client, state := boundaryFixture(t, boundaryOptions(), "HTTP/1.1 302 Found\r\n"+header+"Content-Length: 0\r\n\r\n")
			result, err := client.Fetch(t.Context(), "http://localhost/")
			boundaryZeroResult(t, result)
			if !errors.Is(err, ErrRedirect) {
				t.Fatalf("err=%v", err)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if len(state.dials) != 1 {
				t.Fatalf("dials=%v", state.dials)
			}
		})
	}
	client, _ := New(boundaryOptions())
	current, _ := url.Parse("https://localhost/start")
	response := &http.Response{Header: http.Header{"Location": []string{"http://localhost/final"}}}
	if next, err := client.redirect(current, response, 0); next != nil || !errors.Is(err, ErrRedirect) {
		t.Fatalf("downgrade next=%v err=%v", next, err)
	}
}

func TestBoundaryOriginalAuthorityPreservedWithCanonicalPin(t *testing.T) {
	client, state := boundaryFixture(t, boundaryOptions(), "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ndata")
	result, err := client.Fetch(t.Context(), "http://LOCALHOST:080/path")
	if err != nil || result.FinalURL != "http://LOCALHOST:080/path" || string(result.Bytes) != "data" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.requests) != 1 || state.requests[0].Host != "LOCALHOST:080" || !reflect.DeepEqual(state.dials, []string{"203.0.113.7:80"}) {
		t.Fatalf("requests=%v dials=%v", state.requests, state.dials)
	}
}

func TestBoundaryRedirectRevalidatesSameOriginDNS(t *testing.T) {
	client, state := boundaryFixture(t, boundaryOptions(), "HTTP/1.1 302 Found\r\nLocation: /next\r\nContent-Length: 0\r\n\r\n")
	lookups := 0
	client.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
		lookups++
		if host != "localhost" {
			t.Fatalf("unexpected host %q", host)
		}
		if lookups == 1 {
			return []net.IP{net.ParseIP("203.0.113.7")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	result, err := client.Fetch(t.Context(), "http://localhost/start")
	boundaryZeroResult(t, result)
	if !errors.Is(err, ErrTarget) || lookups != 2 {
		t.Fatalf("err=%v lookups=%d", err, lookups)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.dials) != 1 {
		t.Fatalf("forbidden redirect dial: %v", state.dials)
	}
}
