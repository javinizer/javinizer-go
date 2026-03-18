package fc2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func testConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Scrapers.FC2.Enabled = true
	cfg.Scrapers.FC2.RequestDelay = 0
	cfg.Scrapers.Proxy.Enabled = false
	return cfg
}

func TestScraperInterfaceCompliance(t *testing.T) {
	s := New(testConfig())
	var _ models.Scraper = s
	var _ models.ScraperQueryResolver = s
}

func TestNameAndEnabled(t *testing.T) {
	cfg := testConfig()
	s := New(cfg)

	assert.Equal(t, "fc2", s.Name())
	assert.True(t, s.IsEnabled())

	cfg.Scrapers.FC2.Enabled = false
	s = New(cfg)
	assert.False(t, s.IsEnabled())
}

func TestFetcherRateLimit(t *testing.T) {
	testCases := []struct {
		name        string
		delay       time.Duration
		lastRequest time.Time
	}{
		{
			name:        "no delay - should not wait",
			delay:       0,
			lastRequest: time.Now().Add(-time.Second),
		},
		{
			name:        "delay just passed - should wait",
			delay:       100 * time.Millisecond,
			lastRequest: time.Now().Add(-50 * time.Millisecond),
		},
		{
			name:        "delay fully passed - should not wait",
			delay:       100 * time.Millisecond,
			lastRequest: time.Now().Add(-200 * time.Millisecond),
		},
		{
			name:        "zero time last request - should not wait",
			delay:       100 * time.Millisecond,
			lastRequest: time.Time{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Scrapers.FC2.RequestDelay = int(tc.delay.Milliseconds())
			s := New(cfg)
			s.lastRequestTime.Store(tc.lastRequest)

			// Test that the function doesn't panic and completes
			s.waitForRateLimit()

			// Test updateLastRequestTime
			s.updateLastRequestTime()
			loadedTime, ok := s.lastRequestTime.Load().(time.Time)
			assert.True(t, ok)
			assert.False(t, loadedTime.IsZero())
		})
	}
}

func TestUpdateLastRequestTime(t *testing.T) {
	cfg := testConfig()
	s := New(cfg)

	firstTime := time.Now()
	s.updateLastRequestTime()

	loadedTime, ok := s.lastRequestTime.Load().(time.Time)
	assert.True(t, ok)
	assert.WithinDuration(t, firstTime, loadedTime, 100*time.Millisecond)

	time.Sleep(10 * time.Millisecond)
	s.updateLastRequestTime()

	loadedTime, ok = s.lastRequestTime.Load().(time.Time)
	assert.True(t, ok)
	assert.True(t, loadedTime.After(firstTime))
}

