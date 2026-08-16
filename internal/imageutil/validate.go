package imageutil

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	_ "golang.org/x/image/webp"
)

// maxThumbnailValidationBytes ...
const (
	maxThumbnailValidationBytes   = 2 * 1024 * 1024
	defaultMaxResponseHeaderBytes = 10 << 20
	responseHeaderReadSlop        = 4 << 10
	httpsScheme                   = "https"
)

var validateRemoteImageWithClient = ValidateRemoteImageWithClient

// The blocklist policy is owned by internal/ssrf (THE single guard).
func isPublicTargetIP(ip net.IP) bool {
	return !ssrf.IsBlockedIP(ip)
}

func resolvePublicTargetIPs(ctx context.Context, host string, lookup func(context.Context, string) ([]net.IPAddr, error)) ([]net.IP, error) {
	addresses, err := lookup(ctx, host)
	if err != nil {
		// Transient: DNS failure is fail-closed but never a permanent policy
		// rejection; callers must not persist it as such.
		return nil, &ssrf.UnverifiableHostError{Host: host, Err: err}
	}
	if len(addresses) == 0 {
		return nil, &ssrf.UnverifiableHostError{Host: host, Err: errors.New("resolved to no addresses")}
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ip := address.IP
		if !isPublicTargetIP(ip) {
			return nil, fmt.Errorf("SSRF blocked: %s resolves to private/internal IP", host)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

func dialPublicTarget(ctx context.Context, network, addr string, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
	}
	ips, err := resolvePublicTargetIPs(ctx, host, lookup)
	if err != nil {
		return nil, err
	}
	var dialErr error
	for _, ip := range ips {
		conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = errors.Join(dialErr, err)
	}
	return nil, dialErr
}

func cloneTLSConfig(config *tls.Config) *tls.Config {
	if config == nil {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return config.Clone()
}

// dialTLSProxy dials dialAddr and handshakes with SNI serverName (the proxy
// HOSTNAME): after proxy dialing was pinned to a resolved IP, the dial
// address no longer carries the name the proxy's certificate expects. A
// caller-set config.ServerName always wins.
// targetTLSConfigFor clones base and defaults ServerName to the request host
// -- but never overrides a caller-set ServerName (custom certificate and
// SNI setups must survive pinning).
func targetTLSConfigFor(base *tls.Config, host string) *tls.Config {
	clone := base.Clone()
	if clone.ServerName == "" {
		clone.ServerName = host
	}
	return clone
}

func dialTLSProxy(ctx context.Context, network, addr, serverName string, dial func(context.Context, string, string) (net.Conn, error), config *tls.Config, handshakeTimeout time.Duration) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	conn, err := dial(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	tlsConfig := cloneTLSConfig(config)
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = serverName
	}
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = host
	}
	tlsConn := tls.Client(conn, tlsConfig)
	// This path handshakes MANUALLY, so Transport.TLSHandshakeTimeout would
	// otherwise be ignored: a proxy that accepts TCP but stalls TLS could hang
	// validation forever on a deadline-less caller context.
	handshakeCtx := ctx
	if handshakeTimeout > 0 {
		var cancel context.CancelFunc
		handshakeCtx, cancel = context.WithTimeout(ctx, handshakeTimeout)
		defer cancel()
	}
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// canonicalProxyAddr renders the proxy's dial target with its scheme default
// port filled in (net/http dials proxies on 80/443 when no port is given).
func canonicalProxyAddr(proxyURL *url.URL) string {
	if proxyURL.Port() != "" {
		return proxyURL.Host
	}
	port := "80"
	if proxyURL.Scheme == httpsScheme {
		port = "443"
	}
	return net.JoinHostPort(proxyURL.Hostname(), port)
}

// resolveProxyDialAddrs pins the proxy endpoint itself: a hostname proxy URL
// is resolved ONCE and every answer becomes a pinned dial candidate, so DNS
// rebinding or multi-answer churn between preflight and connect cannot
// redirect the validator's proxy connection elsewhere -- and an unreachable
// first answer fails over to the rest instead of stalling every attempt.
// Configured proxies are trusted infrastructure (often internal), so answers
// are pinned, not public-gated. Literal-IP proxies pass through untouched.
func resolveProxyDialAddrs(ctx context.Context, proxyURL *url.URL, lookup func(context.Context, string) ([]net.IPAddr, error)) ([]string, error) {
	addr := canonicalProxyAddr(proxyURL)
	// canonicalProxyAddr always yields host:port, so the split cannot fail.
	host, _, _ := net.SplitHostPort(addr)
	if net.ParseIP(host) != nil {
		return []string{addr}, nil
	}
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve configured proxy %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve configured proxy %s: no addresses", host)
	}
	_, port, _ := net.SplitHostPort(addr)
	candidates := make([]string, 0, len(addrs))
	for _, resolved := range addrs {
		candidates = append(candidates, net.JoinHostPort(resolved.IP.String(), port))
	}
	return candidates, nil
}

// dialPinnedAddrs dials the first reachable pinned proxy address.
func dialPinnedAddrs(ctx context.Context, network string, addrs []string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	var dialErr error
	for _, addr := range addrs {
		conn, err := dial(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		dialErr = errors.Join(dialErr, err)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, dialErr
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func encodeProxyRequest(req *http.Request, pinnedURL *url.URL, proxyUser *url.Userinfo) ([]byte, error) {
	writeReq := req.Clone(req.Context())
	writeReq.URL = pinnedURL
	writeReq.Host = req.URL.Host
	writeReq.Header = req.Header.Clone()
	if proxyUser != nil {
		password, _ := proxyUser.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(proxyUser.Username() + ":" + password))
		writeReq.Header.Set("Proxy-Authorization", "Basic "+credentials)
	}
	var encoded bytes.Buffer
	if err := writeReq.Write(&encoded); err != nil {
		return nil, err
	}
	requestBytes := encoded.Bytes()
	lineEnd := bytes.Index(requestBytes, []byte("\r\n"))
	absoluteURI := pinnedURL.Scheme + "://" + pinnedURL.Host + pinnedURL.RequestURI()
	requestLine := fmt.Sprintf("%s %s %s\r\n", req.Method, absoluteURI, req.Proto)
	return append([]byte(requestLine), requestBytes[lineEnd+2:]...), nil
}

type responseHeaderLimitReader struct {
	reader    io.Reader
	remaining int64
	done      bool
}

func (r *responseHeaderLimitReader) Read(data []byte) (int, error) {
	read, err := r.reader.Read(data)
	if r.done {
		return read, err
	}
	if int64(read) > r.remaining {
		return int(r.remaining) + 1, fmt.Errorf("response headers exceed configured limit")
	}
	r.remaining -= int64(read)
	return read, err
}

type proxyResponseBody struct {
	io.ReadCloser
	conn net.Conn
	done chan struct{}
	once sync.Once
}

func (b *proxyResponseBody) Close() error {
	var closeErr error
	b.once.Do(func() {
		close(b.done)
		closeErr = errors.Join(b.conn.Close(), b.ReadCloser.Close())
	})
	return closeErr
}

func roundTripHTTPProxy(ctx context.Context, req *http.Request, proxyURL *url.URL, pinnedHost string, transport *http.Transport, lookup func(context.Context, string) ([]net.IPAddr, error)) (*http.Response, error) {
	proxyAddrs, err := resolveProxyDialAddrs(ctx, proxyURL, lookup)
	if err != nil {
		return nil, err
	}
	// Respect legacy Dial-only transports (assigning DialContext while
	// ignoring Dial would silently discard them).
	dial := ssrf.DialContextFunc(transport)
	var conn net.Conn
	if proxyURL.Scheme == httpsScheme {
		serverName := proxyURL.Hostname()
		tlsConfig := transport.TLSClientConfig
		conn, err = dialPinnedAddrs(ctx, "tcp", proxyAddrs, func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialTLSProxy(ctx, network, addr, serverName, dial, tlsConfig, transport.TLSHandshakeTimeout)
		})
	} else {
		conn, err = dialPinnedAddrs(ctx, "tcp", proxyAddrs, dial)
	}
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	closeConn := func(err error) (*http.Response, error) {
		close(done)
		_ = conn.Close()
		return nil, err
	}
	pinnedURL := *req.URL
	pinnedURL.Host = pinnedHost
	pinnedURL.Opaque = ""
	requestBytes, err := encodeProxyRequest(req, &pinnedURL, proxyURL.User)
	if err != nil {
		return closeConn(err)
	}
	if err := writeFull(conn, requestBytes); err != nil {
		return closeConn(err)
	}
	maxHeaderBytes := transport.MaxResponseHeaderBytes
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = defaultMaxResponseHeaderBytes
	}
	// Clamp: at settings within 4096 of math.MaxInt64 the slop addition would
	// overflow negative and break the framing read below.
	headerLimit := maxHeaderBytes + responseHeaderReadSlop
	if headerLimit < maxHeaderBytes {
		headerLimit = math.MaxInt64
	}
	headerReader := &responseHeaderLimitReader{reader: conn, remaining: headerLimit}
	bufferedReader := bufio.NewReader(headerReader)
	headerTimeout := transport.ResponseHeaderTimeout
	if headerTimeout > 0 {
		// This manual ReadResponse path has no client/transport machinery
		// above it: honor Transport.ResponseHeaderTimeout via a read
		// deadline so a proxy that accepted the request but never answers
		// cannot hang validation indefinitely. Set the deadline once before
		// the 1xx loop: a proxy can send informational 1xx responses
		// indefinitely, and resetting per-iteration would make the timeout
		// an idle timer rather than bounding the complete header wait.
		_ = conn.SetReadDeadline(time.Now().Add(headerTimeout))
	}
	for {
		resp, err := http.ReadResponse(bufferedReader, req)
		if err != nil {
			return closeConn(err)
		}
		if resp.StatusCode >= 100 && resp.StatusCode < 200 && resp.StatusCode != http.StatusSwitchingProtocols {
			_ = resp.Body.Close()
			continue
		}
		headerReader.done = true
		if headerTimeout > 0 {
			// ResponseHeaderTimeout governs the header wait only; the body
			// streams without a deadline over this connection.
			_ = conn.SetReadDeadline(time.Time{})
		}
		resp.Body = &proxyResponseBody{ReadCloser: resp.Body, conn: conn, done: done}
		return resp, nil
	}
}

func isRetryableProxyStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

type lookupIPAddrsFunc func(context.Context, string) ([]net.IPAddr, error)

// lookupProxyAddrs is the resolution seam for the native-socks endpoint pin.
var lookupProxyAddrs lookupIPAddrsFunc = net.DefaultResolver.LookupIPAddr

type pinnedProxyTransport struct {
	base   *http.Transport
	lookup func(context.Context, string) ([]net.IPAddr, error)
	// directOnce/direct cache the no-proxy clone so a fresh http.Transport is
	// not allocated per request.
	directOnce sync.Once
	direct     *http.Transport
}

func (t *pinnedProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	lookup := t.lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	var proxyURL *url.URL
	var err error
	if t.base.Proxy != nil {
		proxyURL, err = t.base.Proxy(req)
		if err != nil {
			return nil, err
		}
	}
	if ssrf.IsSOCKSProxyURL(proxyURL) {
		// Native SOCKS5/SOCKS5H: the TUNNEL resolves the target. Preserve the
		// hostname, pin the proxy endpoint, and still refuse private literals.
		if literal := ssrf.HostIPLiteral(req.URL.Hostname()); literal != nil && ssrf.IsBlockedIP(literal) {
			return nil, &ssrf.BlockedTargetError{Target: req.URL.Hostname(), Reason: "private/internal IP literal"}
		}
		transport := t.base.Clone()
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DisableKeepAlives = true
		defer transport.CloseIdleConnections()
		proxyAddrs, plErr := resolveProxyDialAddrs(req.Context(), proxyURL, lookup)
		if plErr != nil {
			return nil, plErr
		}
		baseDial := ssrf.DialContextFunc(transport)
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialPinnedAddrs(ctx, network, proxyAddrs, baseDial)
		}
		resp, err := transport.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		resp.Request = req
		return resp, nil
	}
	if proxyURL == nil {
		t.directOnce.Do(func() {
			transport := t.base.Clone()
			transport.Proxy = nil
			transport.DisableKeepAlives = true
			originalDialContext := ssrf.DialContextFunc(transport)
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Read the lookup seam dynamically so a replaced resolver still
				// applies after the clone is cached.
				current := t.lookup
				if current == nil {
					current = net.DefaultResolver.LookupIPAddr
				}
				return dialPublicTarget(ctx, network, addr, current, originalDialContext)
			}
			t.direct = transport
		})
		return t.direct.RoundTrip(req)
	}
	host := req.URL.Hostname()
	ips, err := resolvePublicTargetIPs(req.Context(), host, lookup)
	if err != nil {
		return nil, err
	}
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == httpsScheme {
			port = "443"
		} else {
			port = "80"
		}
	}
	var roundTripErr error
	for _, ip := range ips {
		pinnedHost := net.JoinHostPort(ip.String(), port)
		transport := t.base.Clone()
		// Per-attempt clone (pinned host/TLS vary): retire its pool state after
		// the attempt so error/abandon paths leak no sockets or goroutines.
		defer transport.CloseIdleConnections()
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DisableKeepAlives = true

		if req.URL.Scheme == "http" && (proxyURL.Scheme == "http" || proxyURL.Scheme == httpsScheme) {
			resp, err := roundTripHTTPProxy(req.Context(), req, proxyURL, pinnedHost, transport, lookup)
			if err == nil && !isRetryableProxyStatus(resp.StatusCode) {
				resp.Request = req
				return resp, nil
			}
			if resp != nil {
				err = errors.Join(err, fmt.Errorf("proxy returned retryable status %d", resp.StatusCode), resp.Body.Close())
			}
			roundTripErr = errors.Join(roundTripErr, err)
			continue
		}
		pinnedReq := req.Clone(req.Context())
		pinnedURL := *req.URL
		pinnedURL.Host = pinnedHost
		pinnedReq.URL = &pinnedURL
		pinnedReq.Host = req.URL.Host
		if req.URL.Scheme == httpsScheme {
			baseTLSConfig := cloneTLSConfig(transport.TLSClientConfig)
			transport.TLSClientConfig = targetTLSConfigFor(baseTLSConfig, host)
			// Pin the proxy endpoint for the CONNECT path too: the peer the
			// validator tunnels through must be the one resolved up front,
			// with failover across all its answers.
			proxyDialAddrs, err := resolveProxyDialAddrs(req.Context(), proxyURL, lookup)
			if err != nil {
				roundTripErr = errors.Join(roundTripErr, err)
				continue
			}
			pinnedProxyDial := ssrf.DialContextFunc(transport)
			// With a proxy in play, net/http only ever dials THE PROXY here.
			transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialPinnedAddrs(ctx, network, proxyDialAddrs, pinnedProxyDial)
			}
			if proxyURL.Scheme == httpsScheme && transport.DialTLSContext == nil {
				dial := transport.DialContext
				serverName := proxyURL.Hostname()
				transport.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
					return dialPinnedAddrs(ctx, network, proxyDialAddrs, func(ctx context.Context, network, addr string) (net.Conn, error) {
						return dialTLSProxy(ctx, network, addr, serverName, dial, baseTLSConfig, transport.TLSHandshakeTimeout)
					})
				}
			}
		}
		resp, err := transport.RoundTrip(pinnedReq)
		if err == nil {
			resp.Request = req
			return resp, nil
		}
		roundTripErr = errors.Join(roundTripErr, err)
	}
	return nil, roundTripErr
}

