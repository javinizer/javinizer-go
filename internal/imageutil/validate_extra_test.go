package imageutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteImageWithSafeClientGuards(t *testing.T) {
	require.Error(t, ValidateRemoteImageWithSafeClient(t.Context(), nil, "https://example.com/a.jpg", "", ""))
	require.Error(t, ValidateRemoteImageWithSafeClient(t.Context(), http.DefaultClient, "http://127.0.0.1/a.jpg", "", ""))
}

func TestValidateRemoteImageWithSafeClientHonorsRedirectPolicy(t *testing.T) {
	policyErr := errors.New("redirect denied")
	client := &http.Client{
		Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://example.org/image.png"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error { return policyErr },
	}
	err := ValidateRemoteImageWithSafeClient(t.Context(), client, "https://example.com/start", "agent", "")
	require.ErrorIs(t, err, policyErr)
}

func TestValidateRemoteImageWithSafeClientWrapsHTTPTransportAndLimitsRedirects(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, ValidateRemoteImageWithSafeClient(ctx, &http.Client{}, "https://example.com/image", "", ""))
	transport := &http.Transport{}
	err := ValidateRemoteImageWithSafeClient(ctx, &http.Client{Transport: transport}, "https://example.com/image", "", "")
	require.Error(t, err)
	require.Nil(t, transport.DialContext)

	redirects := 0
	client := &http.Client{Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
		redirects++
		location := fmt.Sprintf("https://example.com/image/%d", redirects)
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	err = ValidateRemoteImageWithSafeClient(t.Context(), client, "https://example.com/start", "", "")
	require.ErrorContains(t, err, "stopped after 10 redirects")
	assert.Equal(t, 10, redirects)
}

func TestValidateRemoteImageWithSafeClientPinsProxyTarget(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	require.NoError(t, err)
	var targetHost string
	var hostHeader string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		targetHost = req.URL.Host
		hostHeader = req.Host
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	require.NoError(t, ValidateRemoteImageWithSafeClient(t.Context(), client, "http://1.1.1.1/image.png", "agent", ""))
	assert.Equal(t, "1.1.1.1", targetHost)
	assert.Equal(t, "1.1.1.1", hostHeader)
	require.Error(t, ValidateRemoteImageWithSafeClient(t.Context(), client, "https://1.1.1.1/image.png", "agent", ""))

	proxyErr := errors.New("proxy selection failed")
	errorClient := &http.Client{Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) { return nil, proxyErr }}}
	require.ErrorIs(t, ValidateRemoteImageWithSafeClient(t.Context(), errorClient, "http://1.1.1.1/image.png", "agent", ""), proxyErr)

	pinned := &pinnedProxyTransport{base: &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}
	privateReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1/image.png", nil)
	require.NoError(t, err)
	_, err = pinned.RoundTrip(privateReq)
	require.ErrorContains(t, err, "private/internal")
	httpsReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://1.1.1.1/image.png", nil)
	require.NoError(t, err)
	pinnedNilTLS := &pinnedProxyTransport{base: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	_, err = pinnedNilTLS.RoundTrip(httpsReq)
	require.Error(t, err)
	_, err = pinned.RoundTrip(httpsReq)
	require.Error(t, err)

	dialErr := errors.New("dial stopped")
	var dialedAddr string
	socks := &pinnedProxyTransport{
		base: &http.Transport{DialContext: func(_ context.Context, _ string, addr string) (net.Conn, error) {
			dialedAddr = addr
			return nil, dialErr
		}},
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		},
	}
	socksReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://public.example/image.png", nil)
	require.NoError(t, err)
	_, err = socks.RoundTrip(socksReq)
	require.ErrorIs(t, err, dialErr)
	assert.Equal(t, "1.1.1.1:80", dialedAddr)
	privateSocks := *socks
	privateSocks.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	_, err = privateSocks.RoundTrip(socksReq)
	require.ErrorContains(t, err, "private/internal")

	secureProxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(secureProxy.Close)
	secureProxyURL, err := url.Parse(secureProxy.URL)
	require.NoError(t, err)
	proxyRoots := x509.NewCertPool()
	proxyRoots.AddCert(secureProxy.Certificate())
	secureClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(secureProxyURL), TLSClientConfig: &tls.Config{RootCAs: proxyRoots}}}
	require.Error(t, ValidateRemoteImageWithSafeClient(t.Context(), secureClient, "https://1.1.1.1/image.png", "agent", ""))
}

