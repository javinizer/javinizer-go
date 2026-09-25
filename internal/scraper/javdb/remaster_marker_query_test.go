package javdb

import (
	"context"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// remasterSearchPage renders a JavDB search result page listing the given IDs.
func remasterSearchPage(ids ...string) string {
	var b strings.Builder
	b.WriteString(`<html><body><div class="movie-list">`)
	for _, id := range ids {
		slug := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(id))
		b.WriteString(`<div class="item"><a href="/v/` + slug + `">` +
			`<div class="video-title"><strong>` + id + `</strong> Some Title</div>` +
			`<div class="uid">` + id + `</div></a></div>`)
	}
	b.WriteString(`</div></body></html>`)
	return b.String()
}

// remasterDetailPage renders a minimal JavDB detail page for the given ID.
func remasterDetailPage(id string) string {
	return `<html><body>
		<h2 class="title is-4"><strong>` + id + `</strong> Some Title</h2>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>日期:</strong><span class="value">2024-01-02</span></div>
		</div>
	</body></html>`
}

func newMarkerTestScraper(responses map[string]string) *scraper {
	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: responses})
	return &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}
}

func TestRemasterMarkerSuffix(t *testing.T) {
	assert.Equal(t, "H", remasterMarkerSuffix("RCT-156H"))
	assert.Equal(t, "H", remasterMarkerSuffix("rct-156-h"))
	assert.Equal(t, "HD", remasterMarkerSuffix("RCT-156HD"))
	assert.Equal(t, "AI", remasterMarkerSuffix("DV-818AI"))
	assert.Equal(t, "AI", remasterMarkerSuffix("dv818ai"))
	assert.Empty(t, remasterMarkerSuffix("RCT-156"))
	assert.Empty(t, remasterMarkerSuffix("IPX-535A"), "a part letter is not a remaster marker")
	assert.Empty(t, remasterMarkerSuffix("IPX-535"))
	assert.Empty(t, remasterMarkerSuffix("300MIUM-700"))
}

// A folded marker query must not variant-match the base release when only the
// original is listed: RCT-156H (folded from RCT-156-HD) targets the remaster,
// and returning the original's page would scrape the wrong movie.
func TestSearchMarkerQueryDoesNotResolveBaseRelease(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=RCT-156H&f=all": remasterSearchPage("RCT-156"),
		"https://javdb.test/v/rct156":                remasterDetailPage("RCT-156"),
	})
	_, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err, "marker query must miss honestly when only the base release is listed")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// The single-detail-link fallback must not return the base release for an
// AI-folded query either.
func TestSearchMarkerQuerySkipsSingleLinkFallback(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=DV-818AI&f=all": remasterSearchPage("DV-818"),
		"https://javdb.test/v/dv818":                 remasterDetailPage("DV-818"),
	})
	_, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "a lone base-release listing must not stand in for an AI-folded query")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// Control: when both releases are listed, the marker query resolves the
// remaster — the exact identity wins.
func TestSearchMarkerQueryResolvesListedRemaster(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=RCT-156H&f=all": remasterSearchPage("RCT-156", "RCT-156H"),
		"https://javdb.test/v/rct156":                remasterDetailPage("RCT-156"),
		"https://javdb.test/v/rct156h":               remasterDetailPage("RCT-156H"),
	})
	res, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "RCT-156H", res.ID)
}

// Control: a marker query whose remaster is the sole listing still resolves
// it — the single-link fallback is only skipped for marker queries whose
// listed items did not match.
func TestSearchMarkerQueryResolvesSoleListedRemaster(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=DV-818AI&f=all": remasterSearchPage("DV-818AI"),
		"https://javdb.test/v/dv818ai":               remasterDetailPage("DV-818AI"),
	})
	res, err := s.Search(context.Background(), "DV-818AI")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "DV-818AI", res.ID)
}

// Control: a padding-equal marker spelling (RCT-0156H vs RCT-156H) is the
// same release and still resolves.
func TestSearchMarkerQueryAcceptsPaddingEqualSpelling(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=RCT-156H&f=all": remasterSearchPage("RCT-0156H"),
		"https://javdb.test/v/rct0156h":              remasterDetailPage("RCT-0156H"),
	})
	res, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "RCT-0156H", res.ID)
}
