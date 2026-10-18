package javdb

import (
	"context"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchRawAIRequiresUniqueDisplayCandidate(t *testing.T) {
	for _, cards := range [][]string{
		{"DV-818AI", "DV-819AI"},
		{"DV-819AI", "DV-818AI"},
		{"DV-00899AI", "DV-818AI"},
		{"DV-818AI", "DV-00899AI"},
		{"DV-899AI", "DV-818AI"},
	} {
		t.Run(cards[0]+"/"+cards[1], func(t *testing.T) {
			s := newMarkerTestScraper(map[string]string{
				"https://javdb.test/search?q=dv00899ai&f=all": remasterSearchPage(cards...),
				"https://javdb.test/v/dv818ai":                remasterDetailPage("DV-818AI"),
				"https://javdb.test/v/dv819ai":                remasterDetailPage("DV-819AI"),
				"https://javdb.test/v/dv899ai":                remasterDetailPage("DV-899AI"),
				// A catalog CID must not bypass search as a guessed JavDB video code.
				"https://javdb.test/v/dv00899ai": remasterDetailPage("DV-00899AI"),
			})
			result, err := s.Search(context.Background(), "dv00899ai")
			require.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestSearchRawAIUniqueDisplayIdentitySurvivesDetailFetch(t *testing.T) {
	for _, detail := range []string{"DV-818AI", "DV-0818AI", "DV-819AI"} {
		t.Run(detail, func(t *testing.T) {
			s := newMarkerTestScraper(map[string]string{
				"https://javdb.test/search?q=dv00899ai&f=all": remasterSearchPage("IPX-818AI", "DV-818AI", "DV-818AI"),
				"https://javdb.test/v/dv818ai":                remasterDetailPage(detail),
			})
			result, err := s.Search(context.Background(), "dv00899ai")
			if detail == "DV-819AI" {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, detail, result.ID)
			}
		})
	}
}

func TestSearchRawAIExactCatalogListingWins(t *testing.T) {
	for _, cards := range [][]string{
		{"dv00899ai", "DV-818AI", "DV-819AI"},
		{"DV-818AI", "DV-819AI", "dv00899ai"},
	} {
		s := newMarkerTestScraper(map[string]string{
			"https://javdb.test/search?q=dv00899ai&f=all": remasterSearchPage(cards...),
			"https://javdb.test/v/dv00899ai":              remasterDetailPage("DV-818AI"),
		})
		result, err := s.Search(context.Background(), "dv00899ai")
		require.NoError(t, err)
		assert.Equal(t, "DV-818AI", result.ID)
	}
}

func TestFindDetailSelectionRejectsEmptyQuery(t *testing.T) {
	s := newMarkerTestScraper(nil)
	url, selectedID, err := s.findDetailSelectionCtx(context.Background(), " ")
	require.ErrorContains(t, err, "movie ID cannot be empty")
	assert.Empty(t, url)
	assert.Empty(t, selectedID)
}

func TestSearchRawAIDetailRetryKeepsSelectedIdentity(t *testing.T) {
	for _, detail := range []string{"DV-818AI", "DV-819AI"} {
		t.Run(detail, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasPrefix(r.URL.Path, "/search"):
					_, _ = w.Write([]byte(remasterSearchPage("DV-818AI")))
				case r.URL.Path == "/v/dv818ai":
					if requests.Add(1) == 1 {
						_, _ = w.Write([]byte(`<html><body></body></html>`))
						return
					}
					_, _ = w.Write([]byte(remasterDetailPage(detail)))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			s := &scraper{client: resty.New(), enabled: true, baseURL: server.URL, rateLimiter: ratelimit.NewLimiter(0), settings: models.ScraperSettings{Enabled: true}}
			result, err := s.Search(context.Background(), "dv00899ai")
			require.Equal(t, int32(2), requests.Load())
			if detail == "DV-819AI" {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, detail, result.ID)
			}
		})
	}
}

func TestFindDetailSelectionRejectsExcessiveHTMLDepth(t *testing.T) {
	// x/net/html rejects a stack deeper than 512 elements even for an
	// in-memory reader. Exercise the actual parser rather than a test hook.
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=dv00899ai&f=all": strings.Repeat("<div>", 513),
	})
	url, selectedID, err := s.findDetailSelectionCtx(context.Background(), "dv00899ai")
	require.ErrorContains(t, err, "failed to parse search page HTML")
	require.ErrorContains(t, err, "exceeds 512 nodes")
	assert.Empty(t, url)
	assert.Empty(t, selectedID)
}