func TestResolvePublicTargetIP(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	_, err := resolvePublicTargetIP(t.Context(), "example.com", func(context.Context, string) ([]net.IPAddr, error) { return nil, lookupErr })
	require.ErrorIs(t, err, lookupErr)
	_, err = resolvePublicTargetIP(t.Context(), "example.com", func(context.Context, string) ([]net.IPAddr, error) { return nil, nil })
	require.ErrorContains(t, err, "no addresses")
	_, err = resolvePublicTargetIP(t.Context(), "example.com", func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	require.ErrorContains(t, err, "private/internal")
	ip, err := resolvePublicTargetIP(t.Context(), "example.com", func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
	})
	require.NoError(t, err)
	require.Equal(t, net.ParseIP("1.1.1.1"), ip)
}

func TestResolvePublicDialAddress(t *testing.T) {
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
	}
	_, err := resolvePublicDialAddress(t.Context(), "invalid", lookup)
	require.ErrorContains(t, err, "invalid address")
	addr, err := resolvePublicDialAddress(t.Context(), "public.example:443", lookup)
	require.NoError(t, err)
	require.Equal(t, "1.1.1.1:443", addr)
	_, err = resolvePublicDialAddress(t.Context(), "private.example:80", func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	require.ErrorContains(t, err, "private/internal")
}

func TestDialTLSProxy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	dialer := &net.Dialer{}
	conn, err := dialTLSProxy(t.Context(), "tcp", server.Listener.Addr().String(), dialer.DialContext, &tls.Config{RootCAs: roots})
	require.NoError(t, err)
	require.Equal(t, server.Listener.Addr().String(), conn.RemoteAddr().String())
	require.NoError(t, conn.Close())

	_, err = dialTLSProxy(t.Context(), "tcp", "invalid", dialer.DialContext, &tls.Config{})
	require.Error(t, err)
	dialErr := errors.New("dial failed")
	_, err = dialTLSProxy(t.Context(), "tcp", "proxy.example:443", func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErr
	}, &tls.Config{})
	require.ErrorIs(t, err, dialErr)

	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(plain.Close)
	_, err = dialTLSProxy(t.Context(), "tcp", plain.Listener.Addr().String(), dialer.DialContext, &tls.Config{InsecureSkipVerify: true})
	require.Error(t, err)
}

func TestValidateRemoteImageWithClientRequestAndTransportErrors(t *testing.T) {
	require.Error(t, ValidateRemoteImageWithClient(t.Context(), nil, "https://example.com", "", ""))
	require.Error(t, ValidateRemoteImageWithClient(t.Context(), http.DefaultClient, "://bad", "", ""))

	transportErr := errors.New("offline")
	client := &http.Client{Transport: validationTransport(func(*http.Request) (*http.Response, error) { return nil, transportErr })}
	err := ValidateRemoteImageWithClient(context.Background(), client, "https://example.com/image", "agent", "https://example.com/")
	require.ErrorIs(t, err, transportErr)
}

func TestValidateRemoteImageWithClientRejectsZeroDimensions(t *testing.T) {
	const magic = "zero-dimension-image"
	image.RegisterFormat("zero-dimension-test", magic, func(io.Reader) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 0, 0)), nil
	}, func(io.Reader) (image.Config, error) {
		return image.Config{Width: 0, Height: 1}, nil
	})
	client := &http.Client{Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "agent", req.Header.Get("User-Agent"))
		assert.Equal(t, "https://example.com/ref", req.Header.Get("Referer"))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/x-zero"}}, Body: io.NopCloser(strings.NewReader(magic)), Request: req}, nil
	})}
	err := ValidateRemoteImageWithClient(t.Context(), client, "https://example.com/image", "agent", "https://example.com/ref")
	require.ErrorContains(t, err, "dimensions are invalid")
}
