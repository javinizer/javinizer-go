package mgstage

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildMGStageDetailHTMLV5 creates a MGStage detail page
func buildMGStageDetailHTMLV5(id, title, date, runtime, maker, label, series, genres, actresses, coverURL string) string {
	genreLinks := ""
	for _, g := range strings.Split(genres, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			genreLinks += fmt.Sprintf(`<a href="/genre/%s">%s</a>`, g, g)
		}
	}

	actressLinks := ""
	for _, a := range strings.Split(actresses, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			actressLinks += fmt.Sprintf(`<a href="/actress/%s">%s</a>`, a, a)
		}
	}

	return fmt.Sprintf(`<html>
<head><title>%s | MGS</title></head>
<body>
<table>
	<tr><th>品番：</th><td>%s</td></tr>
	<tr><th>配信開始日：</th><td>%s</td></tr>
	<tr><th>収録時間：</th><td>%s min</td></tr>
	<tr><th>メーカー：</th><td><a href="/maker/1">%s</a></td></tr>
	<tr><th>レーベル：</th><td><a href="/label/1">%s</a></td></tr>
	<tr><th>シリーズ：</th><td><a href="/series/1">%s</a></td></tr>
</table>
<div class="tag_area">%s</div>
<div class="actor_area">%s</div>
<img class="enlarge_image" src="%s" alt="cover"/>
</body>
</html>`, title, id, date, runtime, maker, label, series, genreLinks, actressLinks, coverURL)
}

// Search's URL lookup does not check registry enablement directly.
func TestSearchV5_Disabled(t *testing.T) {
	s := newMissingMGStageScraper(t, false)
	_, err := s.getURLCtx(context.Background(), "INVALID-ID")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestGetURLV5(t *testing.T) {
	s := newScriptedMGStageScraper(t, true, map[string]mockHTTPResponse{
		"/search/cSearch.php?search_word=GANA-2850&type=top&page=1&list_cnt=120": {
			statusCode: http.StatusOK,
			body:       `<a href="https://www.mgstage.com/product/product_detail/GANA-2850/">Synthetic product</a>`,
		},
	})
	url, err := s.GetURL(context.Background(), "GANA-2850")
	require.NoError(t, err)
	assert.Equal(t, "https://www.mgstage.com/product/product_detail/GANA-2850/", url)
}

// TestResolveSearchQueryV5 tests ResolveSearchQuery
func TestResolveSearchQueryV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	result, ok := s.ResolveSearchQuery("GANA-2850")
	assert.True(t, ok)
	assert.Equal(t, "GANA-2850", result)

	_, ok = s.ResolveSearchQuery("")
	assert.False(t, ok)

	_, ok = s.ResolveSearchQuery("12345") // digits only should not match
	assert.False(t, ok)
}

// TestNormalizeIDForSearchV5 tests ID normalization
func TestNormalizeIDForSearchV5(t *testing.T) {
	assert.Equal(t, "gana2850", normalizeIDForSearch("GANA-2850"))
	assert.Equal(t, "siro5615", normalizeIDForSearch("SIRO-5615"))
	assert.Equal(t, "abc123", normalizeIDForSearch("ABC-123"))
}

// TestExtractIDFromURLV5 tests URL ID extraction
func TestExtractIDFromURLV5(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"product detail", "https://www.mgstage.com/product/product_detail/GANA-2850/", "GANA-2850"},
		{"no ID", "https://www.mgstage.com/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIDFromURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeMGStageIDTokenV5 tests ID token normalization
func TestNormalizeMGStageIDTokenV5(t *testing.T) {
	result, ok := normalizeMGStageIDToken("GANA-2850")
	assert.True(t, ok)
	assert.Equal(t, "GANA-2850", result)

	_, ok = normalizeMGStageIDToken("INVALID")
	assert.False(t, ok)
}

// TestSplitMGStageIDV5 tests ID splitting
func TestSplitMGStageIDV5(t *testing.T) {
	letter, number := splitMGStageID("GANA-2850")
	assert.Equal(t, "GANA", letter)
	assert.Equal(t, "2850", number)

	letter, number = splitMGStageID("INVALID")
	assert.Equal(t, "", letter)
	assert.Equal(t, "", number)
}

// TestParseHTMLV5 tests HTML parsing
func TestParseHTMLV5(t *testing.T) {
	html := buildMGStageDetailHTMLV5("GANA-2850", "Test Movie | MGS", "2024/01/15", "90", "MakerA", "LabelB", "SeriesC", "Action, Drama", "Actress A", "https://pics.mgs.com/cover.jpg")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseHTML(doc, "https://www.mgstage.com/product/product_detail/GANA-2850/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "GANA-2850", result.ID)
	assert.Equal(t, "MakerA", result.Maker)
	assert.Equal(t, "LabelB", result.Label)
	assert.Equal(t, "SeriesC", result.Series)
	assert.Equal(t, 90, result.Runtime)
}

// TestExtractTableValueV5 tests table value extraction
func TestExtractTableValueV5(t *testing.T) {
	html := `<table>
		<tr><th>品番：</th><td>GANA-2850</td></tr>
		<tr><th>配信開始日：</th><td>2024/01/15</td></tr>
		<tr><th>収録時間：</th><td>90 min</td></tr>
	</table>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	assert.Equal(t, "GANA-2850", extractTableValue(doc, "品番："))
	assert.Equal(t, "2024/01/15", extractTableValue(doc, "配信開始日："))
	assert.Equal(t, "90 min", extractTableValue(doc, "収録時間："))
	assert.Equal(t, "", extractTableValue(doc, "不明："))
}

// TestExtractGenresV5 tests genre extraction
func TestExtractGenresV5(t *testing.T) {
	html := `<html><body><table>
		<tr><th>ジャンル：</th><td><a href="/genre/1">Action</a><a href="/genre/2">Drama</a></td></tr>
	</table></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	genres := extractGenres(doc)
	assert.Equal(t, 2, len(genres))
}

// TestExtractActressesV5 tests actress extraction
func TestExtractActressesV5(t *testing.T) {
	html := `<html><body><table>
		<tr><th>出演：</th><td><a href="/actress/1">Actress A</a><a href="/actress/2">Actress B</a></td></tr>
	</table></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	actresses := extractActresses(doc)
	assert.Equal(t, 2, len(actresses))
}

// TestCanHandleURLV5 tests CanHandleURL
func TestCanHandleURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.mgstage.com/product/product_detail/GANA-2850/", true},
		{"https://example.com/product/product_detail/GANA-2850/", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// TestNewScraperV5 tests scraper creation
func TestNewScraperV5(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
}

// TestScrapeURLV5_NonMGStageURL tests ScrapeURL rejecting non-MGStage URLs
func TestScrapeURLV5_NonMGStageURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/product/product_detail/GANA-2850/")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestMGStageIDsMatchV5 tests ID matching
func TestMGStageIDsMatchV5(t *testing.T) {
	assert.True(t, mgstageIDsMatch("GANA-2850", "GANA-2850"))
	assert.True(t, mgstageIDsMatch("gana-2850", "GANA-2850"))
	assert.False(t, mgstageIDsMatch("GANA-2850", "SIRO-5615"))
}
