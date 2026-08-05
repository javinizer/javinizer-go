package imageutil

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"image"
	"image/png"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProxyDialAddrsPins(t *testing.T) {
	// Literal proxies pass through without touching DNS.
	literal, err := url.Parse("http://192.0.2.1:3128")
	require.NoError(t, err)
	addrs, err := resolveProxyDialAddrs(t.Context(), literal, func(context.Context, string) ([]net.IPAddr, error) {
		t.Fatal("literal proxies must not resolve")
		return nil, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.1:3128"}, addrs)

	// Hostname proxies pin every answer with scheme defaults applied.
	named, err := url.Parse("https://proxy.example")
	require.NoError(t, err)
	addrs, err = resolveProxyDialAddrs(t.Context(), named, stubProxyLookup)
	require.NoError(t, err)
	assert.Equal(t, []string{"203.0.113.9:443"}, addrs)

	// Multiple answers all become failover candidates.
	multi := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.9")}, {IP: net.ParseIP("203.0.113.10")}}, nil
	}
	addrs, err = resolveProxyDialAddrs(t.Context(), named, multi)
	require.NoError(t, err)
	assert.Equal(t, []string{"203.0.113.9:443", "203.0.113.10:443"}, addrs)

	// nil lookup falls back to the default resolver (kept for direct helper
	// callers outside the pinned transport).
	localhost, err := url.Parse("http://localhost:3128")
	require.NoError(t, err)
	addrs, err = resolveProxyDialAddrs(t.Context(), localhost, nil)
	require.NoError(t, err)
	require.NotEmpty(t, addrs)
	assert.True(t, strings.HasSuffix(addrs[0], ":3128"))
	assert.NotContains(t, addrs[0], "localhost")

	// Resolution failure and empty answers both fail closed.
	_, err = resolveProxyDialAddrs(t.Context(), named, func(context.Context, string) ([]net.IPAddr, error) {
		return nil, assert.AnError
	})
	require.ErrorContains(t, err, "resolve configured proxy")
	_, err = resolveProxyDialAddrs(t.Context(), named, func(context.Context, string) ([]net.IPAddr, error) { return nil, nil })
	require.ErrorContains(t, err, "no addresses")
}

// Proxy failover: an unreachable first answer must not sink the request; the
// remaining pinned answers are tried in order.
func TestRoundTripHTTPProxyFailsOverResolvedAddresses(t *testing.T) {
	multi := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.9")}, {IP: net.ParseIP("203.0.113.10")}}, nil
	}
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	go func() {
		buf := make([]byte, 4096)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
	}()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://public.example/x", nil)
	require.NoError(t, err)
	var tried []string
	resp, err := roundTripHTTPProxy(t.Context(), req, &url.URL{Scheme: "http", Host: "proxy:80"}, "1.1.1.1:80", &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			tried = append(tried, addr)
			if addr == "203.0.113.9:80" {
				return nil, errors.New("first answer unreachable")
			}
			return client, nil
		},
	}, multi)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	assert.Equal(t, []string{"203.0.113.9:80", "203.0.113.10:80"}, tried, "later answers are failover candidates")
}

func TestDialPinnedAddrsStopsWhenExhaustedOrCanceled(t *testing.T) {
	_, err := dialPinnedAddrs(t.Context(), "tcp", []string{"192.0.2.1:80", "192.0.2.2:80"}, func(context.Context, string, string) (net.Conn, error) {
		return nil, assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = dialPinnedAddrs(ctx, "tcp", []string{"192.0.2.1:80", "192.0.2.2:80"}, func(context.Context, string, string) (net.Conn, error) {
		return nil, assert.AnError
	})
	require.ErrorIs(t, err, context.Canceled)
}

// After pinning, the proxy connection still presents the proxy HOSTNAME as
// TLS SNI (pinned IPs would otherwise break cert validation / vhost routes).
func TestDialTLSProxyPreservesSNIOnPinnedAddr(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "proxy.example"},
		DNSNames:     []string{"proxy.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}),
	)
	require.NoError(t, err)

	var serverHelloSNI string
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	go func() {
		raw, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		server := tls.Server(raw, &tls.Config{
			GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				serverHelloSNI = hello.ServerName
				return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
			},
		})
		_ = server.Handshake()
		_ = server.Close()
	}()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialTLSProxy(t.Context(), "tcp", listener.Addr().String(), "proxy.example", dialer.DialContext, &tls.Config{InsecureSkipVerify: true}, 0) //nolint:gosec // SNI assertion does not need chain validation
	require.NoError(t, err)
	_ = conn.Close()
	assert.Equal(t, "proxy.example", serverHelloSNI, "SNI keeps the proxy hostname after IP pinning")
}

