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
	"mime"
	"net"
	"net/http"
	"net/netip"
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

var blockedTargetPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/3"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("4000::/2"),
	netip.MustParsePrefix("8000::/1"),
}

func isPublicTargetIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range blockedTargetPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func resolvePublicTargetIPs(ctx context.Context, host string, lookup func(context.Context, string) ([]net.IPAddr, error)) ([]net.IP, error) {
	addresses, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("SSRF blocked: failed to resolve hostname %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("SSRF blocked: hostname %q resolved to no addresses", host)
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
		return &tls.Config{}
	}
	return config.Clone()
}

func dialTLSProxy(ctx context.Context, network, addr string, dial func(context.Context, string, string) (net.Conn, error), config *tls.Config) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	conn, err := dial(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	tlsConfig := cloneTLSConfig(config)
	tlsConfig.ServerName = host
	tlsConn := tls.Client(conn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
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

func roundTripHTTPProxy(ctx context.Context, req *http.Request, proxyURL *url.URL, pinnedHost string, transport *http.Transport) (*http.Response, error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		if proxyURL.Scheme == httpsScheme {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "443")
		} else {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "80")
		}
	}
	dial := transport.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	var conn net.Conn
	var err error
	if proxyURL.Scheme == httpsScheme {
		conn, err = dialTLSProxy(ctx, "tcp", proxyAddr, dial, transport.TLSClientConfig)
	} else {
		conn, err = dial(ctx, "tcp", proxyAddr)
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
	headerReader := &responseHeaderLimitReader{reader: conn, remaining: maxHeaderBytes + responseHeaderReadSlop}
	bufferedReader := bufio.NewReader(headerReader)
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
		resp.Body = &proxyResponseBody{ReadCloser: resp.Body, conn: conn, done: done}
		return resp, nil
	}
}

func isRetryableProxyStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

type pinnedProxyTransport struct {
	base   *http.Transport
	lookup func(context.Context, string) ([]net.IPAddr, error)
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
	if proxyURL == nil {
		transport := t.base.Clone()
		transport.Proxy = nil
		transport.DisableKeepAlives = true
		originalDialContext := transport.DialContext
		if originalDialContext == nil {
			originalDialContext = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialPublicTarget(ctx, network, addr, lookup, originalDialContext)
		}
		return transport.RoundTrip(req)
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
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DisableKeepAlives = true
		if req.URL.Scheme == "http" && (proxyURL.Scheme == "http" || proxyURL.Scheme == httpsScheme) {
			resp, err := roundTripHTTPProxy(req.Context(), req, proxyURL, pinnedHost, transport)
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
			targetTLSConfig := baseTLSConfig.Clone()
			targetTLSConfig.ServerName = host
			transport.TLSClientConfig = targetTLSConfig
			if proxyURL.Scheme == httpsScheme && transport.DialTLSContext == nil {
				dial := transport.DialContext
				if dial == nil {
					dial = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
				}
				transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialTLSProxy(ctx, network, addr, dial, baseTLSConfig)
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

// ValidateRemoteImage ...
func ValidateRemoteImage(ctx context.Context, rawURL string) error {
	if err := ssrf.CheckURL(rawURL); err != nil {
		return err
	}
	return ValidateRemoteImageWithSafeClient(ctx, ssrf.NewSSRFSafeClient(30*time.Second), rawURL, config.DefaultUserAgent, httpclient.ResolveMediaReferer(rawURL, ""))
}

// ValidateRemoteImageWithSafeClient ...
func ValidateRemoteImageWithSafeClient(ctx context.Context, client *http.Client, rawURL, userAgent, referer string) error {
	if err := ssrf.CheckURL(rawURL); err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("image validator client is nil")
	}
	safeClient := *client
	if client.Transport == nil {
		safeClient.Transport = &pinnedProxyTransport{base: http.DefaultTransport.(*http.Transport).Clone()}
	} else if transport, ok := client.Transport.(*http.Transport); ok {
		safeClient.Transport = &pinnedProxyTransport{base: transport.Clone()}
	}
	previousCheckRedirect := client.CheckRedirect
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := ssrf.CheckURL(req.URL.String()); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
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
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
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
