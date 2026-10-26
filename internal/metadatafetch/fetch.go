// Package metadatafetch provides an opt-in, bounded fetch boundary. It is not
// wired to scrapers, cache writes, or any identity/snapshot authority.
package metadatafetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/imagevalidate"
)

// Route selects an explicit connection route. The zero value is invalid.
type Route string

const httpsScheme = "https"

const (
	// Direct connects to locally resolved and validated target addresses.
	Direct Route = "direct"
	// SOCKS5 sends only locally resolved, validated IP literals to a SOCKS5 proxy.
	SOCKS5 Route = "socks5"
)

// Limits contains mandatory positive resource budgets. RequestTimeout bounds
// caller waits through DNS, connections/redirects and response bodies, and is
// checked around image validation. It cannot preempt codec CPU or terminate
// native OS resolver work that outlives a canceled lookup (see the README).
// ConnectTimeout covers DNS and all address attempts for each hop, including
// SOCKS negotiation. HeaderTimeout and TLSHandshakeTimeout bound those phases.
type Limits struct {
	RequestTimeout      time.Duration
	ConnectTimeout      time.Duration
	HeaderTimeout       time.Duration
	TLSHandshakeTimeout time.Duration
	MaxHeaderBytes      int64
	MaxBodyBytes        int64
	MaxConcurrent       int
}

// RedirectPolicy bounds redirect hops. Cross-origin redirects require explicit
// opt-in; HTTPS to HTTP redirects are always rejected. Zero hops disables redirects.
type RedirectPolicy struct {
	MaxHops          int
	AllowCrossOrigin bool
}

// Options must explicitly select a route and finite limits. ProxyURL is required
// for SOCKS5 and forbidden for Direct. Only socks5:// URLs are supported; targets
// and proxy endpoints must resolve entirely to public addresses. Environment
// proxy settings, remote DNS, browser fetchers and direct fallback are not used.
type Options struct {
	Route     Route
	ProxyURL  string
	Limits    Limits
	Redirects RedirectPolicy
}

var (
	// ErrOptions indicates invalid budgets or unsupported route configuration.
	ErrOptions = errors.New("invalid metadata fetch options")
	// ErrBusy indicates all request/decode slots are occupied; no work was queued.
	ErrBusy = errors.New("metadata fetch concurrency limit reached")
	// ErrURL indicates an invalid URL, including userinfo or a non-HTTP scheme.
	ErrURL = errors.New("invalid metadata fetch URL")
	// ErrTarget indicates an invalid, unresolved or non-public destination.
	ErrTarget = errors.New("metadata fetch destination rejected")
	// ErrConnect indicates TCP/SOCKS failure without exposing URL/credentials.
	ErrConnect = errors.New("metadata fetch connection failed")
	// ErrRedirect indicates a rejected or excessive redirect.
	ErrRedirect = errors.New("metadata fetch redirect rejected")
	// ErrResponse indicates invalid headers, status, encoding or body framing.
	ErrResponse = errors.New("metadata fetch response rejected")
	// ErrBodyLimit indicates a response exceeding the encoded byte budget.
	ErrBodyLimit = errors.New("metadata fetch body exceeds limit")
	// ErrImage indicates image validation failed after a successful bounded fetch.
	ErrImage = errors.New("metadata fetch image rejected")
)

// Result owns the complete unencoded response bytes. SHA256 hashes precisely
// those bytes, not an identity, image rendering, cache key or metadata snapshot.
// FinalURL is the final validated URL; callers must not log its potentially
// sensitive query string. On failure no partial Result is returned.
type Result struct {
	Bytes    []byte
	FinalURL string
	SHA256   [sha256.Size]byte
}

type resolver func(context.Context, string) ([]net.IP, error)
type dialer func(context.Context, string, string) (net.Conn, error)

// Client is safe for concurrent use. A single instance bounds active requests
// and image decoding together. Returned bytes are owned by the caller.
type Client struct {
	options Options
	proxy   *url.URL
	slots   chan struct{}
	resolve resolver
	dial    dialer
	rootCAs *x509.CertPool // Test-only trust roots; New always uses system roots.
}

// New validates the complete configuration without network I/O. There is no
// fallback on invalid routing and no caller-supplied transport or TLS override.
func New(options Options) (*Client, error) {
	l := options.Limits
	if l.RequestTimeout <= 0 || l.ConnectTimeout <= 0 || l.HeaderTimeout <= 0 || l.TLSHandshakeTimeout <= 0 || l.MaxHeaderBytes <= 0 || l.MaxBodyBytes <= 0 || l.MaxBodyBytes == math.MaxInt64 || l.MaxConcurrent <= 0 || l.MaxConcurrent > 1024 || options.Redirects.MaxHops < 0 || options.Redirects.MaxHops > 20 {
		return nil, ErrOptions
	}
	var proxyURL *url.URL
	switch options.Route {
	case Direct:
		if options.ProxyURL != "" {
			return nil, ErrOptions
		}
	case SOCKS5:
		var err error
		proxyURL, err = parseProxy(options.ProxyURL)
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrOptions
	}
	return &Client{
		options: options, proxy: proxyURL, slots: make(chan struct{}, l.MaxConcurrent),
		resolve: func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		},
		dial: (&net.Dialer{}).DialContext,
	}, nil
}

