package ssrf

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestPinnedDialValidation(t *testing.T) {
	for _, tc := range []struct {
		name, network, addr string
		ips                 []net.IP
		want                []string
	}{
		{"failover", "tcp", "public.test:443", []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("203.0.113.2")}, []string{"203.0.113.1:443", "203.0.113.2:443"}},
		{"ipv4", "tcp4", "public.test:443", []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("203.0.113.1")}, []string{"203.0.113.1:443"}},
		{"ipv6", "tcp6", "public.test:443", []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("2001:db8::1")}, []string{"[2001:db8::1]:443"}},
		{"mixed", "tcp", "public.test:443", []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("10.0.0.1")}, nil},
		{"mapped private", "tcp", "public.test:443", []net.IP{net.ParseIP("::ffff:127.0.0.1")}, nil},
		{"invalid IP", "tcp", "public.test:443", []net.IP{{1, 2, 3}}, nil},
		{"nil IP", "tcp", "public.test:443", []net.IP{nil}, nil},
		{"empty", "tcp", "public.test:443", nil, nil},
		{"wrong family", "tcp6", "public.test:443", []net.IP{net.ParseIP("203.0.113.1")}, nil},
		{"missing port", "tcp", "public.test", nil, nil},
		{"named port", "tcp", "public.test:https", nil, nil},
		{"zero port", "tcp", "public.test:0", nil, nil},
		{"overflow port", "tcp", "public.test:65536", nil, nil},
		{"empty host", "tcp", ":443", nil, nil},
		{"unsupported network", "udp", "public.test:443", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useTestLookup(t, map[string][]net.IP{"public.test": tc.ips})
			var got []string
			marker := errors.New("dial refused")
			dial := pinnedDialer(func(_ context.Context, _, addr string) (net.Conn, error) { got = append(got, addr); return nil, marker })
			_, err := dial(t.Context(), tc.network, tc.addr)
			if err == nil {
				t.Fatal("expected failure")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("dialed %v; want %v", got, tc.want)
			}
			if len(got) > 0 && !errors.Is(err, marker) {
				t.Fatalf("lost dial error: %v", err)
			}
		})
	}
}

func TestPinnedDialResolvesOnceAndCancelsFailover(t *testing.T) {
	lookups := 0
	t.Cleanup(setLookupIPForTest(func(string) ([]net.IP, error) {
		lookups++
		return []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("203.0.113.2")}, nil
	}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	dial := pinnedDialer(func(_ context.Context, _, addr string) (net.Conn, error) {
		calls++
		if addr != "203.0.113.1:443" {
			t.Errorf("unpinned address: %s", addr)
		}
		cancel()
		return nil, errors.New("failed")
	})
	_, err := dial(ctx, "tcp", "public.test:443")
	if !errors.Is(err, context.Canceled) || lookups != 1 || calls != 1 {
		t.Fatalf("err=%v lookups=%d calls=%d", err, lookups, calls)
	}
	_, err = dial(ctx, "tcp", "public.test:443")
	if !errors.Is(err, context.Canceled) || lookups != 1 || calls != 1 {
		t.Fatalf("pre-canceled err=%v lookups=%d calls=%d", err, lookups, calls)
	}
}

func TestWrappedTransportPreservesProxyAndRejectsCustomTLS(t *testing.T) {
	proxyURL, _ := url.Parse("http://proxy.test:3128")
	for _, legacy := range []bool{false, true} {
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		called := false
		if legacy {
			transport.DialTLS = func(string, string) (net.Conn, error) { called = true; return nil, nil }
		} else {
			transport.DialTLSContext = func(context.Context, string, string) (net.Conn, error) { called = true; return nil, nil }
		}
		if WrapTransportWithSSRFCheck(transport) != transport {
			t.Fatal("transport replaced")
		}
		req, _ := http.NewRequest("GET", "https://target.test", nil)
		got, err := transport.Proxy(req)
		if err != nil || got.String() != proxyURL.String() {
			t.Fatalf("proxy changed: %v %v", got, err)
		}
		if _, err := transport.DialTLSContext(t.Context(), "tcp", "target.test:443"); err == nil || called {
			t.Fatalf("custom TLS bypass: err=%v called=%v", err, called)
		}
	}
}

func TestCheckURLRejectsInvalidAnswers(t *testing.T) {
	for _, ips := range [][]net.IP{nil, {nil}, {{1, 2, 3}}} {
		func() {
			restore := setLookupIPForTest(func(string) ([]net.IP, error) { return ips, nil })
			defer restore()
			if err := CheckURL("https://public.test/"); err == nil {
				t.Fatalf("accepted invalid DNS answer: %v", ips)
			}
		}()
	}
}

func TestPinnedTLSKeepsHostnameVerificationAndSNI(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" {
			t.Errorf("lost hostname: Host=%q SNI=%q", r.Host, r.TLS.ServerName)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	useTestLookup(t, map[string][]net.IP{"example.com": {net.ParseIP("203.0.113.1")}, "wrong.test": {net.ParseIP("203.0.113.1")}})
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "203.0.113.1:443" {
			return nil, errors.New("unexpected endpoint")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	WrapTransportWithSSRFCheck(transport)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	req, _ := http.NewRequestWithContext(t.Context(), "GET", "https://example.com/", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	req, _ = http.NewRequestWithContext(t.Context(), "GET", "https://wrong.test/", nil)
	resp, err = client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("accepted wrong certificate hostname")
	}
}

func TestPinnedHTTPProxyPreservesDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "http://target.test/resource" || r.Host != "target.test" {
			t.Errorf("proxy target changed: %s %s", r.URL, r.Host)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	useTestLookup(t, map[string][]net.IP{"proxy.test": {net.ParseIP("203.0.113.1")}})
	proxyURL, _ := url.Parse("http://proxy.test:3128")
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "203.0.113.1:3128" {
			return nil, errors.New("unexpected proxy endpoint")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	WrapTransportWithSSRFCheck(transport)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	req, _ := http.NewRequestWithContext(t.Context(), "GET", "http://target.test/resource", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestPinnedDialUsesOneTimeoutBudget(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"public.test": {net.ParseIP("203.0.113.1"), net.ParseIP("203.0.113.2")}})
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	calls := 0
	dial := pinnedDialerWithTimeout(func(attempt context.Context, _, _ string) (net.Conn, error) {
		calls++
		got, ok := attempt.Deadline()
		if !ok || got.After(deadline) {
			t.Error("connection budget restarted")
		}
		if calls == 2 {
			if got != deadline {
				t.Error("final attempt lost caller deadline")
			}
			cancel()
			return nil, attempt.Err()
		}
		return nil, errors.New("failed")
	}, 2*time.Minute)
	_, err := dial(ctx, "tcp", "public.test:443")
	if !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestCustomDialerKeepsDeadlineAndSingleAttempt(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"public.test": {net.ParseIP("203.0.113.1"), net.ParseIP("203.0.113.2")}})
	calls := 0
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		calls++
		if _, ok := ctx.Deadline(); ok {
			t.Error("added custom dialer deadline")
		}
		return nil, errors.New("custom failure")
	}}
	WrapTransportWithSSRFCheck(transport)
	_, err := transport.DialContext(t.Context(), "tcp", "public.test:443")
	if err == nil || calls != 1 {
		t.Fatalf("custom attempts=%d err=%v", calls, err)
	}
}

