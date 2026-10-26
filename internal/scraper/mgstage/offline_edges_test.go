package mgstage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestOfflineEdgesSiblingMetadata(t *testing.T) {
	// A leading unrelated column prevents the first-header row extractor from
	// matching. The following sibling pairs must retain the first matching value.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<table class="detail_data"><tr>
 <th>Other</th><td>Ignore</td>
 <th>品番：</th><td> 200GANA-123 </td><th>品番：</th><td>WRONG-999</td>
 <th>メーカー：</th><td><a> Fixture Studio </a></td><th>メーカー：</th><td><a>Wrong Studio</a></td>
 <th>ジャンル：</th><td><a>Drama</a><a>Drama</a><a> </a><a>Comedy</a></td>
 <th>出演：</th><td><a>山田 花子</a><a>山田 花子</a><a> </a><a>佐藤 花子</a></td>
 </tr></table>`))
	require.NoError(t, err)
	require.Equal(t, "200GANA-123", extractTableValue(doc, "品番："))
	require.Equal(t, "Fixture Studio", extractTableLinkValue(doc, "メーカー："))
	require.Empty(t, extractTableValue(doc, "Missing"))
	require.Empty(t, extractTableLinkValue(doc, "Missing"))
	require.Equal(t, []string{"Drama", "Comedy"}, extractGenres(doc))
	cast := extractActresses(doc)
	require.Len(t, cast, 2)
	require.Equal(t, "山田 花子", cast[0].JapaneseName)
	require.Equal(t, "佐藤 花子", cast[1].JapaneseName)
	require.NoError(t, validateScraperSettings(&models.ScraperSettings{}))
}

func TestOfflineEdgesRedirectedDetailAndMissingTrailer(t *testing.T) {
	calls := 0
	s := &scraper{rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, productURLForTest, req.URL.String())
		// Model the final response URL after a redirect without contacting a server.
		final := req.Clone(req.Context())
		final.URL = &url.URL{Scheme: "https", Host: "www.mgstage.com", Path: "/age-check/"}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Request: final, Body: io.NopCloser(strings.NewReader(`<table><tr><th>品番：</th><td>200GANA-123</td></tr></table>`))}, nil
	}))}
	require.False(t, s.checkDirectURL(context.Background(), productURLForTest), "an age-check response must not become a product match")
	require.Equal(t, 1, calls)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<iframe title="empty trailer"></iframe>`))
	require.NoError(t, err)
	require.Empty(t, extractTrailerURL(doc, s.client))
	require.Equal(t, 1, calls, "missing trailer source must not dispatch HTTP")
}
