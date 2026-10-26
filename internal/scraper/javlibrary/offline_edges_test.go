package javlibrary

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

type offlineEdgesTransport func(*http.Request) (*http.Response, error)

func (f offlineEdgesTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfflineEdgesConstructorInvalidProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://environment.invalid:8080")
	proxy := &models.ProxyConfig{Enabled: true, DefaultProfile: "broken", Profiles: map[string]models.ProxyProfile{"broken": {URL: "http://%invalid"}}}
	s := newScraper(&models.ScraperSettings{Enabled: true, Timeout: 3}, proxy, models.FlareSolverrConfig{})
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	require.NotNil(t, s.client)
	require.Nil(t, s.flaresolverr)
	transport, ok := s.client.GetClient().Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, transport.Proxy, "invalid configured proxy must not fall back to environment proxy")
	require.Equal(t, int64(3), int64(s.client.GetClient().Timeout.Seconds()))
}

func TestOfflineEdgesSettingsValidation(t *testing.T) {
	for _, lang := range []string{"", "en", "ja", "cn", "tw"} {
		require.NoError(t, validateScraperSettings(&models.ScraperSettings{Language: lang, BaseURL: "https://www.javlibrary.com"}))
	}
	require.ErrorContains(t, validateScraperSettings(&models.ScraperSettings{Language: "fr"}), "language")
	require.Error(t, validateScraperSettings(&models.ScraperSettings{BaseURL: "https://unrelated.invalid"}))
}

func TestOfflineEdgesCancelledFetchDoesNotDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour), client: resty.New().SetTransport(offlineEdgesTransport(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled fetch dispatched")
		return nil, io.ErrUnexpectedEOF
	}))}
	body, err := s.fetchPageCtx(ctx, "https://www.javlibrary.com/en/?v=fixture")
	require.Empty(t, body)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "rate limit wait failed")
}

func TestOfflineEdgesSearchDetailTransportFailure(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	s.rateLimiter = ratelimit.NewLimiter(0)
	calls := 0
	s.client = resty.New().SetTransport(offlineEdgesTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			require.Equal(t, "/en/vl_searchbyid.php", req.URL.Path)
			require.Equal(t, "ABC-123", req.URL.Query().Get("keyword"))
			return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<div class="video" id="vid_fixture"><div class="id">ABC-123</div></div>`))}, nil
		}
		require.Equal(t, "fixture", req.URL.Query().Get("v"))
		return nil, io.ErrUnexpectedEOF
	}))
	result, err := s.Search(context.Background(), "ABC-123")
	require.Nil(t, result)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.ErrorContains(t, err, "failed to fetch detail page")
	require.Equal(t, 2, calls)
}

func TestOfflineEdgesMalformedRatingAndCoverGallery(t *testing.T) {
	s := &scraper{client: resty.New().SetTransport(offlineEdgesTransport(func(*http.Request) (*http.Response, error) {
		t.Error("non-DMM cover must not dispatch HTTP")
		return nil, io.ErrUnexpectedEOF
	}))}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="video_rating"><span class="num">not a score</span></div>`))
	require.NoError(t, err)
	require.Nil(t, s.extractRating("", doc))
	result, err := s.ParseHTMLRaw(`<title>Fixture title - JAVLibrary</title><div id="video_info"></div><img id="video_jacket_img" src="https://images.invalid/cover.jpg"><img data-src="https://images.invalid/cover.jpg"><img data-src="https://images.invalid/scene.jpg">`, "ABC-123", "https://www.javlibrary.com/en/?v=fixture", "en")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", result.ID)
	require.Equal(t, "https://images.invalid/cover.jpg", result.CoverURL)
	require.Equal(t, []string{"https://images.invalid/scene.jpg"}, result.ScreenshotURL)
}

func TestOfflineEdgesAdapterRejectsUnrenderableDocument(t *testing.T) {
	// ParseHTML is a public document adapter; callers can supply a DOM that
	// cannot be serialized, unlike the parser's own well-formed output.
	root := &html.Node{Type: html.DocumentNode}
	root.AppendChild(&html.Node{Type: html.ErrorNode})
	doc := goquery.NewDocumentFromNode(root)
	result, err := (&scraper{}).ParseHTML(doc, "https://www.javlibrary.com/en/?v=fixture")
	require.Nil(t, result)
	require.ErrorContains(t, err, "javlibrary: failed to render document to HTML")
}
