package minnanoavsource

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCollectDiscoversAndParsesProfiles(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		var body, contentType string
		switch req.URL.Path {
		case "/sitemap.xml":
			body = `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`
			contentType = "application/xml"
		case "/sitemap_actress_1.xml":
			body = `<urlset><url><loc>https://www.minnano-av.com/actress811239.html?ignored=1</loc></url></urlset>`
			contentType = "application/xml"
		case "/actress811239.html":
			body = `<html><head><meta property="og:image" content="https://www.minnano-av.com/thumb.jpg"></head><body><h1>安部麻沙美<span>あべまさみ / Abe Masami</span></h1><a href="https://al.dmm.co.jp/?lurl=https%3A%2F%2Fvideo.dmm.co.jp%2Fav%2Flist%2F%3Factress%3D28262">FANZA</a></body></html>`
			contentType = "text/html"
		default:
			return nil, &urlError{req.URL.String()}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	fetcher := actresscache.NewFetcher(client, 0, "test")
	var got []actresscache.Candidate
	err := New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: fetcher, SitemapURL: "https://www.minnano-av.com/sitemap.xml"}, func(candidate actresscache.Candidate) error {
		got = append(got, candidate)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "minnanoav:actress:811239", got[0].Key)
	require.Equal(t, 28262, got[0].DMMID)
	require.Equal(t, "Masami", got[0].FirstName)
	require.Equal(t, "Abe", got[0].LastName)
	require.Equal(t, "安部麻沙美", got[0].JapaneseName)
}

type urlError struct{ value string }

func (e *urlError) Error() string { return "unexpected URL: " + e.value }

var _ = require.NoError

func TestCollectRequiresFetcher(t *testing.T) {
	err := New().Collect(context.Background(), actresscache.SourceOptions{}, func(actresscache.Candidate) error { return nil })
	require.Error(t, err)
}

func TestProfileHelpersRejectInvalidURLs(t *testing.T) {
	_, ok := normalizeProfileURL("https://example.test/actress1.html")
	require.False(t, ok)
	_, ok = normalizeProfileURL("https://www.minnano-av.com/not-an-actress.html")
	require.False(t, ok)
	_, ok = profileID("https://www.minnano-av.com/actress1.html")
	require.True(t, ok)
	_, ok = profileID("bad")
	require.False(t, ok)
}

func TestCollectRejectsMalformedSitemap(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader("not xml")), Request: req}, nil
	})}
	fetcher := actresscache.NewFetcher(client, 0, "test")
	err := New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: fetcher, SitemapURL: "https://www.minnano-av.com/sitemap.xml"}, func(actresscache.Candidate) error { return nil })
	require.Error(t, err)
}

func TestCollectHonorsLimitSkipAndEmitErrors(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		var body, contentType string
		switch req.URL.Path {
		case "/sitemap.xml":
			body, contentType = `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`, "application/xml"
		case "/sitemap_actress_1.xml":
			body, contentType = `<urlset><url><loc>https://www.minnano-av.com/actress1.html</loc></url><url><loc>https://www.minnano-av.com/actress2.html</loc></url></urlset>`, "application/xml"
		default:
			body, contentType = `<html></html>`, "text/html"
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	fetcher := actresscache.NewFetcher(client, 0, "test")
	err := New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: fetcher, Limit: 1}, func(actresscache.Candidate) error { return errors.New("stop") })
	require.ErrorContains(t, err, "stop")
	count := 0
	err = New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: fetcher, ShouldSkip: func(string) bool { return true }}, func(actresscache.Candidate) error { count++; return nil })
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCollectRejectsMissingActressSitemaps(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader(`<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_video.xml</loc></sitemap></sitemapindex>`)), Request: req}, nil
	})}
	fetcher := actresscache.NewFetcher(client, 0, "test")
	err := New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: fetcher}, func(actresscache.Candidate) error { return nil })
	require.ErrorContains(t, err, "no actress URL sets")
}

func TestCollectRecordsProfileFailuresAndContinues(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/actress1.html" {
			return nil, errors.New("profile unavailable")
		}
		body := `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`
		contentType := "application/xml"
		if req.URL.Path == "/sitemap_actress_1.xml" {
			body = `<urlset><url><loc>https://www.minnano-av.com/actress1.html</loc></url><url><loc>https://www.minnano-av.com/actress2.html</loc></url></urlset>`
		}
		if req.URL.Path == "/actress2.html" {
			body = `<html><head><meta property="og:image" content="https://www.minnano-av.com/thumb.jpg"></head><body><h1>安部麻沙美<span>あべまさみ / Abe Masami</span></h1></body></html>`
			contentType = "text/html"
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	var mu sync.Mutex
	var failures []string
	var got []actresscache.Candidate
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Fetcher: actresscache.NewFetcher(client, 0, "test"),
		RecordFailure: func(candidate actresscache.Candidate, err error) error {
			mu.Lock()
			defer mu.Unlock()
			failures = append(failures, candidate.Key+":"+err.Error())
			return nil
		},
	}, func(candidate actresscache.Candidate) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, candidate)
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, failures, 1)
	assert.Len(t, got, 1)
	assert.Equal(t, "minnanoav:actress:2", got[0].Key)
}
func TestCollectReturnsProfileFetchError(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/actress1.html" {
			return nil, errors.New("profile unavailable")
		}
		body := `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`
		contentType := "application/xml"
		if req.URL.Path == "/sitemap_actress_1.xml" {
			body = `<urlset><url><loc>https://www.minnano-av.com/actress1.html</loc></url></urlset>`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	err := New().Collect(context.Background(), actresscache.SourceOptions{Fetcher: actresscache.NewFetcher(client, 0, "test")}, func(actresscache.Candidate) error { return nil })
	require.ErrorContains(t, err, "profile unavailable")
}