func TestCallerOwnedBudgetEnablesValidatedFailover(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"public.test": {net.ParseIP("2001:db8::1"), net.ParseIP("203.0.113.1")}})
	calls := 0
	var deadline time.Time
	marker := errors.New("unreachable")
	transport := &http.Transport{DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		calls++
		got, ok := ctx.Deadline()
		if !ok {
			t.Error("missing caller budget")
		}
		if calls == 1 {
			deadline = got
			if addr != "[2001:db8::1]:443" {
				t.Errorf("first address: %s", addr)
			}
		} else if !got.After(deadline) || addr != "203.0.113.1:443" {
			t.Errorf("failover context or address changed: %s", addr)
		}
		return nil, marker
	}}
	WrapTransportWithSSRFCheck(transport, 30*time.Second)
	_, err := transport.DialContext(t.Context(), "tcp", "public.test:443")
	if !errors.Is(err, marker) || calls != 2 {
		t.Fatalf("attempts=%d err=%v", calls, err)
	}
}

type socksForwardDialer struct{ serve func(net.Conn) }

func (d socksForwardDialer) Dial(string, string) (net.Conn, error) {
	client, server := net.Pipe()
	go d.serve(server)
	return client, nil
}

func TestSOCKSCustomDialerReceivesPinnedLiteral(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"target.test": {net.ParseIP("203.0.113.1")}})
	serverResult := make(chan error, 1)
	forward := socksForwardDialer{serve: func(conn net.Conn) {
		defer conn.Close()
		var err error
		defer func() { serverResult <- err }()
		greeting := make([]byte, 3)
		if _, err = io.ReadFull(conn, greeting); err != nil {
			return
		}
		if !reflect.DeepEqual(greeting, []byte{5, 1, 0}) {
			err = errors.New("unexpected SOCKS greeting")
			return
		}
		if _, err = conn.Write([]byte{5, 0}); err != nil {
			return
		}
		request := make([]byte, 10)
		if _, err = io.ReadFull(conn, request); err != nil {
			return
		}
		if request[0] != 5 || request[1] != 1 || request[3] != 1 || !net.IP(request[4:8]).Equal(net.ParseIP("203.0.113.1")) || binary.BigEndian.Uint16(request[8:]) != 443 {
			err = errors.New("SOCKS request did not use pinned IPv4 endpoint")
			return
		}
		_, err = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	}}
	dialer, err := proxy.SOCKS5("tcp", "proxy.test:1080", nil, forward)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{DialContext: dialer.(proxy.ContextDialer).DialContext}
	WrapTransportWithSSRFCheck(transport, 30*time.Second)
	conn, err := transport.DialContext(t.Context(), "tcp", "target.test:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestStalledAddressLeavesFailoverBudget(t *testing.T) {
	useTestLookup(t, map[string][]net.IP{"public.test": {net.ParseIP("2001:db8::1"), net.ParseIP("203.0.113.1")}})
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	calls := 0
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return client, nil
	}}
	WrapTransportWithSSRFCheck(transport, 20*time.Millisecond)
	got, err := transport.DialContext(t.Context(), "tcp", "public.test:443")
	if err != nil || got != client || calls != 2 {
		t.Fatalf("stalled fallback conn=%v calls=%d err=%v", got, calls, err)
	}
}

func TestFallbackKeepsExplicitBudget(t *testing.T) {
	transport := &http.Transport{}
	WrapTransportWithSSRFCheck(transport, time.Nanosecond)
	_, err := transport.DialContext(t.Context(), "tcp", "public.test:443")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("explicit budget lost: %v", err)
	}
}