func TestResolveSearchQuery(t *testing.T) {
	s := New(testConfig())

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "canonical", input: "FC2-PPV-4847718", want: "FC2-PPV-4847718", ok: true},
		{name: "compact", input: "FC2PPV4847718", want: "FC2-PPV-4847718", ok: true},
		{name: "ppv short", input: "PPV-4847718", want: "FC2-PPV-4847718", ok: true},
		{name: "article url", input: "https://adult.contents.fc2.com/article/4847718/", want: "FC2-PPV-4847718", ok: true},
		{name: "plain article id", input: "4847718", want: "FC2-PPV-4847718", ok: true},
		{name: "invalid", input: "ABP-123", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := s.ResolveSearchQuery(tt.input)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetURL(t *testing.T) {
	s := New(testConfig())

	u, err := s.GetURL("PPV-4847718")
	assert.NoError(t, err)
	assert.Equal(t, "https://adult.contents.fc2.com/article/4847718/", u)

	u, err = s.GetURL("https://adult.contents.fc2.com/article/4847718/?lang=en")
	assert.NoError(t, err)
	assert.Equal(t, "https://adult.contents.fc2.com/article/4847718/", u)

	_, err = s.GetURL("ABP-123")
	assert.Error(t, err)
}

func TestParseDetailPage(t *testing.T) {
	html := `
<!doctype html>
<html>
<head>
<meta property="og:title" content="FC2-PPV-4847718 Sample Title">
<meta property="og:description" content="FC2-PPV-4847718 Sample description text">
<meta property="og:image" content="//storage.example.com/cover.png">
<meta property="og:video" content="https://adult.contents.fc2.com/embed/4847718/">
<script type="application/ld+json">{"@type":"Product","aggregateRating":{"ratingValue":4.9,"reviewCount":204}}</script>
</head>
<body>
  <div class="items_article_MainitemThumb">
    <span><p class="items_article_info">30:39</p></span>
  </div>
  <div class="items_article_headerInfo">
    <ul><li>by <a href="https://adult.contents.fc2.com/users/demo/">Demo Seller</a></li></ul>
  </div>
  <section class="items_article_TagArea">
    <a class="tag tagTag">素人</a>
    <a class="tag tagTag">中出し</a>
    <a class="tag tagTag">素人</a>
  </section>
  <div class="items_article_softDevice"><p>販売日 : 2026/02/13</p></div>
  <div class="items_article_softDevice"><p>商品ID : FC2 PPV 4847718</p></div>
  <ul class="items_article_SampleImagesArea">
    <li><a href="//contents-thumbnail2.fc2.com/w1280/sample1.png"></a></li>
    <li><a href="/sample2.png"></a></li>
  </ul>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, html, "https://adult.contents.fc2.com/article/4847718/", "4847718")
	if assert.NotNil(t, result) {
		assert.Equal(t, "fc2", result.Source)
		assert.Equal(t, "FC2-PPV-4847718", result.ID)
		assert.Equal(t, "FC2-PPV-4847718", result.ContentID)
		assert.Equal(t, "Sample Title", result.Title)
		assert.Equal(t, "Sample Title", result.OriginalTitle)
		assert.Equal(t, "Sample description text", result.Description)
		assert.Equal(t, 31, result.Runtime)
		assert.Equal(t, "Demo Seller", result.Maker)
		assert.Equal(t, "https://storage.example.com/cover.png", result.CoverURL)
		assert.Equal(t, "https://storage.example.com/cover.png", result.PosterURL)
		assert.Equal(t, "https://adult.contents.fc2.com/embed/4847718/", result.TrailerURL)
		assert.Equal(t, []string{"素人", "中出し"}, result.Genres)
		assert.Equal(t, []string{
			"https://contents-thumbnail2.fc2.com/w1280/sample1.png",
			"https://adult.contents.fc2.com/sample2.png",
		}, result.ScreenshotURL)

		if assert.NotNil(t, result.ReleaseDate) {
			expected := time.Date(2026, 2, 13, 0, 0, 0, 0, time.UTC)
			assert.True(t, result.ReleaseDate.Equal(expected))
		}
		if assert.NotNil(t, result.Rating) {
			assert.InDelta(t, 4.9, result.Rating.Score, 0.0001)
			assert.Equal(t, 204, result.Rating.Votes)
		}
	}
}

func TestParseRuntime(t *testing.T) {
	assert.Equal(t, 31, parseRuntime("30:39"))
	assert.Equal(t, 65, parseRuntime("1:04:31"))
	assert.Equal(t, 120, parseRuntime("120min"))
	assert.Equal(t, 0, parseRuntime(""))
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  float64
	}{
		{name: "float64", input: 1.5, want: 1.5},
		{name: "float32", input: float32(1.5), want: 1.5},
		{name: "int", input: 5, want: 5},
		{name: "int64", input: int64(10), want: 10},
		{name: "json.Number valid", input: json.Number("123.45"), want: 123.45},
		{name: "json.Number invalid", input: json.Number("not a number"), want: 0},
		{name: "string valid", input: "42.5", want: 42.5},
		{name: "string invalid", input: "not a number", want: 0},
		{name: "invalid type", input: []byte{1, 2, 3}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toFloat64(tt.input))
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  int
	}{
		{name: "int", input: 5, want: 5},
		{name: "int64", input: int64(10), want: 10},
		{name: "float64", input: 10.7, want: 10},
		{name: "float32", input: float32(10.7), want: 10},
		{name: "json.Number int", input: json.Number("123"), want: 123},
		{name: "json.Number float", input: json.Number("123.45"), want: 123},
		{name: "string valid", input: "42", want: 42},
		{name: "string invalid", input: "not a number", want: 0},
		{name: "invalid type", input: []byte{1, 2, 3}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toInt(tt.input))
		})
	}
}

func TestIsNotFoundPage(t *testing.T) {
	assert.True(t, isFC2NotFoundPage("申し訳ありません、お探しの商品が見つかりませんでした"))
	assert.False(t, isFC2NotFoundPage("<html><title>normal item page</title></html>"))
}

func TestExtractProductIDFromHTML(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "standard format",
			html: "<div class=\"items_article_softDevice\"><p>商品ID : FC2 PPV 4847718</p></div>",
			want: "4847718",
		},
		{
			name: "compact format",
			html: "<div class=\"items_article_softDevice\"><p>商品ID:FC2PPV4847718</p></div>",
			want: "4847718",
		},
		{
			name: "no product ID",
			html: "<div class=\"items_article_softDevice\"><p>販売日 : 2026/02/13</p></div>",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractProductIDFromHTML(tt.html))
		})
	}
}

func TestParseReleaseDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "valid date slash", input: "2026/02/13", want: "2026-02-13"},
		{name: "valid date dash", input: "2026-02-13", want: "2026-02-13"},
		{name: "valid date mixed", input: "2026/02-13", want: "2026-02-13"},
		{name: "empty string", input: "", want: ""},
		{name: "invalid format", input: "not a date", want: ""},
		{name: "invalid month", input: "2026/13/13", want: ""},
		{name: "invalid day", input: "2026/02/32", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseReleaseDate(tt.input)
			if tt.want == "" {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.want, result.Format("2006-01-02"))
			}
		})
	}
}

func TestExtractInfoValue(t *testing.T) {
	html := `<div class="items_article_softDevice"><p>販売日 : 2026/02/13</p></div>
<div class="items_article_softDevice"><p>商品ID : FC2 PPV 4847718</p></div>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	assert.Equal(t, "2026/02/13", extractInfoValue(doc, "販売日"))
	assert.Equal(t, "FC2 PPV 4847718", extractInfoValue(doc, "商品ID"))
	assert.Equal(t, "", extractInfoValue(doc, "不存在的标签"))
}

func TestCleanString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "normal string", input: "hello world", want: "hello world"},
		{name: "multiple spaces", input: "hello   world", want: "hello world"},
		{name: "newlines", input: "hello\nworld", want: "hello world"},
		{name: "tabs", input: "hello\tworld", want: "hello world"},
		{name: "leading/trailing", input: "  hello world  ", want: "hello world"},
		{name: "mixed whitespace", input: "  hello\n\tworld  ", want: "hello world"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanString(tt.input))
		})
	}
}

