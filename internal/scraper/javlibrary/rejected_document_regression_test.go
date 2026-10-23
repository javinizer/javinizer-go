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

type rejectedDocumentTransport func(*http.Request) (*http.Response, error)

func (f rejectedDocumentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// x/net/html's documented 512-node limit includes implied html/body nodes.
// These fixtures are only about 2KB and do not require a failing reader seam.
func TestRejectedDocumentRetainsSafeMetadataFallback(t *testing.T) {
	_, err := goquery.NewDocumentFromReader(strings.NewReader(strings.Repeat("<dl>", 510)))
	require.NoError(t, err)
	_, err = goquery.NewDocumentFromReader(strings.NewReader(strings.Repeat("<dl>", 511)))
	require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
	for _, tc := range []struct {
		name, script string
		rating       *models.Rating
	}{
		{"missing rating", "", nil},
		{"malformed rating", `<script>$rating = "1..2";</script>`, nil},
		{"valid rating", `<script>$rating = "8.0";</script>`, &models.Rating{Score: 8}},
	} {
		for _, search := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "direct detail", true: "search detail"}[search], func(t *testing.T) {
				s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
				calls := 0
				s.client = resty.New().SetTransport(rejectedDocumentTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<title>Known title - JAVLibrary</title><div id="video_info"></div>` + tc.script + strings.Repeat("<dl>", 511)))}, nil
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
				require.Equal(t, tc.rating, result.Rating)
				require.Equal(t, 1, calls)
			})
		}
	}

}
