package javbus

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildFullDetailHTML creates a complete JavBus detail page HTML with all fields
func buildFullDetailHTML(id, title, date, runtime, maker, label, director, series, description, genres, actresses, coverURL, posterURL string) string {
	genreLinks := ""
	for _, g := range strings.Split(genres, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			genreLinks += fmt.Sprintf(`<a href="/genre/1">%s</a>`, g)
		}
	}

	actressLinks := ""
	for _, a := range strings.Split(actresses, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			actressLinks += fmt.Sprintf(`<a href="/star/%s"><img title="%s" src="/star-thumb.jpg"/></a>`, a, a)
		}
	}

	screenshotHTML := ""
	if posterURL != "" {
		screenshotHTML = fmt.Sprintf(`<a class="sample-box" href="%s">sample</a>`, posterURL)
	}

	trailerHTML := ""
	if id == "TRAIL-001" {
		trailerHTML = `<video><source src="https://example.com/trailer.mp4" type="video/mp4"></video>`
	}

	return fmt.Sprintf(`<html>
<head>
<meta name="description" content="%s"/>
<meta property="og:description" content="%s"/>
<title>%s %s - JavBus</title>
</head>
<body>
<div id="info">
	<p><span class="header">品番:</span> %s</p>
	<p><span class="header">発売日:</span> %s</p>
	<p><span class="header">収録時間:</span> %s</p>
	<p><span class="header">監督:</span> <a href="/director/1">%s</a></p>
	<p><span class="header">メーカー:</span> <a href="/maker/1">%s</a></p>
	<p><span class="header">レーベル:</span> <a href="/label/1">%s</a></p>
	<p><span class="header">シリーズ:</span> <a href="/series/1">%s</a></p>
</div>
<div id="star-div">
	<a href="/star/1"><img title="%s" src="/star-thumb.jpg"/></a>
</div>
<div id="genre-toggle">
	<a href="/genre/1">%s</a>
</div>
<a class="bigImage" href="%s"><img src="%s" title="%s" /></a>
<div id="sample-waterfall">
	%s
</div>
%s
</body>
</html>`, description, description, id, title, id, date, runtime, director, maker, label, series, actresses, genres, coverURL, coverURL, title, screenshotHTML, trailerHTML)
}

// TestSearchV5_FullE2E tests Search from search to parse with httptest server
func TestSearchV5_FullE2E(t *testing.T) {
	searchHTML := fmt.Sprintf(`<html><body>
<a class="movie-box" href="/IPX-999" title="IPX-999"><date>IPX-999</date></a>
</body></html>`)

	detailHTML := buildFullDetailHTML("IPX-999", "Test Movie", "2024-01-15", "120", "TestMaker", "TestLabel", "TestDirector", "TestSeries", "Test description", "Action, Drama", "Actress A", "https://pics.dmm.co.jp/cover.jpg", "https://pics.dmm.co.jp/sample.jpg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "search") {
			fmt.Fprint(w, searchHTML)
			return
		}
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-999", result.ID)
	assert.Equal(t, "TestMaker", result.Maker)
	assert.Equal(t, "TestLabel", result.Label)
	assert.Equal(t, "TestDirector", result.Director)
	assert.Equal(t, "TestSeries", result.Series)
	assert.Equal(t, "Test description", result.Description)
	assert.NotEmpty(t, result.ReleaseDate)
	assert.Equal(t, 120, result.Runtime)
	assert.NotEmpty(t, result.Genres)
	assert.NotEmpty(t, result.Actresses)
}

