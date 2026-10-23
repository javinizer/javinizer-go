package aventertainment

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
	for _, search := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct detail", true: "search detail"}[search], func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
			calls := 0
			s.client = resty.New().SetTransport(nestingLimitTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(strings.Repeat("<dl>", 511)))}, nil
			}))
			var result *models.ScraperResult
			if search {
				result, err = s.Search(context.Background(), "https://www.aventertainments.com/product_lists.aspx?item_no=ABC-123")
			} else {
				result, err = s.ScrapeURL(context.Background(), "https://www.aventertainments.com/product_lists.aspx?item_no=ABC-123")
			}
			require.Nil(t, result)
			require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
			require.Equal(t, 1, calls)
		})
	}
}

func TestOfflineNestingLimitRejectsSearchLinks(t *testing.T) {
	require.Empty(t, extractDetailLinks(strings.Repeat("<dl>", 511), "https://www.aventertainments.com"))
}
