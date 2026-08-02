package actresscache

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetcherBlocksInternalHosts(t *testing.T) {
	called := false
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	fetcher := NewFetcher(client, 0, "test")
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
	assert.False(t, called, "no blocked request should reach the network")
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

	called := false
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	fetcher := NewFetcher(client, 0, "test")
	fetcher.resolveTargets = true // custom transport test: force DNS validation
	_, _, err := fetcher.Get(context.Background(), "https://metadata.evil/leak", "*/*", 1024)
	var blockedErr *BlockedFetchError
	require.ErrorAs(t, err, &blockedErr)
	assert.False(t, called)
}

func TestFetcherAllowsPrivateHostsWhenOptedIn(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	fetcher := NewFetcher(client, 0, "test")
	fetcher.AllowPrivateHosts = true
	body, _, err := fetcher.Get(context.Background(), "http://127.0.0.1:8080/thumb.png", "*/*", 1024)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
}

func TestFetcherCapsRedirectChains(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"/loop"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	fetcher := NewFetcher(client, 0, "test")
	fetcher.AllowPrivateHosts = true // exercise the redirect cap, not the SSRF guard
	_, _, err := fetcher.Get(context.Background(), "http://127.0.0.1/loop", "*/*", 1024)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stopped after 10 redirects")
}

func TestFetcherBlocksCGNATMetadataRange(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	fetcher := NewFetcher(client, 0, "test")
	_, _, err := fetcher.Get(context.Background(), "http://100.100.100.200/latest/meta-data", "*/*", 1024)
	var blockedErr *BlockedFetchError
	require.ErrorAs(t, err, &blockedErr)
}

func TestFetcherBlocksRedirectToInternalHost(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://169.254.169.254/latest/meta-data"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})}
	fetcher := NewFetcher(client, 0, "test")
	_, _, err := fetcher.Get(context.Background(), "https://cdn.example/evil", "*/*", 1024)
	var blockedErr *BlockedFetchError
	require.ErrorAs(t, err, &blockedErr)
}

func TestValidateThumbnailRejectsInternalHost(t *testing.T) {
	_, err := ValidateThumbnail(context.Background(), NewFetcher(nil, 0, "test"), "http://169.254.169.254/latest/meta-data", 0, 1<<20)
	require.Error(t, err)
	var rejected *ThumbnailRejectedError
	require.ErrorAs(t, err, &rejected)
}