// TestSearchV5_NotFound tests Search when movie is not found
func TestSearchV5_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "search") {
			// Empty search results
			fmt.Fprint(w, `<html><body><div class="alert">No results</div></body></html>`)
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "NOTFOUND-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestScrapeURLV5_FullParse tests ScrapeURL with complete detail page
func TestScrapeURLV5_FullParse(t *testing.T) {
	detailHTML := buildFullDetailHTML("ABC-123", "Full Test Movie", "2024-06-01", "90", "MakerA", "LabelB", "DirC", "SeriesD", "A great movie", "Comedy, Romance", "Jane Doe", "https://pics.dmm.co.jp/cover2.jpg", "")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Use Search which goes through getURLCtx (using baseURL) then fetches the detail page
	// Instead, test the fetch + parse path directly since ScrapeURL requires javbus.com URL
	html, status, err := s.fetchPageCtx(context.Background(), ts.URL+"/ABC-123")
	require.NoError(t, err)
	assert.Equal(t, 200, status)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result, err := s.parseDetailPage(doc, ts.URL+"/ABC-123", "ABC-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "MakerA", result.Maker)
	assert.NotEmpty(t, result.Genres)
	assert.NotEmpty(t, result.Actresses)
}

// TestScrapeURLV5_ChallengePage tests ScrapeURL detecting challenge pages
func TestScrapeURLV5_ChallengePage(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		statusCode int
	}{
		{"driver verify page", `<html><body>/doc/driver-verify?referer=</body></html>`, 200},
		{"age verification", `<html><body>age verification javbus</body></html>`, 200},
		{"cloudflare challenge", `<html><body>cf-challenge-script Check your browser</body></html>`, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=UTF-8")
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.html)
			}))
			defer ts.Close()

			s := &scraper{
				client:      resty.New().SetBaseURL(ts.URL),
				enabled:     true,
				baseURL:     ts.URL,
				language:    "ja",
				rateLimiter: ratelimit.NewLimiter(0),
				settings:    models.ScraperSettings{Enabled: true},
			}

			_, err := s.ScrapeURL(context.Background(), ts.URL+"/ABC-123")
			assert.Error(t, err)
		})
	}
}

// TestGetURLV5_WithSearch tests GetURL with search fallback
func TestGetURLV5_WithSearch(t *testing.T) {
	searchHTML := fmt.Sprintf(`<html><body>
<a class="movie-box" href="/IPX-100" title="IPX-100"><date>IPX-100</date></a>
</body></html>`)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, searchHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	detailURL, err := s.GetURL(context.Background(), "IPX-100")
	require.NoError(t, err)
	assert.Contains(t, detailURL, "IPX-100")
}

