package caribbeancom

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestParseDetailPageDeep2_FullPage(t *testing.T) {
	html := `<html><body>
	<script>var Movie = {"movie_id":"020326-001"};</script>
	<h1 itemprop="name">Test Movie Title</h1>
	<p itemprop="description">Test description</p>
	<ul>
		<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">2024/03/15</span></li>
		<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">01:30:00</span></li>
		<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content"><a itemprop="actor"><span itemprop="name">田中麻美</span></a></span></li>
		<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content"><a>Tag1</a><a>Tag2</a></span></li>
	</ul>
	<meta property="og:image" content="https://www.caribbeancom.com/moviepages/020326-001/images/l_l.jpg">
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.caribbeancom.com/moviepages/020326-001/index.html", "020326-001", "ja")
	assert.Equal(t, "020326-001", result.ID)
	assert.Contains(t, result.Title, "Test Movie")
	assert.Equal(t, 90, result.Runtime)
	assert.NotNil(t, result.ReleaseDate)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "田中麻美", result.Actresses[0].JapaneseName)
	assert.Equal(t, []string{"Tag1", "Tag2"}, result.Genres)
	assert.Contains(t, result.CoverURL, "l_l.jpg")
}

func TestNormalizeMovieIDDeep2_ExtraCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"010122-01", "010122-001"},
		{"123456_789", "123456-789"},
		{"ABC-123", "abc-123"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, normalizeMovieID(tt.input), "input=%q", tt.input)
	}
}

func TestParseRuntimeDeep2_ISOFormat(t *testing.T) {
	assert.Equal(t, 90, parseRuntime("T1H30M0S"))
	assert.Equal(t, 60, parseRuntime("T1H0M0S"))
	assert.Equal(t, 30, parseRuntime("T0H30M0S"))
}

func TestParseRuntimeDeep2_ClockFormat(t *testing.T) {
	assert.Equal(t, 90, parseRuntime("1:30:00"))
	assert.Equal(t, 61, parseRuntime("1:00:30")) // rounds up
	assert.Equal(t, 45, parseRuntime("0:45:00"))
}

func TestParseRuntimeDeep2_MinuteFormat(t *testing.T) {
	assert.Equal(t, 90, parseRuntime("90 min"))
	assert.Equal(t, 120, parseRuntime("120 minutes"))
	assert.Equal(t, 60, parseRuntime("60分"))
}

func TestParseRuntimeDeep2_Empty(t *testing.T) {
	assert.Equal(t, 0, parseRuntime(""))
	assert.Equal(t, 0, parseRuntime("   "))
}

func TestParseReleaseDateDeep2_Formats(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		isNil    bool
	}{
		{"2024-03-15", 2024, false},
		{"03-15-2024", 2024, false},
		{"invalid", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		result := parseReleaseDate(tt.input)
		if tt.isNil {
			assert.Nil(t, result, "input=%q", tt.input)
		} else {
			assert.NotNil(t, result, "input=%q", tt.input)
			assert.Equal(t, tt.expected, result.Year(), "input=%q", tt.input)
		}
	}
}

func TestParseReleaseDateFromIDDeep2_ValidIDs(t *testing.T) {
	tests := []struct {
		id       string
		expected int
		isNil    bool
	}{
		{"020326-001", 2026, false}, // 02=month, 03=day, 26=year
		{"010122-001", 2022, false},
		{"invalid", 0, true},
	}
	for _, tt := range tests {
		result := parseReleaseDateFromID(tt.id)
		if tt.isNil {
			assert.Nil(t, result, "id=%q", tt.id)
		} else {
			assert.NotNil(t, result, "id=%q", tt.id)
			assert.Equal(t, tt.expected, result.Year(), "id=%q", tt.id)
		}
	}
}

func TestAtoiSafeDeep2(t *testing.T) {
	assert.Equal(t, 123, atoiSafe("123"))
	assert.Equal(t, 0, atoiSafe(""))
	assert.Equal(t, 0, atoiSafe("  "))
	assert.Equal(t, 0, atoiSafe("abc"))
	assert.Equal(t, 42, atoiSafe("  42  "))
}

func TestExtractMovieIDDeep2_FromJSON(t *testing.T) {
	html := `var Movie = {"movie_id":"123456-001"};`
	id := extractMovieID(html, "", "")
	assert.Equal(t, "123456-001", id)
}

func TestExtractMovieIDDeep2_FromURL(t *testing.T) {
	id := extractMovieID("", "https://www.caribbeancom.com/moviepages/123456-001/index.html", "")
	assert.Equal(t, "123456-001", id)
}

func TestExtractMovieIDDeep2_FromFallback(t *testing.T) {
	id := extractMovieID("", "", "123456-001")
	assert.Equal(t, "123456-001", id)
}

func TestExtractTrailerURLDeep2_JSON(t *testing.T) {
	html := `"sample_flash_url":"https://example.com/trailer.swf"`
	url := extractTrailerURL(html, "https://www.caribbeancom.com")
	assert.Contains(t, url, "example.com")
}

func TestExtractTrailerURLDeep2_Assignment(t *testing.T) {
	html := `sample_flash_url = 'https://example.com/trailer2.swf'`
	url := extractTrailerURL(html, "https://www.caribbeancom.com")
	assert.Contains(t, url, "example.com")
}

func TestExtractTrailerURLDeep2_EscapedChars(t *testing.T) {
	html := `"sample_flash_url":"https:\/\/example.com\/trailer.swf\u0026param=1"`
	url := extractTrailerURL(html, "https://www.caribbeancom.com")
	assert.Contains(t, url, "example.com")
	assert.Contains(t, url, "&param=1")
}

func TestIsMovieDetailPageDeep2_NullMovie(t *testing.T) {
	html := `var Movie = null;`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.False(t, isMovieDetailPage(doc, html))
}

func TestIsMovieDetailPageDeep2_ValidMovie(t *testing.T) {
	html := `<html><body><div id="moviepages">` + `"movie_id":"123456-001"` + `</div></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.True(t, isMovieDetailPage(doc, html))
}