// validateImageTarget runs the SSRF preflight with the CALLER's context so a
// canceled/deadline-bound request aborts during DNS instead of waiting out a
// background resolution.
func validateImageTarget(ctx context.Context, rawURL string, remoteDNS bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if !remoteDNS {
		return ssrf.CheckTarget(ctx, parsed)
	}
	return lexicalImageTargetCheck(parsed)
}

// lexicalImageTargetCheck is the DNS-free shape guard used for remote-DNS
// (SOCKS5) clients: local resolution cannot prove what the proxy will get,
// but scheme, host presence, and private literals are knowable without DNS.
func lexicalImageTargetCheck(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != httpsScheme {
		return &ssrf.BlockedTargetError{Target: u.String(), Reason: "non-http(s) scheme"}
	}
	if u.Hostname() == "" {
		return &ssrf.BlockedTargetError{Target: u.String(), Reason: "empty host"}
	}
	if ssrf.IsBlockedHost(u.Hostname()) {
		return &ssrf.BlockedTargetError{Target: u.Hostname(), Reason: "lexically private/loopback host or literal"}
	}
	return nil
}

// checkImageTargetHop validates a redirect hop with the same remote-DNS
// contract the initial-URL preflight uses.
func checkImageTargetHop(ctx context.Context, u *url.URL, remoteDNS bool) error {
	if remoteDNS {
		return lexicalImageTargetCheck(u)
	}
	return ssrf.CheckTarget(ctx, u)
}

