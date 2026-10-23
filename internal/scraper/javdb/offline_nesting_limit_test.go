package javdb

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
		result, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/abc123")
		require.Nil(t, result)
		require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
		require.Equal(t, 1, calls)
	})
}

func TestOfflineNestingLimitJavDBSearchAndRetry(t *testing.T) {
	for _, mode := range []string{"search parse", "detail parse", "direct retry", "search detail retry"} {
		t.Run(mode, func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
			calls, details := 0, 0
			s.client = resty.New().SetTransport(nestingLimitTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				body := strings.Repeat("<dl>", 511)
				if req.URL.Path == "/search" && mode != "search parse" {
					body = `<div class="movie-list"><div class="item"><a href="/v/abc123"><div class="uid">ABC-123</div></a></div></div>`
				}
				if strings.HasPrefix(req.URL.Path, "/v/") {
					details++
					if strings.Contains(mode, "retry") && details == 1 {
						body = `<html><title>Loading</title></html>`
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))
			var result *models.ScraperResult
			var err error
			if mode == "direct retry" {
				result, err = s.ScrapeURL(context.Background(), "https://javdb.com/v/abc123")
			} else {
				result, err = s.Search(context.Background(), "ABC-123")
			}
			require.Nil(t, result)
			require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
			want := map[string]int{"search parse": 1, "detail parse": 2, "direct retry": 2, "search detail retry": 3}[mode]
			require.Equal(t, want, calls)
		})
	}
}
