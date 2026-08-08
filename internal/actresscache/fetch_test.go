package actresscache

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fetchTransport func(*http.Request) (*http.Response, error)

type failAfterReader struct {
	read bool
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.ErrUnexpectedEOF
	}
	r.read = true
	copy(p, "partial")
	return len("partial"), nil
}

func (r *failAfterReader) Close() error { return nil }

func (f fetchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNormalizeRateLimitHost(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercase", input: "WWW.Example.COM", want: "example.com"},
		{name: "trailing dot", input: "example.com.", want: "example.com"},
		{name: "subdomain", input: "pics.example.com", want: "pics.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeRateLimitHost(tc.input))
		})
	}
}

func TestFetcherUsesSeparateHostLimiters(t *testing.T) {
	fetcher := mustFetcher(NewFetcher(nil, time.Second, "test"))
	www := fetcher.limiterForHost("www.example.com")
	root := fetcher.limiterForHost("example.com")
	images := fetcher.limiterForHost("pics.example.com")
	require.Same(t, www, root)
	assert.NotSame(t, www, images)
}

func TestFetcherUsesHostDelayOverride(t *testing.T) {
	fetcher := mustFetcher(NewFetcherWithHostDelays(nil, time.Second, "test", map[string]time.Duration{"pics.dmm.co.jp": 100 * time.Millisecond}))
	assert.Equal(t, 100*time.Millisecond, fetcher.delayForHost("pics.dmm.co.jp"))
	assert.Equal(t, time.Second, fetcher.delayForHost("www.minnano-av.com"))
}

func TestFetcherLimitsRedirectDestination(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "origin.test" {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://cdn.test/image.jpg"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	// Fixture transport: opt in to private hosts so the canned RoundTripper
	// is used verbatim, exactly like the pre-pinning behavior.
	fetcher := mustFetcher(NewFetcherWithOptions(client, 0, "test", map[string]time.Duration{"cdn.test": 0}, true))
	body, _, err := fetcher.Get(context.Background(), "https://origin.test/start", "*/*", 100)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	fetcher.mu.Lock()
	_, originLimited := fetcher.limiters["origin.test"]
	_, cdnLimited := fetcher.limiters["cdn.test"]
	fetcher.mu.Unlock()
	assert.True(t, originLimited)
	assert.True(t, cdnLimited)
}

func TestRetryDelayCapsServerWait(t *testing.T) {
	assert.Equal(t, maxRetryDelay, retryDelay(http.Header{"Retry-After": []string{"999999999999999999999"}}, 0))
}

func TestFetcherRetriesTransientStatus(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	fetcher := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true))
	body, _, err := fetcher.Get(context.Background(), "https://example.test/profile", "text/html", 100)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, 3, calls)
}

func TestFetcherRetriesSuccessfulResponseBodyReadFailure(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return &http.Response{StatusCode: http.StatusOK, Body: &failAfterReader{}, Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("complete")), Request: req}, nil
	})}
	body, _, err := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(context.Background(), "https://example.test/profile", "text/html", 100)
	require.NoError(t, err)
	assert.Equal(t, "complete", string(body))
	assert.Equal(t, 3, calls)
}

func TestFetcherRetryBackoffHonorsClientTimeout(t *testing.T) {
	client := &http.Client{Timeout: 20 * time.Millisecond, Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	started := time.Now()
	_, _, err := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(context.Background(), "https://example.test/profile", "text/html", 100)
	require.Error(t, err)
	assert.Less(t, time.Since(started), time.Second)
}

func TestFetcherReturnsTransientHTTPErrorAfterRetries(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	fetcher := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true))
	_, _, err := fetcher.Get(context.Background(), "https://example.test/profile", "text/html", 100)
	var statusErr *HTTPError
	require.ErrorAs(t, err, &statusErr)
	assert.True(t, statusErr.IsTransient())
	assert.Equal(t, 3, calls)
}
