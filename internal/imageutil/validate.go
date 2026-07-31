package imageutil

import (
	"context"
	"crypto/tls"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	_ "golang.org/x/image/webp"
)

// maxThumbnailValidationBytes ...
const maxThumbnailValidationBytes = 2 * 1024 * 1024

var validateRemoteImageWithClient = ValidateRemoteImageWithClient

func resolvePublicTargetIP(ctx context.Context, host string, lookup func(context.Context, string) ([]net.IPAddr, error)) (net.IP, error) {
	addresses, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("SSRF blocked: failed to resolve hostname %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("SSRF blocked: hostname %q resolved to no addresses", host)
	}
	for _, address := range addresses {
		ip := address.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return nil, fmt.Errorf("SSRF blocked: %s resolves to private/internal IP", host)
		}
	}
	return addresses[0].IP, nil
}

func resolvePublicDialAddress(ctx context.Context, addr string, lookup func(context.Context, string) ([]net.IPAddr, error)) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
	}
	ip, err := resolvePublicTargetIP(ctx, host, lookup)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ip.String(), port), nil
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
	tlsConfig := config.Clone()
	tlsConfig.ServerName = host
	tlsConn := tls.Client(conn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
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
		originalDialContext := transport.DialContext
		if originalDialContext == nil {
			originalDialContext = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			pinnedAddr, err := resolvePublicDialAddress(ctx, addr, lookup)
			if err != nil {
				return nil, err
			}
			return originalDialContext(ctx, network, pinnedAddr)
		}
		return transport.RoundTrip(req)
	}
	host := req.URL.Hostname()
	ip, err := resolvePublicTargetIP(req.Context(), host, lookup)
	if err != nil {
		return nil, err
	}
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	pinnedReq := req.Clone(req.Context())
	pinnedURL := *req.URL
	pinnedURL.Host = net.JoinHostPort(ip.String(), port)
	pinnedReq.URL = &pinnedURL
	pinnedReq.Host = req.URL.Host
	transport := t.base.Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	if req.URL.Scheme == "https" {
		baseTLSConfig := transport.TLSClientConfig.Clone()
		targetTLSConfig := baseTLSConfig.Clone()
		targetTLSConfig.ServerName = host
		transport.TLSClientConfig = targetTLSConfig
		if proxyURL.Scheme == "https" && transport.DialTLSContext == nil {
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
	if resp != nil {
		resp.Request = req
	}
	return resp, err
}

// ValidateRemoteImage ...
func ValidateRemoteImage(ctx context.Context, rawURL string) error {
	if err := ssrf.CheckURL(rawURL); err != nil {
		return err
	}
	return validateRemoteImageWithClient(ctx, ssrf.NewSSRFSafeClient(30*time.Second), rawURL, config.DefaultUserAgent, httpclient.ResolveMediaReferer(rawURL, ""))
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
	return ValidateRemoteImageWithClient(ctx, &safeClient, rawURL, userAgent, referer)
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
