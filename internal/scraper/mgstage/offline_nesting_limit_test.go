package mgstage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type nestingLimitTransport func(*http.Request) (*http.Response, error)

func (f nestingLimitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// x/net/html's documented 512-node limit includes implied html/body nodes.
// These fixtures are only about 2KB and do not require a failing reader seam.
func TestOfflineHTMLNestingLimit(t *testing.T) {
	_, err := goquery.NewDocumentFromReader(strings.NewReader(strings.Repeat("<dl>", 510)))
	require.NoError(t, err)
	_, err = goquery.NewDocumentFromReader(strings.NewReader(strings.Repeat("<dl>", 511)))
	require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
	t.Run("direct detail", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
		calls := 0
		s.client = resty.New().SetTransport(nestingLimitTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(strings.Repeat("<dl>", 511)))}, nil
		}))
		result, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/200GANA-1234/")
		require.Nil(t, result)
		require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
		require.Equal(t, 1, calls)
	})
}

func TestOfflineNestingLimitSearchAndDirectProbe(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	deep := strings.Repeat("<dl>", 511)
	require.Empty(t, s.findProductInSearchResults(deep, "200GANA-1234"))
	calls := 0
	s.client = resty.New().SetTransport(nestingLimitTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		body := deep
		if strings.Contains(req.URL.Path, "search") {
			body = `<a href="/product/product_detail/200GANA-1234/">200GANA-1234</a>`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	require.False(t, s.checkDirectURL(context.Background(), "https://www.mgstage.com/product/product_detail/200GANA-1234/"))
	require.Equal(t, 1, calls)
	calls = 0
	result, err := s.Search(context.Background(), "200GANA-1234")
	require.Nil(t, result)
	require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
	require.Equal(t, 2, calls)
}