// math.MaxInt64 - slop and beyond must not wrap the header limit negative.
func TestRoundTripHTTPProxyMaxHeaderBytesClamped(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	go func() {
		buf := make([]byte, 4096)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
	}()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://public.example/x", nil)
	require.NoError(t, err)
	resp, err := roundTripHTTPProxy(t.Context(), req, &url.URL{Scheme: "http", Host: "proxy:80"}, "1.1.1.1:80", &http.Transport{
		DialContext:            func(context.Context, string, string) (net.Conn, error) { return client, nil },
		MaxResponseHeaderBytes: math.MaxInt64 - 1,
	}, stubProxyLookup)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

// A caller CheckRedirect that REWRITES req.URL before returning nil must not
// smuggle the rewritten target past the SSRF guard.
func TestValidateRemoteImageWithSafeClientRechecksRewrittenRedirect(t *testing.T) {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return respondWith("HTTP/1.1 302 Found\r\nLocation: http://93.184.216.34/legit\r\nContent-Length: 0\r\n\r\n")(ctx, network, addr)
	}
	rewriter := func(req *http.Request, _ []*http.Request) error {
		req.URL.Host = "169.254.169.254"
		return nil
	}
	client := &http.Client{Transport: &http.Transport{DialContext: dial}, CheckRedirect: rewriter}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "http://93.184.216.34/x", "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "SSRF blocked", "rewritten redirect target must be revalidated")
}

// A proxy-resolution failure aborts the attempt before any dial.
func TestRoundTripHTTPProxyResolveFailureAbortsBeforeDial(t *testing.T) {
	dialed := 0
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://public.example/x", nil)
	require.NoError(t, err)
	_, err = roundTripHTTPProxy(t.Context(), req, &url.URL{Scheme: "http", Host: "proxy:80"}, "1.1.1.1:80", &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { dialed++; return nil, assert.AnError },
	}, func(context.Context, string) ([]net.IPAddr, error) { return nil, assert.AnError })
	require.ErrorContains(t, err, "resolve configured proxy")
	assert.Zero(t, dialed)
}

func TestTargetTLSConfigNeverOverridesCallerServerName(t *testing.T) {
	custom := targetTLSConfigFor(&tls.Config{ServerName: "override.example"}, "origin.example")
	assert.Equal(t, "override.example", custom.ServerName, "caller SNI must survive")
	defaulted := targetTLSConfigFor(cloneTLSConfig(nil), "origin.example")
	assert.Equal(t, "origin.example", defaulted.ServerName)
}

// A manually-driven TLS handshake on this path must honor
// Transport.TLSHandshakeTimeout: a proxy that accepts TCP but never answers
// the handshake cannot hang validation indefinitely.
func TestDialTLSProxyHonorsHandshakeTimeout(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	go func() {
		buf := make([]byte, 2048)
		_, _ = server.Read(buf) // swallow ClientHello, then never answer
		<-t.Context().Done()
	}()
	start := time.Now()
	_, err := dialTLSProxy(t.Context(), "tcp", "1.1.1.1:443", "proxy.example",
		func(context.Context, string, string) (net.Conn, error) { return client, nil },
		&tls.Config{InsecureSkipVerify: true}, 80*time.Millisecond) //nolint:gosec // timeout assertion, not chain validation
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "handshake must be time-bounded")
}

