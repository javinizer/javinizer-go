package minnanoavsource

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func xmlResponse(req *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func profileFixture() string {
	return `<html><head><meta property="og:image" content="https://www.minnano-av.com/a.jpg"></head><body><h1>花子<span>はなこ / Yamada Hanako</span></h1></body></html>`
}

func sitemapTransport(profileResult func(*http.Request) (*http.Response, error)) testTransport {
	return func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/custom.xml", "/sitemap.xml":
			return xmlResponse(req, `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`), nil
		case "/sitemap_actress_1.xml":
			return xmlResponse(req, `<urlset><url><loc>https://www.minnano-av.com/actress1.html</loc></url></urlset>`), nil
		default:
			return profileResult(req)
		}
	}
}

func TestCollectUsesParameterSitemapMarksSeenAndCompletes(t *testing.T) {
	client := &http.Client{Transport: sitemapTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(profileFixture())), Request: req}, nil
	})}
	var seen []string
	complete := false
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Fetcher:      actresscache.NewFetcher(client, 0, "test"),
		Parameters:   map[string]string{"minnanoav.sitemap": "https://www.minnano-av.com/custom.xml"},
		Workers:      8,
		MarkSeen:     func(key string) { seen = append(seen, key) },
		MarkComplete: func() { complete = true },
	}, func(actresscache.Candidate) error { return nil })
	require.NoError(t, err)
	assert.Equal(t, []string{"minnanoav:actress:1"}, seen)
	assert.True(t, complete)
}

func TestCollectUsesGenericSitemapParameter(t *testing.T) {
	requested := ""
	client := &http.Client{Transport: sitemapTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(profileFixture())), Request: req}, nil
	})}
	fetcher := actresscache.NewFetcher(&http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, ".xml") && strings.Contains(req.URL.Path, "generic") {
			requested = req.URL.String()
		}
		return client.Transport.RoundTrip(req)
	})}, 0, "test")
	err := New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: fetcher, Parameters: map[string]string{"sitemap": "https://www.minnano-av.com/generic.xml"}}, func(actresscache.Candidate) error { return nil })
	// The custom transport intentionally has no generic sitemap fixture; the request itself proves fallback selection.
	require.Error(t, err)
	assert.Contains(t, requested, "generic.xml")
}

func TestCollectReturnsEmitterErrorDuringEnqueue(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/sitemap.xml":
			return xmlResponse(req, `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`), nil
		case "/sitemap_actress_1.xml":
			return xmlResponse(req, `<urlset><url><loc>https://www.minnano-av.com/actress1.html</loc></url><url><loc>https://www.minnano-av.com/actress2.html</loc></url><url><loc>https://www.minnano-av.com/actress3.html</loc></url></urlset>`), nil
		default:
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(profileFixture())), Request: req}, nil
		}
	})}
	want := errors.New("emit failed")
	err := New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: actresscache.NewFetcher(client, 0, "test"), Workers: 1}, func(actresscache.Candidate) error { return want })
	require.ErrorIs(t, err, want)
}

func TestCollectReturnsRecordFailureError(t *testing.T) {
	client := &http.Client{Transport: sitemapTransport(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("profile unavailable")
	})}
	want := errors.New("state unavailable")
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Fetcher:       actresscache.NewFetcher(client, 0, "test"),
		RecordFailure: func(actresscache.Candidate, error) error { return want },
	}, func(actresscache.Candidate) error { return nil })
	require.ErrorIs(t, err, want)
}

func TestCollectStopsAfterCanceledProfileFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: sitemapTransport(func(req *http.Request) (*http.Response, error) {
		cancel()
		return nil, context.Canceled
	})}
	failures := 0
	err := New().Collect(ctx, actresscache.SourceOptions{
		Fetcher:       actresscache.NewFetcher(client, 0, "test"),
		RecordFailure: func(actresscache.Candidate, error) error { failures++; return nil },
	}, func(actresscache.Candidate) error { return nil })
	require.NoError(t, err)
	assert.Equal(t, 1, failures)
}

func TestDiscoverProfileURLsReturnsIndexFetchError(t *testing.T) {
	fetcher := actresscache.NewFetcher(&http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("index unavailable")
	})}, 0, "test")
	_, err := discoverProfileURLs(context.Background(), fetcher, "https://www.minnano-av.com/sitemap.xml")
	require.ErrorContains(t, err, "index unavailable")
}

func TestDiscoverProfileURLsRejectsNestedFailures(t *testing.T) {
	tests := []struct {
		name   string
		nested func(*http.Request) (*http.Response, error)
		want   string
	}{
		{"fetch", func(*http.Request) (*http.Response, error) { return nil, errors.New("down") }, "fetch"},
		{"parse", func(req *http.Request) (*http.Response, error) { return xmlResponse(req, "<broken"), nil }, "parse"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/sitemap.xml" {
					return xmlResponse(req, `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`), nil
				}
				return tc.nested(req)
			})}
			_, err := discoverProfileURLs(context.Background(), actresscache.NewFetcher(client, 0, "test"), "https://www.minnano-av.com/sitemap.xml")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestDiscoverProfileURLsFiltersSortsAndDeduplicates(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/sitemap.xml":
			return xmlResponse(req, `<sitemapindex>
<sitemap><loc>://bad</loc></sitemap><sitemap><loc>http://www.minnano-av.com/sitemap_actress_0.xml</loc></sitemap>
<sitemap><loc>https://www.minnano-av.com/sitemap_actress_list_index.xml</loc></sitemap>
<sitemap><loc>https://www.minnano-av.com/sitemap_actress_2.xml</loc></sitemap><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`), nil
		default:
			return xmlResponse(req, `<urlset><url><loc>https://minnano-av.com/actress2.html?x=1</loc></url><url><loc>https://www.minnano-av.com/actress2.html</loc></url><url><loc>http://www.minnano-av.com/actress3.html</loc></url><url><loc>https://example.test/actress4.html</loc></url></urlset>`), nil
		}
	})}
	urls, err := discoverProfileURLs(context.Background(), actresscache.NewFetcher(client, 0, "test"), "https://www.minnano-av.com/sitemap.xml")
	require.NoError(t, err)
	assert.Equal(t, []string{"https://www.minnano-av.com/actress2.html"}, urls)
}

func TestURLHelpersCoverInvalidInputs(t *testing.T) {
	_, ok := normalizeProfileURL("://bad")
	assert.False(t, ok)
	_, ok = profileID("http://[::1")
	assert.False(t, ok)
	assert.False(t, isMinnanoURL(nil))
	assert.False(t, isMinnanoURL(&url.URL{Scheme: "http", Host: "www.minnano-av.com"}))
	assert.True(t, isMinnanoURL(&url.URL{Scheme: "https", Host: "images.minnano-av.com."}))
}
