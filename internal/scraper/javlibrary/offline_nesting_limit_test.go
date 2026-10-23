package javlibrary

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
	// A valid script rating allows the regex fallback to remain useful when DOM
	// parsing rejects the page. Missing-script nil-DOM handling is tracked as a
	// separate production bug rather than asserting its panic as desirable.
	for _, search := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct detail", true: "search detail"}[search], func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
			calls := 0
			s.client = resty.New().SetTransport(nestingLimitTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<title>Known title - JAVLibrary</title><div id="video_info"></div><script>$rating = "8.0";</script>` + strings.Repeat("<dl>", 511)))}, nil
			}))
			var result *models.ScraperResult
			if search {
				result, err = s.Search(context.Background(), "https://www.javlibrary.com/en/?v=javliay67q")
			} else {
				result, err = s.ScrapeURL(context.Background(), "https://www.javlibrary.com/en/?v=javliay67q")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "Known title", result.Title)
			require.Equal(t, "javliay67q", result.ID)
			require.Empty(t, result.Actresses)
			require.Equal(t, &models.Rating{Score: 8}, result.Rating)
			require.Equal(t, 1, calls)
		})
	}
}
