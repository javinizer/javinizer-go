package actresscache

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/ssrf"
)

func TestFetcherBlocksInternalHosts(t *testing.T) {
	// Default client + standard transport: blocks must trigger before any dial.
	fetcher := mustFetcher(NewFetcher(nil, 0, "test"))
	blocked := []string{
		"http://127.0.0.1/image.jpg",
		"http://[::1]/image.jpg",
		"http://10.1.2.3/image.jpg",
		"http://172.16.5.5/image.jpg",
		"http://192.168.0.10/image.jpg",
		"http://169.254.169.254/latest/meta-data",
		"http://0.0.0.0/image.jpg",
		"http://localhost/image.jpg",
		"http://service.localhost/image.jpg",
	}
	for _, u := range blocked {
		_, _, err := fetcher.Get(context.Background(), u, "*/*", 1024)
		var blockedErr *BlockedFetchError
		require.Truef(t, errors.As(err, &blockedErr), "expected BlockedFetchError for %s, got %v", u, err)
	}
}

func TestFetcherBlocksHostnameResolvingToPrivateIP(t *testing.T) {
	prev := lookupIP
	lookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		if host == "metadata.evil" {
			return []net.IP{net.ParseIP("169.254.169.254")}, nil
		}
		return prev(context.Background(), "ip", host)
	}
	defer func() { lookupIP = prev }()

	// Standard transport: resolveTargets is on by default.
	fetcher := mustFetcher(NewFetcher(nil, 0, "test"))
	_, _, err := fetcher.Get(context.Background(), "https://metadata.evil/leak", "*/*", 1024)
	var blockedErr *BlockedFetchError
	require.ErrorAs(t, err, &blockedErr)
}

func TestFetcherAllowsPrivateHostsWhenOptedIn(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	fetcher := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true))
	body, _, err := fetcher.Get(context.Background(), "http://127.0.0.1:8080/thumb.png", "*/*", 1024)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
}

func TestFetcherCapsRedirectChains(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"/loop"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	fetcher := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)) // exercise the redirect cap, not the SSRF guard
	_, _, err := fetcher.Get(context.Background(), "http://127.0.0.1/loop", "*/*", 1024)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stopped after 10 redirects")
}

func TestFetcherBlocksCGNATMetadataRange(t *testing.T) {
	fetcher := mustFetcher(NewFetcher(nil, 0, "test"))
	_, _, err := fetcher.Get(context.Background(), "http://100.100.100.200/latest/meta-data", "*/*", 1024)
	var blockedErr *BlockedFetchError
	require.ErrorAs(t, err, &blockedErr)
}

func TestFetcherFailsClosedForUnresolvableProxiedHost(t *testing.T) {
	prev := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("dns down")
	}
	defer func() { lookupIP = prev }()

	proxyTransport := &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		return url.Parse("http://proxy.corp.example:3128")
	}}
	fetcher := mustFetcher(NewFetcher(&http.Client{Transport: proxyTransport}, 0, "test"))
	_, _, err := fetcher.Get(context.Background(), "https://unresolvable.invalid/x", "*/*", 1024)
	var unverifiableErr *ssrf.UnverifiableHostError
	require.ErrorAs(t, err, &unverifiableErr, "proxied DNS failure must be transient-unverifiable, not a permanent block")
}

// An allowed-private fixture transport (trusted-mirror mode) takes no
// request-layer resolution, so a broken local resolver does not matter.
func TestFetcherToleratesLookupFailureForAllowedPrivateMirror(t *testing.T) {
	prev := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("dns down")
	}
	defer func() { lookupIP = prev }()

	called := false
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	fetcher := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true))
	body, _, err := fetcher.Get(context.Background(), "https://brand-new.example/file", "*/*", 1024)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "ok", string(body))
}

func TestFetcherBlocksRedirectToInternalHost(t *testing.T) {
	prev := lookupIP
	lookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	defer func() { lookupIP = prev }()
	// Pinnable transport with an in-memory conn stub: the 302 to the
	// link-local metadata address must be stopped by the redirect guard
	// before a second connection is made.
	client := &http.Client{Transport: &http.Transport{DialContext: serveOnce("HTTP/1.1 302 Found\r\nLocation: http://169.254.169.254/latest/meta-data\r\nContent-Length: 0\r\n\r\n")}}
	fetcher := mustFetcher(NewFetcher(client, 0, "test"))
	_, _, err := fetcher.Get(context.Background(), "http://cdn.example/evil", "*/*", 1024)
	var blockedErr *BlockedFetchError
	require.ErrorAs(t, err, &blockedErr)
}

func TestValidateThumbnailRejectsInternalHost(t *testing.T) {
	_, err := ValidateThumbnail(context.Background(), mustFetcher(NewFetcher(nil, 0, "test")), "http://169.254.169.254/latest/meta-data", 0, 1<<20)
	require.Error(t, err)
	var rejected *ThumbnailRejectedError
	require.ErrorAs(t, err, &rejected)
}