func TestStripSiteSuffixDeep2(t *testing.T) {
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie | 無修正アダルト動画 カリビアンコム"))
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie | Caribbeancom"))
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie"))
}

func TestNormalizeLanguageDeep2(t *testing.T) {
	assert.Equal(t, "en", normalizeLanguage("en"))
	assert.Equal(t, "en", normalizeLanguage("EN"))
	assert.Equal(t, "ja", normalizeLanguage("ja"))
	assert.Equal(t, "ja", normalizeLanguage("JA"))
	assert.Equal(t, "ja", normalizeLanguage(""))   // default
	assert.Equal(t, "ja", normalizeLanguage("zh")) // non-english defaults to ja
}

func TestApplyLanguageDeep2_English(t *testing.T) {
	s := &scraper{language: "en", baseURL: "https://www.caribbeancom.com"}
	result := s.applyLanguage("https://www.caribbeancom.com/moviepages/123456-001/index.html")
	assert.Contains(t, result, "en.caribbeancom.com")
	assert.Contains(t, result, "/eng/")
}

func TestApplyLanguageDeep2_Japanese(t *testing.T) {
	s := &scraper{language: "ja", baseURL: "https://www.caribbeancom.com"}
	result := s.applyLanguage("https://en.caribbeancom.com/eng/moviepages/123456-001/index.html")
	assert.Contains(t, result, "www.caribbeancom.com")
	assert.NotContains(t, result, "/eng/")
}

func TestExtractCoverURLDeep2_OGImage(t *testing.T) {
	html := `<html><head><meta property="og:image" content="https://www.caribbeancom.com/moviepages/123456-001/images/l_l.jpg"></head></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	url := extractCoverURL(doc, html, "https://www.caribbeancom.com", "123456-001")
	assert.Contains(t, url, "l_l.jpg")
}

func TestExtractCoverURLDeep2_FallbackFromID(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	url := extractCoverURL(doc, html, "https://www.caribbeancom.com", "123456-001")
	assert.Contains(t, url, "123456-001")
	assert.Contains(t, url, "l_l.jpg")
}

func TestResolveSearchQueryDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	id, ok := s.ResolveSearchQuery("123456-001")
	assert.True(t, ok)
	assert.Equal(t, "123456-001", id)

	id, ok = s.ResolveSearchQuery("https://www.caribbeancom.com/moviepages/123456-001/")
	assert.True(t, ok)
	assert.Equal(t, "123456-001", id)

	_, ok = s.ResolveSearchQuery("invalid-id")
	assert.False(t, ok)
}

func TestExtractSpecValueDeep2(t *testing.T) {
	html := `<ul>
		<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">2024/03/15</span></li>
		<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">90 min</span></li>
	</ul>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	assert.Equal(t, "2024/03/15", extractSpecValue(doc, []string{"配信日"}))
	assert.Equal(t, "90 min", extractSpecValue(doc, []string{"再生時間"}))
	assert.Equal(t, "", extractSpecValue(doc, []string{"nonexistent"}))
}
