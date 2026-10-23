package mgstage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestCurrentScopeMGStageCancellationBeforeRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour), client: resty.New().SetTransport(offlineFixtureTransport(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled rate-limit wait must not dispatch HTTP")
		return nil, context.Canceled
	}))}
	result, err := s.ScrapeURL(ctx, productURLForTest)
	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled)
	found, err := s.searchByID(ctx, "200GANA-123")
	require.Empty(t, found)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, s.checkDirectURL(ctx, productURLForTest))
}

const productURLForTest = "https://www.mgstage.com/product/product_detail/200GANA-123/"

func TestCurrentScopeMGStageCancellationBetweenSearchAndDetail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := 0
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "/search/cSearch.php", req.URL.Path)
		cancel()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<a href="` + productURLForTest + `">matched</a>`))}, nil
	}))}
	result, err := s.Search(ctx, "200GANA-123")
	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, requests)
}

func TestCurrentScopeMGStageRejectsLandingPage(t *testing.T) {
	s := newScriptedMGStageScraper(t, true, map[string]mockHTTPResponse{
		"/product/product_detail/200GANA-123/": {statusCode: http.StatusOK, body: `<title>MGStage</title>`},
	})
	result, err := s.ScrapeURL(context.Background(), productURLForTest)
	require.Nil(t, result)
	require.ErrorContains(t, err, "not found")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<title>MGStage</title>`))
	require.NoError(t, err)
	result, err = s.ParseHTML(doc, "https://www.mgstage.com/")
	require.Nil(t, result)
	require.ErrorContains(t, err, "movie unknown not found")
}

func TestCurrentScopeMGStageImageSelectionAndDuplicateCast(t *testing.T) {
	tests := []struct{ name, html, want string }{
		{"first enlargement wins", `<a class="enlarge" href="/jacket-a.jpg"></a><a class="enlarge" href="/jacket-b.jpg"></a>`, baseURL + "/jacket-a.jpg"},
		{"first thumbnail wins and becomes full size", `<img src="/jacket/ps.jpg"><img src="/cover/ps.jpg">`, baseURL + "/jacket/pl.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			require.Equal(t, tt.want, extractCoverURL(doc))
		})
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<table class="detail_data"><tr><th>出演：</th><td><a> </a><a>山田 花子</a><a>山田 花子</a></td></tr><tr><th>ジャンル：</th><td>Drama</td></tr></table><a class="sample_image"></a>`))
	require.NoError(t, err)
	actresses := extractActresses(doc)
	require.Len(t, actresses, 1)
	require.Equal(t, "山田 花子", actresses[0].JapaneseName)
	require.Equal(t, []string{"Drama"}, extractGenres(doc))
	require.Empty(t, extractScreenshots(doc))
	require.Equal(t, "Example", cleanTitle("Example: Site suffix"))
}

func TestCurrentScopeMGStageSearchDetailFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		transportErr bool
	}{
		{name: "detail status", status: http.StatusBadGateway},
		{name: "detail transport", transportErr: true},
		{name: "detail landing page", status: http.StatusOK, body: `<title>MGStage</title>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &scraper{rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				status, body := http.StatusOK, `<a href="`+productURLForTest+`">matched</a>`
				if calls == 1 {
					require.Equal(t, "/search/cSearch.php", req.URL.Path)
				} else {
					require.Equal(t, "/product/product_detail/200GANA-123/", req.URL.Path)
					if tc.transportErr {
						return nil, io.ErrUnexpectedEOF
					}
					status, body = tc.status, tc.body
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			}))}
			result, err := s.Search(context.Background(), "200GANA-123")
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, 2, calls)
			if tc.transportErr {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else if tc.status == http.StatusBadGateway {
				require.ErrorContains(t, err, "502")
			} else {
				require.ErrorContains(t, err, "not found")
			}
		})
	}
}

func TestCurrentScopeMGStageSearchStopsAtFirstMatch(t *testing.T) {
	s := &scraper{}
	require.Equal(t, productURLForTest, s.findProductInSearchResults(`<a href="`+productURLForTest+`">one</a><a href="/product/product_detail/200GANA-123/">two</a>`, "200GANA-123"))
	query, ok := s.ResolveSearchQuery("https://www.mgstage.com/product/product_detail/legacy/")
	require.True(t, ok)
	require.Equal(t, "LEGACY", query)
}