// A caller-provided TLS ServerName must survive the manual proxy handshake
// (SNI pinning must not rewrite it to the dial host).
func TestDialTLSProxyKeepsCallerServerName(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		DNSNames:     []string{"override.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	)
	require.NoError(t, err)

	hello := make(chan string, 1)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	go func() {
		raw, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		server := tls.Server(raw, &tls.Config{
			GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
				hello <- h.ServerName
				return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
			},
		})
		_ = server.Handshake()
		_ = server.Close()
	}()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialTLSProxy(t.Context(), "tcp", listener.Addr().String(), "proxy.example", dialer.DialContext,
		&tls.Config{InsecureSkipVerify: true, ServerName: "override.example"}, 5*time.Second) //nolint:gosec // asserting SNI plumbing, not chain validation
	require.NoError(t, err)
	_ = conn.Close()
	assert.Equal(t, "override.example", <-hello)
}

// The manual proxy path must keep dialing through a legacy-only Dial hook
// (deprecated, but honored) instead of silently discarding it.
func TestRoundTripHTTPProxyHonorsLegacyDial(t *testing.T) {
	sentinel := errors.New("legacy dialer routed it")
	var dialed []string
	legacyTransport := &http.Transport{
		Dial: func(network, addr string) (net.Conn, error) { //nolint:staticcheck // exercising the legacy seam
			dialed = append(dialed, addr)
			return nil, sentinel
		},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://public.example/x", nil)
	require.NoError(t, err)
	_, err = roundTripHTTPProxy(t.Context(), req, &url.URL{Scheme: "http", Host: "proxy:3128"}, "1.1.1.1:80", legacyTransport, stubProxyLookup)
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"203.0.113.9:3128"}, dialed, "legacy dialer receives the pinned proxy endpoint")
}

// SOCKS5 transports (registered by httpclient as remote-DNS) must keep
// hostname targets end to end: no local DNS runs and the dialer receives
// the name verbatim -- pinnning would break proxy-only and split-horizon
// services.
func TestValidateRemoteImagePreservesRemoteDNSHostnames(t *testing.T) {
	var dialed string
	var pngBuf bytes.Buffer
	require.NoError(t, png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	socksLike := func(_ context.Context, network, addr string) (net.Conn, error) {
		dialed = addr
		return respondWith("HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: "+strconv.Itoa(pngBuf.Len())+"\r\n\r\n"+pngBuf.String())(context.Background(), network, addr)
	}
	transport := &http.Transport{DialContext: socksLike}
	ssrf.MarkRemoteDNSTransport(transport)

	err := ValidateRemoteImageWithSafeClient(context.Background(), &http.Client{Transport: transport}, "http://proxy-only.example/img.png", "ua", "")
	require.NoError(t, err)
	assert.Equal(t, "proxy-only.example:80", dialed, "dial must receive the unresolved hostname")

	// Lexical guard still applies with local DNS off: literals stay blocked.
	dialed = ""
	err = ValidateRemoteImageWithSafeClient(context.Background(), &http.Client{Transport: transport}, "http://10.1.2.3/img.png", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10.1.2.3")
	assert.Empty(t, dialed, "blocked literal never dialed")
}

// The remote-DNS lexical preflight rejects non-http schemes and empty hosts
// without ever consulting DNS (or a resolver we keep unused).
func TestValidateImageTargetLexicalGuardForRemoteDNS(t *testing.T) {
	require.ErrorContains(t, validateImageTarget(t.Context(), "ftp://proxy-only.example/x", true), "non-http(s)")
	require.ErrorContains(t, validateImageTarget(t.Context(), "http:///x", true), "empty host")
	require.NoError(t, validateImageTarget(t.Context(), "https://proxy-only.example/x", true))
}