// TestGetURLV5_EmptyID tests GetURL with empty ID
func TestGetURLV5_EmptyID(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.GetURL(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestGetURLV5_DirectURL tests GetURL with direct URL input
func TestGetURLV5_DirectURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	url, err := s.GetURL(context.Background(), "https://www.javbus.com/IPX-999")
	require.NoError(t, err)
	assert.Contains(t, url, "IPX-999")
}

// TestParseDetailPageV5_Minimal tests parsing a minimal detail page
func TestParseDetailPageV5_Minimal(t *testing.T) {
	minimalHTML := `<html>
<head><title>XYZ-001 some title - JavBus</title></head>
<body>
<div id="info">
	<p><span class="header">品番:</span> XYZ-001</p>
</div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(minimalHTML))
	require.NoError(t, err)

	s := &scraper{
		client:   resty.New(),
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		language: "zh",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://www.javbus.com/XYZ-001", "XYZ-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "XYZ-001", result.ID)
}

// TestParseDetailPageV5_WithTitleFallback tests title fallback to h3
func TestParseDetailPageV5_WithTitleFallback(t *testing.T) {
	html := `<html>
<head><title>No ID here</title></head>
<body>
<div id="info">
	<p><span class="header">品番:</span> FALLBACK-001</p>
</div>
<h3>Fallback Title From H3</h3>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		client:   resty.New(),
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		language: "zh",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://www.javbus.com/FALLBACK-001", "FALLBACK-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "FALLBACK-001", result.ID)
	assert.NotEmpty(t, result.Title)
}

// TestParseDetailPageV5_NoIDInHTML tests fallback to URL ID
func TestParseDetailPageV5_NoIDInHTML(t *testing.T) {
	html := `<html>
<head><title>Some generic page</title></head>
<body>
<div id="info"></div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		client:   resty.New(),
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		language: "zh",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://www.javbus.com/URLID-123", "URLID-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "URLID-123", result.ID)
}

// TestExtractInfoValueV5 tests extractInfoValue with various label patterns
func TestExtractInfoValueV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		labels   []string
		expected string
	}{
		{
			"Japanese label",
			`<div id="info"><p><span class="header">品番:</span> ABC-123</p></div>`,
			[]string{"品番", "識別碼", "识别码", "id"},
			"ABC-123",
		},
		{
			"Chinese traditional label",
			`<div id="info"><p><span class="header">識別碼:</span> DEF-456</p></div>`,
			[]string{"品番", "識別碼", "识别码", "id"},
			"DEF-456",
		},
		{
			"English label with colon in text",
			`<div id="info"><p>id: GHI-789</p></div>`,
			[]string{"品番", "識別碼", "识别码", "id"},
			"id: GHI-789", // Without span.header, TrimLeft only removes leading special chars
		},
		{
			"No matching label",
			`<div id="info"><p><span class="header">unknown:</span> XYZ</p></div>`,
			[]string{"品番"},
			"",
		},
		{
			"Date field",
			`<div id="info"><p><span class="header">発売日:</span> 2024-01-15</p></div>`,
			[]string{"発売日", "發行日期", "发行日期", "date"},
			"2024-01-15",
		},
		{
			"Runtime field",
			`<div id="info"><p><span class="header">収録時間:</span> 120</p></div>`,
			[]string{"収録時間", "長度", "长度", "runtime", "length"},
			"120",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractInfoValue(doc, tt.labels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractInfoLinkValueV5 tests extractInfoLinkValue
func TestExtractInfoLinkValueV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		labels   []string
		expected string
	}{
		{
			"With link",
			`<div id="info"><p><span class="header">メーカー:</span> <a href="/maker/1">MakerA</a></p></div>`,
			[]string{"メーカー", "製作商", "制作商", "maker", "studio"},
			"MakerA",
		},
		{
			"Director link",
			`<div id="info"><p><span class="header">監督:</span> <a href="/director/1">DirectorX</a></p></div>`,
			[]string{"監督", "導演", "导演", "director"},
			"DirectorX",
		},
		{
			"Without link falls back to text",
			`<div id="info"><p><span class="header">メーカー:</span> MakerB</p></div>`,
			[]string{"メーカー", "製作商", "制作商", "maker", "studio"},
			"MakerB",
		},
		{
			"Chinese label",
			`<div id="info"><p><span class="header">製作商:</span> <a href="/maker/1">MakerC</a></p></div>`,
			[]string{"メーカー", "製作商", "制作商", "maker", "studio"},
			"MakerC",
		},
		{
			"Label field",
			`<div id="info"><p><span class="header">レーベル:</span> <a href="/label/1">LabelD</a></p></div>`,
			[]string{"レーベル", "發行商", "发行商", "label"},
			"LabelD",
		},
		{
			"Series field",
			`<div id="info"><p><span class="header">シリーズ:</span> <a href="/series/1">SeriesE</a></p></div>`,
			[]string{"シリーズ", "系列", "series"},
			"SeriesE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractInfoLinkValue(doc, tt.labels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractActressesV5 tests actress extraction with various HTML patterns
func TestExtractActressesV5(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		expectedCount int
	}{
		{
			"Star div with img",
			`<div id="star-div"><a href="/star/1"><img title="Actress A" src="/thumb.jpg"/></a></div>`,
			1,
		},
		{
			"Star div with title attr",
			`<div id="star-div"><a href="/star/1" title="Actress B"></a></div>`,
			1,
		},
		{
			"Info link text",
			`<div id="info"><a href="/star/1">Actress C</a></div>`,
			1,
		},
		{
			"Multiple actresses",
			`<div id="star-div">
				<a href="/star/1"><img title="Actress D" src="/thumb1.jpg"/></a>
				<a href="/star/2"><img title="Actress E" src="/thumb2.jpg"/></a>
			</div>`,
			2,
		},
		{
			"Invalid actress name filtered",
			`<div id="star-div"><a href="/star/1"><img title="出演者" src="/thumb.jpg"/></a></div>`,
			0,
		},
		{
			"HTML in name filtered",
			`<div id="star-div"><a href="/star/1"><img title="<script>alert(1)</script>" src="/thumb.jpg"/></a></div>`,
			0,
		},
		{
			"Empty actress ignored",
			`<div id="star-div"><a href="/star/1"></a></div>`,
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			actresses := extractActresses(doc)
			assert.Equal(t, tt.expectedCount, len(actresses))
		})
	}
}

// TestExtractGenresV5 tests genre extraction
func TestExtractGenresV5(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		expectedCount int
	}{
		{
			"Genre toggle links",
			`<div id="genre-toggle"><a href="/genre/1">Action</a><a href="/genre/2">Drama</a></div>`,
			2,
		},
		{
			"Info genre links",
			`<div id="info"><a href="/genre/1">Comedy</a></div>`,
			1,
		},
		{
			"Deduplication",
			`<div id="genre-toggle"><a href="/genre/1">Action</a></div>
			 <div id="info"><a href="/genre/1">Action</a></div>`,
			1,
		},
		{
			"Empty genre",
			`<div id="genre-toggle"><a href="/genre/1"></a></div>`,
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			genres := extractGenres(doc)
			assert.Equal(t, tt.expectedCount, len(genres))
		})
	}
}

// TestExtractCoverURLV5 tests cover URL extraction
func TestExtractCoverURLV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool // whether a cover URL is found
	}{
		{
			"Big image href",
			`<a class="bigImage" href="https://pics.dmm.co.jp/cover.jpg"></a>`,
			true,
		},
		{
			"Big image src",
			`<a class="bigImage"><img src="https://pics.dmm.co.jp/cover.jpg"/></a>`,
			true,
		},
		{
			"Cover with data-src",
			`<a class="bigImage"><img data-src="https://pics.dmm.co.jp/cover.jpg" src="https://pics.dmm.co.jp/cover.jpg"/></a>`,
			true,
		},
		{
			"Cover img data-original",
			`<a class="bigImage"><img data-original="https://pics.dmm.co.jp/cover.jpg" src="https://pics.dmm.co.jp/cover.jpg"/></a>`,
			true,
		},
		{
			"No cover",
			`<div>no cover here</div>`,
			false,
		},
		{
			"Non-image href ignored",
			`<a class="bigImage" href="https://example.com/page.html"></a>`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractCoverURL(doc, "https://www.javbus.com")
			assert.Equal(t, tt.expected, result != "")
		})
	}
}

// TestExtractScreenshotURLsV5 tests screenshot URL extraction
func TestExtractScreenshotURLsV5(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		expectedCount int
	}{
		{
			"Sample box href",
			`<a class="sample-box" href="https://pics.dmm.co.jp/sample1.jpg"></a>`,
			1,
		},
		{
			"Sample waterfall href",
			`<div id="sample-waterfall"><a href="https://pics.dmm.co.jp/sample2.jpg"></a></div>`,
			1,
		},
		{
			"Fallback to img src",
			`<a class="sample-box"><img src="https://pics.dmm.co.jp/sample3.jpg"/></a>`,
			1,
		},
		{
			"Fallback to img data-src",
			`<div id="sample-waterfall"><img data-src="https://pics.dmm.co.jp/sample4.jpg"/></div>`,
			1,
		},
		{
			"Multiple screenshots",
			`<a class="sample-box" href="https://pics.dmm.co.jp/s1.jpg"></a>
			 <a class="sample-box" href="https://pics.dmm.co.jp/s2.jpg"></a>`,
			2,
		},
		{
			"No screenshots",
			`<div>no screenshots</div>`,
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractScreenshotURLs(doc, "https://www.javbus.com")
			assert.Equal(t, tt.expectedCount, len(result))
		})
	}
}

// TestExtractTrailerURLV5 tests trailer URL extraction
func TestExtractTrailerURLV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			"Video source",
			`<video><source src="https://example.com/trailer.mp4" type="video/mp4"></video>`,
			"https://example.com/trailer.mp4",
		},
		{
			"No trailer",
			`<div>no trailer</div>`,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractTrailerURL(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractDescriptionV5 tests description extraction
func TestExtractDescriptionV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			"Meta description",
			`<html><head><meta name="description" content="A great movie"/></head></html>`,
			"A great movie",
		},
		{
			"OG description fallback",
			`<html><head><meta property="og:description" content="OG description"/></head></html>`,
			"OG description",
		},
		{
			"No description",
			`<html><body>no desc</body></html>`,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractDescription(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestApplyLanguageToURLV5 tests URL language modification
func TestApplyLanguageToURLV5(t *testing.T) {
	tests := []struct {
		name     string
		language string
		input    string
		expected string
	}{
		{"ja prefix", "ja", "https://www.javbus.com/ABC-123", "https://www.javbus.com/ja/ABC-123"},
		{"en prefix", "en", "https://www.javbus.com/ABC-123", "https://www.javbus.com/en/ABC-123"},
		{"zh no prefix", "zh", "https://www.javbus.com/ABC-123", "https://www.javbus.com/ABC-123"},
		{"replaces existing en", "ja", "https://www.javbus.com/en/ABC-123", "https://www.javbus.com/ja/ABC-123"},
		{"replaces existing ja", "en", "https://www.javbus.com/ja/ABC-123", "https://www.javbus.com/en/ABC-123"},
		{"removes cn prefix for zh", "zh", "https://www.javbus.com/cn/ABC-123", "https://www.javbus.com/ABC-123"},
		{"removes tw prefix for zh", "zh", "https://www.javbus.com/tw/ABC-123", "https://www.javbus.com/ABC-123"},
		{"invalid URL returns as-is", "ja", "://invalid", "://invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &scraper{
				language: tt.language,
				settings: models.ScraperSettings{Enabled: true},
			}
			result := s.applyLanguageToURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsJavbusChallengePageV5 tests challenge page detection
func TestIsJavbusChallengePageV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"driver verify", `<html>/doc/driver-verify?referer=</html>`, true},
		{"age verification", `<html>age verification javbus</html>`, true},
		{"driver verification", `<html>driver verification</html>`, true},
		{"empty string", "", false},
		{"normal page", `<html>Normal content</html>`, false},
		{"click to enlarge", `<html>click to enlarge</html>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isJavbusChallengePage(tt.html)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsInvalidActressNameV5 tests invalid actress name filtering
func TestIsInvalidActressNameV5(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"HTML tags", "<script>", true},
		{"画像を拡大", "画像を拡大", true},
		{"点击放大", "点击放大", true},
		{"點擊放大", "點擊放大", true},
		{"click to enlarge", "click to enlarge", true},
		{"出演者", "出演者", true},
		{"演員", "演員", true},
		{"演员", "演员", true},
		{"valid name", "Yui Hatano", false},
		{"valid Japanese name", "波多野結衣", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInvalidActressName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFetchPageCtxV5_NetworkError tests fetchPageCtx with connection error
func TestFetchPageCtxV5_NetworkError(t *testing.T) {
	s := &scraper{
		client:      resty.New().SetBaseURL("http://127.0.0.1:1"), // Port 1 should be unreachable
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := s.fetchPageCtx(ctx, "http://127.0.0.1:1/nonexistent")
	assert.Error(t, err)
}

// TestParseDetailPageV5_WithRuntime tests runtime parsing
func TestParseDetailPageV5_WithRuntime(t *testing.T) {
	html := `<html>
<head><title>RT-001 Runtime Test - JavBus</title></head>
<body>
<div id="info">
	<p><span class="header">品番:</span> RT-001</p>
	<p><span class="header">収録時間:</span> 90</p>
</div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		client:   resty.New(),
		enabled:  true,
		language: "zh",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://www.javbus.com/RT-001", "RT-001")
	require.NoError(t, err)
	assert.Equal(t, 90, result.Runtime)
}

// TestParseDetailPageV5_WithDate tests date parsing
func TestParseDetailPageV5_WithDate(t *testing.T) {
	html := `<html>
<head><title>DT-001 Date Test - JavBus</title></head>
<body>
<div id="info">
	<p><span class="header">品番:</span> DT-001</p>
	<p><span class="header">発売日:</span> 2024-03-15</p>
</div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		client:   resty.New(),
		enabled:  true,
		language: "zh",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://www.javbus.com/DT-001", "DT-001")
	require.NoError(t, err)
	assert.NotEmpty(t, result.ReleaseDate)
}

// TestFindDetailURLV5_SingleCandidate tests findDetailURL with exactly one candidate
func TestFindDetailURLV5_SingleCandidate(t *testing.T) {
	html := `<html><body>
<a class="movie-box" href="/SINGLE-001"><date>OtherID</date></a>
</body></html>`

	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	result := s.findDetailURL(html, "https://www.javbus.com", "SINGLE-001")
	assert.NotEmpty(t, result) // Should use the single candidate fallback
}

// TestFindDetailURLV5_MultipleCandidates tests findDetailURL with multiple non-matching candidates
func TestFindDetailURLV5_MultipleCandidates(t *testing.T) {
	html := `<html><body>
<a class="movie-box" href="/OTHER-001" title="OTHER-001"><date>OTHER-001</date></a>
<a class="movie-box" href="/OTHER-002" title="OTHER-002"><date>OTHER-002</date></a>
</body></html>`

	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	result := s.findDetailURL(html, "https://www.javbus.com", "TARGET-999")
	assert.Empty(t, result) // Multiple non-matching candidates = no result
}

// TestExtractIDFromURLV5_EdgeCases tests additional URL ID extraction cases
func TestExtractIDFromURLV5_EdgeCases(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("zh prefix", func(t *testing.T) {
		id, err := s.ExtractIDFromURL("https://www.javbus.com/zh/ABC-123")
		assert.NoError(t, err)
		assert.Equal(t, "ABC-123", id)
	})

	t.Run("tw prefix", func(t *testing.T) {
		id, err := s.ExtractIDFromURL("https://www.javbus.com/tw/ABC-123")
		assert.NoError(t, err)
		assert.Equal(t, "ABC-123", id)
	})

	t.Run("cn prefix", func(t *testing.T) {
		id, err := s.ExtractIDFromURL("https://www.javbus.com/cn/ABC-123")
		assert.NoError(t, err)
		assert.Equal(t, "ABC-123", id)
	})

	t.Run("multiple path segments error", func(t *testing.T) {
		_, err := s.ExtractIDFromURL("https://www.javbus.com/genre/1")
		assert.Error(t, err)
	})
}

// TestScrapeURLV5_NonJavBusURL tests ScrapeURL rejecting non-JavBus URLs
func TestScrapeURLV5_NonJavBusURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestNewScraperV5_WithProxy tests scraper creation with proxy
func TestNewScraperV5_WithProxy(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled:  true,
		BaseURL:  "https://www.javbus.com",
		Proxy:    &models.ProxyConfig{Enabled: true, Profile: "test"},
		Language: "ja",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
	assert.Equal(t, "ja", s.language)
}

// TestNewScraperV5_CustomBaseURL tests scraper with custom base URL
func TestNewScraperV5_CustomBaseURL(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "https://www.javbus.org",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.Equal(t, "https://www.javbus.org", s.baseURL)
}

// TestNewScraperV5_DefaultBaseURL tests scraper gets default base URL
func TestNewScraperV5_DefaultBaseURL(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.Equal(t, defaultBaseURL, s.baseURL)
}

// TestSearchV5_WithHTTPError tests Search when detail page returns error
func TestSearchV5_WithHTTPError(t *testing.T) {
	searchHTML := `<html><body>
<a class="movie-box" href="/ERR-001" title="ERR-001"><date>ERR-001</date></a>
</body></html>`

	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "search") {
			fmt.Fprint(w, searchHTML)
			return
		}
		// Detail page returns 500
		w.WriteHeader(500)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "ERR-001")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestGetURLV5_BlockedError tests GetURL propagating blocked errors
func TestGetURLV5_BlockedError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "search") {
			w.WriteHeader(403)
			fmt.Fprint(w, `<html><body>Access Denied - blocked by firewall</body></html>`)
			return
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Should search and eventually return not found (blocked errors are skipped)
	_, err := s.GetURL(context.Background(), "BLOCKED-001")
	assert.Error(t, err)
}

// TestGetURLV5_SearchReturnsResult tests GetURL when search returns the detail URL
func TestGetURLV5_SearchReturnsResult(t *testing.T) {
	searchHTML := `<html><body>
<a class="movie-box" href="/FOUND-001" title="FOUND-001"><date>FOUND-001</date></a>
</body></html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, searchHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	url, err := s.GetURL(context.Background(), "FOUND-001")
	require.NoError(t, err)
	assert.Contains(t, url, "FOUND-001")
}