func TestCanonicalFC2ID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "standard", input: "4847718", want: "FC2-PPV-4847718"},
		{name: "with spaces", input: " 4847718 ", want: "FC2-PPV-4847718"},
		{name: "empty", input: "", want: "FC2-PPV-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, canonicalFC2ID(tt.input))
		})
	}
}

func TestStripFC2IDPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "with FC2 prefix", input: "FC2-PPV-4847718 Sample Title", want: "Sample Title"},
		{name: "with spaces", input: "FC2 PPV 4847718 Sample Title", want: "Sample Title"},
		{name: "with colon", input: "FC2-PPV-4847718: Sample Title", want: "Sample Title"},
		{name: "no prefix", input: "Sample Title", want: "Sample Title"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripFC2IDPrefix(tt.input))
		})
	}
}

func TestStripSiteSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "with FC2 suffix pipe", input: "Sample Title | FC2", want: "Sample Title"},
		{name: "with FC2 suffix fullwidth", input: "Sample Title ｜ FC2", want: "Sample Title"},
		{name: "no suffix", input: "Sample Title", want: "Sample Title"},
		{name: "with FC2 in middle", input: "FC2 | Sample Title", want: "FC2 | Sample Title"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripSiteSuffix(tt.input))
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	sourceURL := "https://adult.contents.fc2.com/article/4847718/"

	tests := []struct {
		name   string
		input  string
		source string
		want   string
	}{
		{name: "protocol relative", input: "//storage.example.com/cover.png", source: sourceURL, want: "https://storage.example.com/cover.png"},
		{name: "absolute http", input: "http://example.com/image.png", source: sourceURL, want: "http://example.com/image.png"},
		{name: "absolute https", input: "https://example.com/image.png", source: sourceURL, want: "https://example.com/image.png"},
		{name: "relative", input: "/sample.png", source: sourceURL, want: "https://adult.contents.fc2.com/sample.png"},
		{name: "empty", input: "", source: sourceURL, want: ""},
		{name: "invalid URL", input: "not a url", source: sourceURL, want: "https://adult.contents.fc2.com/article/4847718/not%20a%20url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeURL(tt.input, tt.source))
		})
	}
}

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "http", input: "http://example.com", want: true},
		{name: "https", input: "https://example.com", want: true},
		{name: "no scheme", input: "example.com", want: false},
		{name: "ftp", input: "ftp://example.com", want: false},
		{name: "empty", input: "", want: false},
		{name: "invalid", input: "not a url", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isHTTPURL(tt.input))
		})
	}
}

func TestExtractArticleID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "article URL", input: "https://adult.contents.fc2.com/article/4847718/", want: "4847718"},
		{name: "compact ID", input: "FC2-PPV-4847718", want: "4847718"},
		{name: "PPV short", input: "PPV-4847718", want: "4847718"},
		{name: "plain ID", input: "4847718", want: "4847718"},
		{name: "invalid", input: "ABP-123", want: ""},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractArticleID(tt.input))
		})
	}
}