// hopRemoteDNSDecision reports whether THIS request hop resolves remotely:
// registry-marked transports or a native socks5/socks5h decision computed
// from the hop's request (header/URL-sensitive policies included).
func hopRemoteDNSDecision(req *http.Request, transport *http.Transport) bool {
	if transport == nil {
		return false
	}
	if ssrf.TransportResolvesRemotely(transport) {
		return true
	}
	if transport.Proxy == nil {
		return false
	}
	u, err := transport.Proxy(req)
	return err == nil && ssrf.IsSOCKSProxyURL(u)
}

// ValidateRemoteImage ...
func ValidateRemoteImage(ctx context.Context, rawURL string) error {
	if err := validateImageTarget(ctx, rawURL, false); err != nil {
		return err
	}
	return ValidateRemoteImageWithSafeClient(ctx, ssrf.NewSSRFSafeClient(30*time.Second), rawURL, config.DefaultUserAgent, httpclient.ResolveMediaReferer(rawURL, ""))
}

// ValidateRemoteImageWithSafeClient ...
func ValidateRemoteImageWithSafeClient(ctx context.Context, client *http.Client, rawURL, userAgent, referer string) error {
	if client == nil {
		return fmt.Errorf("image validator client is nil")
	}
	// Remote-DNS transports get the lexical-only preflight: local DNS cannot
	// prove what the proxy will resolve. Detection covers the factory-marked
	// shape AND native socks5 proxying (Transport.Proxy = socks5 URL), probed
	// once against the REAL target URL -- policy funcs answer faithfully.
	remoteDNS := false
	var nativeSocksURL *url.URL
	if transport, ok := client.Transport.(*http.Transport); ok {
		remoteDNS = ssrf.TransportResolvesRemotely(transport)
		if !remoteDNS && transport.Proxy != nil {
			if probeURL, perr := url.Parse(strings.TrimSpace(rawURL)); perr == nil && probeURL != nil {
				probeReq := &http.Request{Method: http.MethodGet, URL: probeURL}
				probeReq.Header = http.Header{"User-Agent": {userAgent}}
				if u, err := transport.Proxy(probeReq); err == nil && ssrf.IsSOCKSProxyURL(u) {
					nativeSocksURL = u
					remoteDNS = true
				}
			}
		}
	}
	if err := validateImageTarget(ctx, rawURL, remoteDNS); err != nil {
		return err
	}
	safeClient := *client
	if remoteDNS {
		// Fail closed: a custom TLS dialer bypasses the guarded DialContext
		// for HTTPS, so it is unpinnable on ANY transport shape.
		clone := client.Transport.(*http.Transport).Clone()
		if clone.DialTLSContext != nil || clone.DialTLS != nil { //nolint:staticcheck // fail closed on unpinnable TLS
			return fmt.Errorf("image validation rejects transports with DialTLSContext/DialTLS on remote-DNS paths (unpinnable)")
		}
		if nativeSocksURL != nil {
			// Use the per-hop-evaluating pinning transport so redirect hops that
			// the policy routes DIRECT fall back to the full guard instead of
			// riding the first hop's SOCKS endpoint.
			safeClient.Transport = &pinnedProxyTransport{base: clone, lookup: lookupProxyAddrs}
		} else {
			ssrf.MarkRemoteDNSTransport(clone)
			safeClient.Transport = ssrf.WrapTransportPreservingHostnames(clone)
		}
	} else if client.Transport == nil {
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return fmt.Errorf("image validation requires a *http.Transport-capable default transport")
		}
		// A globally customized default transport with DialTLS/DialTLSContext
		// would bypass the pinned DialContext for HTTPS: reject it here too,
		// same as an explicitly supplied client transport.
		if defaultTransport.DialTLSContext != nil || defaultTransport.DialTLS != nil { //nolint:staticcheck // fail closed on unpinnable defaults
			return fmt.Errorf("image validation rejects default transports with DialTLSContext/DialTLS (unpinnable)")
		}
		safeClient.Transport = &pinnedProxyTransport{base: defaultTransport.Clone()}
	} else if transport, ok := client.Transport.(*http.Transport); ok { //nolint:gocritic // remoteDNS arm precedes
		if transport.DialTLSContext != nil || transport.DialTLS != nil { //nolint:staticcheck // fail closed: a custom TLS dialer would bypass the pinned dial for HTTPS
			return fmt.Errorf("image validation rejects transports with DialTLSContext/DialTLS (unpinnable)")
		}
		safeClient.Transport = &pinnedProxyTransport{base: transport.Clone()}
	} else {
		// Fail closed: a wrapped RoundTripper (cloudscraper-style) cannot be
		// pinned, so silently keeping it would downgrade every guard.
		return fmt.Errorf("image validation requires a *http.Transport (got %T)", client.Transport)
	}
	previousCheckRedirect := client.CheckRedirect
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// The redirect cap runs regardless of a caller-supplied policy: an
		// always-nil policy must not let a looping server hang validation.
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		checkTransport, _ := client.Transport.(*http.Transport)
		if err := checkImageTargetHop(req.Context(), req.URL, hopRemoteDNSDecision(req, checkTransport)); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			if err := previousCheckRedirect(req, via); err != nil {
				return err
			}
			// Caller redirect policies may REWRITE req.URL before approving the
			// hop; validate the final target, not just the pre-callback one,
			// and evaluate the decision against THE REWRITTEN request.
			return checkImageTargetHop(req.Context(), req.URL, hopRemoteDNSDecision(req, checkTransport))
		}
		return nil
	}
	return validateRemoteImageWithClient(ctx, &safeClient, rawURL, userAgent, referer)
}

// ValidateRemoteImageWithClient ...
func ValidateRemoteImageWithClient(ctx context.Context, client *http.Client, rawURL, userAgent, referer string) error {
	if client == nil {
		return fmt.Errorf("image validator client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	// Advertise exactly the formats with registered decoders (no image/*
	// wildcard: that re-advertises AVIF/SVG, which DecodeConfig rejects).
	req.Header.Set("Accept", "image/webp,image/apng,image/png,image/jpeg,image/gif")
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = httpclient.DrainAndClose(resp.Body) }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("image source returned status %d", resp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return fmt.Errorf("image source returned content type %q", resp.Header.Get("Content-Type"))
	}
	cfg, _, err := image.DecodeConfig(io.LimitReader(resp.Body, maxThumbnailValidationBytes))
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("image dimensions are invalid")
	}
	return nil
}
