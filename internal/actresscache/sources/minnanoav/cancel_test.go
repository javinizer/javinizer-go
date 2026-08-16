package minnanoavsource

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A crawl cancelled after discovery must surface an error and must not mark
// the source complete: reporting completion would let the builder prune
// unvisited records and publish a truncated cache.
func TestCollectCancelledCrawlDoesNotMarkComplete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/sitemap.xml":
			body = `<sitemapindex><sitemap><loc>https://www.minnano-av.com/sitemap_actress_1.xml</loc></sitemap></sitemapindex>`
		case "/sitemap_actress_1.xml":
			cancel()
			body = `<urlset><url><loc>https://www.minnano-av.com/actress811239.html</loc></url></urlset>`
		default:
			return nil, &urlError{req.URL.String()}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	fetcher := mustFetch(actresscache.NewFetcherWithOptions(client, 0, "test", nil, true))
	marked := false
	err := New().Collect(ctx, actresscache.SourceOptions{
		Fetcher:      fetcher,
		SitemapURL:   "https://www.minnano-av.com/sitemap.xml",
		MarkComplete: func() { marked = true },
		RecordFailure: func(actresscache.Candidate, error) error {
			return nil
		},
	}, func(actresscache.Candidate) error { return nil })
	require.Error(t, err)
	assert.False(t, marked, "cancelled crawl must not mark the source complete")
}