// Fetch performs one bounded GET and returns only a complete successful (2xx)
// response. It sends no caller cookies, credentials, referer or custom headers.
func (c *Client) Fetch(ctx context.Context, rawURL string) (Result, error) {
	return c.fetch(ctx, rawURL, nil)
}

// FetchImage also validates JPEG/PNG/WebP bytes with imagevalidate while holding
// the same concurrency slot. Invalid image limits are rejected before any I/O.
// Codec CPU work cannot be interrupted; cancellation is checked around decoding.
func (c *Client) FetchImage(ctx context.Context, rawURL string, limits imagevalidate.Limits) (Result, error) {
	if limits.MaxEncodedBytes <= 0 || limits.MaxEncodedBytes == math.MaxInt64 || limits.MaxDimension <= 0 || limits.MaxPixels == 0 {
		return Result{}, ErrOptions
	}
	return c.fetch(ctx, rawURL, &limits)
}

func (c *Client) fetch(parent context.Context, rawURL string, imageLimits *imagevalidate.Limits) (Result, error) {
	if c == nil || c.slots == nil || parent == nil {
		return Result{}, ErrOptions
	}
	ctx, cancel := context.WithTimeout(parent, c.options.Limits.RequestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	current, err := parseTarget(rawURL)
	if err != nil {
		return Result{}, err
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return Result{}, ErrBusy
	}
	maxBytes := c.options.Limits.MaxBodyBytes
	if imageLimits != nil && imageLimits.MaxEncodedBytes < maxBytes {
		maxBytes = imageLimits.MaxEncodedBytes
	}
	for hop := 0; ; hop++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		response, cleanup, err := c.roundTrip(ctx, current)
		if err != nil {
			return Result{}, safeError(ctx, err)
		}
		// Never drain an untrusted redirect/error body. Disabling keepalives also
		// prevents connection reuse from bypassing a later hop's DNS validation.
		if isRedirect(response.StatusCode) {
			next, redirectErr := c.redirect(current, response, hop)
			cleanup()
			if redirectErr != nil {
				return Result{}, redirectErr
			}
			current = next
			continue
		}
		data, readErr := readResponse(ctx, response, maxBytes)
		cleanup()
		if readErr != nil {
			return Result{}, readErr
		}
		if imageLimits != nil {
			if _, _, err := imagevalidate.Validate(ctx, bytes.NewReader(data), *imageLimits); err != nil {
				if ctx.Err() != nil {
					return Result{}, ctx.Err()
				}
				return Result{}, ErrImage
			}
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		digest := sha256.Sum256(data)
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		return Result{Bytes: data, FinalURL: current.String(), SHA256: digest}, nil
	}
}

func readResponse(ctx context.Context, response *http.Response, maxBytes int64) ([]byte, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.StatusCode == http.StatusPartialContent || response.Uncompressed || !identityEncoding(response.Header) {
		return nil, ErrResponse
	}
	if response.ContentLength > maxBytes {
		return nil, ErrBodyLimit
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, ErrResponse
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrBodyLimit
	}
	return data, nil
}

func identityEncoding(header http.Header) bool {
	values := header.Values("Content-Encoding")
	return len(values) == 0 || len(values) == 1 && strings.EqualFold(strings.TrimSpace(values[0]), "identity")
}

func parseTarget(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != httpsScheme || u.Host == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || strings.Contains(u.Hostname(), "%") {
		return nil, ErrURL
	}
	for _, r := range u.Hostname() {
		if r > 127 {
			return nil, ErrURL
		}
	}
	if _, err := targetAddress(u); err != nil {
		return nil, ErrURL
	}
	return u, nil
}

func targetAddress(u *url.URL) (string, error) {
	port := u.Port()
	if port == "" {
		if strings.HasSuffix(u.Host, ":") {
			return "", ErrURL
		}
		port = "80"
		if u.Scheme == httpsScheme {
			port = "443"
		}
	}
	n, err := strconv.Atoi(port)
	if u.Hostname() == "" || err != nil || n < 1 || n > 65535 {
		return "", ErrURL
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), strconv.Itoa(n)), nil
}

func isRedirect(status int) bool {
	return status == http.StatusMovedPermanently || status == http.StatusFound || status == http.StatusSeeOther || status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect
}

func (c *Client) redirect(current *url.URL, response *http.Response, hop int) (*url.URL, error) {
	if hop >= c.options.Redirects.MaxHops || !identityEncoding(response.Header) {
		return nil, ErrRedirect
	}
	locations := response.Header.Values("Location")
	if len(locations) != 1 || locations[0] == "" {
		return nil, ErrRedirect
	}
	reference, err := url.Parse(locations[0])
	if err != nil {
		return nil, ErrRedirect
	}
	next, err := parseTarget(current.ResolveReference(reference).String())
	if err != nil || current.Scheme == httpsScheme && next.Scheme != httpsScheme {
		return nil, ErrRedirect
	}
	oldAddress, _ := targetAddress(current)
	newAddress, _ := targetAddress(next)
	if !c.options.Redirects.AllowCrossOrigin && (current.Scheme != next.Scheme || oldAddress != newAddress) {
		return nil, ErrRedirect
	}
	return next, nil
}

// safeError intentionally never returns transport/resolver errors, which can
// contain complete URLs, proxy credentials or other untrusted response text.
func safeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, safe := range []error{context.Canceled, context.DeadlineExceeded, ErrTarget, ErrConnect, ErrURL} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return context.DeadlineExceeded
	}
	return fmt.Errorf("%w: request failed", ErrResponse)
}
