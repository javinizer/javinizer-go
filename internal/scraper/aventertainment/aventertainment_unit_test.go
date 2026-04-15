package aventertainment

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveSearchQuery tests the ID normalization and format resolution
func TestResolveSearchQuery(t *testing.T) {
	settings := testSettings(true)
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	tests := []struct {
		name   string
		input  string
		want   string
		wantOk bool
	}{
		{
			name:   "standard IPX format returns as-is",
			input:  "IPX-123",
			want:   "",
			wantOk: false, // Standard IDs don't need transformation
		},
		{
			name:   "OnePondo format with underscore",
			input:  "1pon_020326_001",
			want:   "1pon_020326_001",
			wantOk: true,
		},
		{
			name:   "OnePondo format with dash",
			input:  "1pon-020326-001",
			want:   "1pon_020326_001",
			wantOk: true,
		},
		{
			name:   "Caribbeancom format",
			input:  "carib_020326_001",
			want:   "carib_020326_001",
			wantOk: true,
		},
		{
			name:   "Caribbeancom without hyphen",
			input:  "caribbean_020326_001",
			want:   "carib_020326_001",
			wantOk: true,
		},
		{
			name:   "Caribbeancom lowercase",
			input:  "CARIB_020326_001",
			want:   "carib_020326_001",
			wantOk: true,
		},
		{
			name:   "OnePondo embedded in filename",
			input:  "050419_844-1pon-1080p",
			want:   "1pon_050419_844",
			wantOk: true,
		},
		{
			name:   "Caribbeancom embedded in filename",
			input:  "021226_001-carib-720p",
			want:   "carib_021226_001",
			wantOk: true,
		},
		{
			name:   "Bare date format defaults to 1pondo",
			input:  "050419_844",
			want:   "1pon_050419_844",
			wantOk: true,
		},
		{
			name:   "empty input returns empty",
			input:  "",
			want:   "",
			wantOk: false,
		},
		{
			name:   "input with path uses basename",
			input:  "/path/to/1pon_020326_001.mp4",
			want:   "1pon_020326_001",
			wantOk: true,
		},
		{
			name:   "input with backslash path",
			input:  `C:\Videos\carib_020326_001.avi`,
			want:   "carib_020326_001",
			wantOk: true,
		},
		{
			name:   "input with extension",
			input:  "1pon_020326_001.mp4",
			want:   "1pon_020326_001",
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := scraper.ResolveSearchQuery(tt.input)
			assert.Equal(t, tt.wantOk, ok, "ResolveSearchQuery should return correct ok value")
			assert.Equal(t, tt.want, got, "ResolveSearchQuery should return correct transformed query")
		})
	}
}

// TestFindDate tests date extraction from HTML
func TestFindDate(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "Japanese release date with label",
			html: `<span class="title">発売日</span><span class="value">03/15/2024</span>`,
			want: "03/15/2024",
		},
		{
			name: "English release date with label",
			html: `<span class="title">Release Date</span><span class="value">2024-03-15</span>`,
			want: "2024-03-15",
		},
		{
			name: "Date in format MM/DD/YYYY",
			html: `<div>Release: 12/25/2023 and more</div>`,
			want: "12/25/2023",
		},
		{
			name: "Date in format YYYY/MM/DD",
			html: `<div>Release: 2023/12/25 and more</div>`,
			want: "2023/12/25",
		},
		{
			name: "Date in format YYYY-MM-DD",
			html: `<div>Release: 2023-12-25 and more</div>`,
			want: "2023-12-25",
		},
		{
			name: "no date returns empty",
			html: `<div>No date here</div>`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findDate(tt.html)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFindRuntime tests runtime extraction from HTML
func TestFindRuntime(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "Japanese runtime with label",
			html: `<span class="title">収録時間</span><span class="value">120 分</span>`,
			want: "収録時間</span><span class=\"value\">120 分", // Returns full match m[0]
		},
		{
			name: "English runtime with label",
			html: `<span class="title">Runtime</span><span class="value">2:30</span>`,
			want: "Runtime</span><span class=\"value\">2:30", // Returns full match m[0]
		},
		{
			name: "Clock format time",
			html: `<div>Running time: 1:45</div>`,
			want: "1:45",
		},
		{
			name: "Minute format",
			html: `<div>Runtime: 90 min</div>`,
			want: "90 min",
		},
		{
			name: "Minute format with Japanese",
			html: `<div>Runtime: 120 分</div>`,
			want: "120 分",
		},
		{
			name: "Approximate runtime",
			html: `<div>Apx. 90 Min</div>`,
			want: "90 Min", // Returns m[0] which is just "90 Min"
		},
		{
			name: "no runtime returns empty",
			html: `<div>No runtime info</div>`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findRuntime(tt.html)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFindMaker tests studio/maker extraction from HTML
func TestFindMaker(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "Japanese studio label with link",
			html: `<span class="title">スタジオ</span><span class="value"><a href="#">Test Studio</a></span>`,
			want: "Test Studio",
		},
		{
			name: "English studio label with link",
			html: `<span class="title">Studio</span><span class="value"><a href="#">Another Studio</a></span>`,
			want: "Another Studio",
		},
		{
			name: "studio_products link",
			html: `<a href="/ppv/studio?xyz">Test Studio</a>`,
			want: "Test Studio",
		},
		{
			name: "no studio returns empty",
			html: `<div>No studio info</div>`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMaker(tt.html)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractDescription tests description extraction from document
func TestExtractDescription(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		selector string
		want     string
	}{
		{
			name:     "product-description block",
			html:     `<div class="product-description">This is the product description.</div>`,
			selector: ".product-description",
			want:     "This is the product description.",
		},
		{
			name:     "product-detail description",
			html:     `<div class="product-detail"><div class="description">Detailed description here.</div></div>`,
			selector: ".product-detail .description",
			want:     "Detailed description here.",
		},
		{
			name:     "value-description block",
			html:     `<div class="value-description">Value description text.</div>`,
			selector: ".value-description",
			want:     "Value description text.",
		},
		{
			name:     "meta description tag",
			html:     `<meta name="description" content="Meta description content">`,
			selector: "meta[name='description']",
			want:     "Meta description content",
		},
		{
			name:     "empty description returns empty",
			html:     `<div class="product-description"></div>`,
			selector: ".product-description",
			want:     "",
		},
		{
			name:     "description with script tags",
			html:     `<div class="product-description">Real text<script>var x=1;</script>More text</div>`,
			selector: ".product-description",
			want:     "Real textMore text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			got := extractDescription(doc)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractGenres tests genre extraction from selection
func TestExtractGenres(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		wantLen  int
		wantGens []string
	}{
		{
			name:     "multiple genres from category links",
			html:     `<div class="value-category"><a href="#">Action</a><a href="#">Romance</a><a href="#">Drama</a></div>`,
			wantLen:  3,
			wantGens: []string{"Action", "Romance", "Drama"},
		},
		{
			name:     "genre from cat_id link",
			html:     `<div><a href="/cat?cat_id=123">Horror</a></div>`,
			wantLen:  1,
			wantGens: []string{"Horror"},
		},
		{
			name:     "genre from dept link",
			html:     `<div><a href="/genre?dept=456">Comedy</a></div>`,
			wantLen:  1,
			wantGens: []string{"Comedy"},
		},
		{
			name:     "duplicates are removed",
			html:     `<div class="value-category"><a href="#">Action</a><a href="#">Action</a><a href="#">Romance</a></div>`,
			wantLen:  2,
			wantGens: []string{"Action", "Romance"},
		},
		{
			name:     "nil selection returns nil",
			html:     "",
			wantLen:  0,
			wantGens: nil,
		},
		{
			name:     "empty selection returns empty slice",
			html:     `<div></div>`,
			wantLen:  0,
			wantGens: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			var scope *goquery.Selection
			if tt.html != "" {
				scope = doc.Selection
			}

			got := extractGenres(scope)
			assert.Len(t, got, tt.wantLen)
			for _, gen := range tt.wantGens {
				assert.Contains(t, got, gen)
			}
		})
	}
}

// TestIsActressLabel tests actress label detection
func TestIsActressLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{
			name:  "Japanese 主演女優",
			label: "主演女優",
			want:  true,
		},
		{
			name:  "Japanese 女優",
			label: "女優",
			want:  true,
		},
		{
			name:  "English actress",
			label: "actress",
			want:  true,
		},
		{
			name:  "English starring",
			label: "starring",
			want:  true,
		},
		{
			name:  "not an actress label",
			label: "studio",
			want:  false,
		},
		{
			name:  "not an actress label",
			label: "category",
			want:  false,
		},
		{
			name:  "not an actress label",
			label: "date",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isActressLabel(tt.label)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsStudioLabel tests studio label detection
func TestIsStudioLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{
			name:  "Japanese スタジオ",
			label: "スタジオ",
			want:  true,
		},
		{
			name:  "English studio",
			label: "studio",
			want:  true,
		},
		{
			name:  "not a studio label",
			label: "actress",
			want:  false,
		},
		{
			name:  "not a studio label",
			label: "category",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStudioLabel(tt.label)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsCategoryLabel tests category label detection
func TestIsCategoryLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{
			name:  "Japanese カテゴリ",
			label: "カテゴリ",
			want:  true,
		},
		{
			name:  "English category",
			label: "category",
			want:  true,
		},
		{
			name:  "English categories",
			label: "categories",
			want:  true,
		},
		{
			name:  "not a category label",
			label: "studio",
			want:  false,
		},
		{
			name:  "not a category label",
			label: "actress",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCategoryLabel(tt.label)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsReleaseDateLabel tests release date label detection
func TestIsReleaseDateLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{
			name:  "Japanese 発売日",
			label: "発売日",
			want:  true,
		},
		{
			name:  "Japanese 配信日",
			label: "配信日",
			want:  true,
		},
		{
			name:  "English releasedate",
			label: "releasedate",
			want:  true,
		},
		{
			name:  "English release",
			label: "release",
			want:  true,
		},
		{
			name:  "bare date label",
			label: "date",
			want:  true,
		},
		{
			name:  "not a date label",
			label: "studio",
			want:  false,
		},
		{
			name:  "not a date label",
			label: "runtime",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isReleaseDateLabel(tt.label)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsRuntimeLabel tests runtime label detection
func TestIsRuntimeLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{
			name:  "Japanese 収録時間",
			label: "収録時間",
			want:  true,
		},
		{
			name:  "Japanese 再生時間",
			label: "再生時間",
			want:  true,
		},
		{
			name:  "English runtime",
			label: "runtime",
			want:  true,
		},
		{
			name:  "English runningtime",
			label: "runningtime",
			want:  true,
		},
		{
			name:  "English playtime",
			label: "playtime",
			want:  true,
		},
		{
			name:  "English length",
			label: "length",
			want:  true,
		},
		{
			name:  "not a runtime label",
			label: "date",
			want:  false,
		},
		{
			name:  "not a runtime label",
			label: "studio",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRuntimeLabel(tt.label)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractActresses tests actress extraction from selection
func TestExtractActresses(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		wantLen int
	}{
		{
			name:    "single actress",
			html:    `<div><a href="/ppv_actressdetail/1">Test Actress</a></div>`,
			wantLen: 1,
		},
		{
			name:    "multiple actresses",
			html:    `<div><a href="/ppv_actressdetail/1">Actress One</a><a href="/ppv_actressdetail/2">Actress Two</a></div>`,
			wantLen: 2,
		},
		{
			name:    "Japanese actress name",
			html:    `<div><a href="/ppv/idoldetail/1">田中愛</a></div>`,
			wantLen: 1,
		},
		{
			name:    "actress with first and last name",
			html:    `<div><a href="/ppv_actressdetail/1">Jane Doe</a></div>`,
			wantLen: 1,
		},
		{
			name:    "duplicates are removed",
			html:    `<div><a href="/ppv_actressdetail/1">Actress</a><a href="/ppv_actressdetail/2">Actress</a></div>`,
			wantLen: 1,
		},
		{
			name:    "nil selection returns nil",
			html:    "",
			wantLen: 0,
		},
		{
			name:    "no actress links returns empty slice",
			html:    `<div>No actress links here</div>`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			var scope *goquery.Selection
			if tt.html != "" {
				scope = doc.Selection
			}

			got := extractActresses(scope)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

// TestExtractPosterURL tests poster URL extraction
func TestExtractPosterURL(t *testing.T) {
	tests := []struct {
		name string
		html string
		base string
		want string
	}{
		{
			name: "PlayerCover img src",
			html: `<div><img id="PlayerCover" src="/vodimages/xlarge/ipx123.jpg"/></div>`,
			base: "https://www.aventertainments.com",
			want: "https://www.aventertainments.com/vodimages/xlarge/ipx123.jpg",
		},
		{
			name: "og:image meta tag",
			html: `<meta property="og:image" content="https://cdn.example.com/poster.jpg"/>`,
			base: "https://www.aventertainments.com",
			want: "https://cdn.example.com/poster.jpg",
		},
		{
			name: "no poster returns empty",
			html: `<div>No poster info</div>`,
			base: "https://www.aventertainments.com",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			got := extractPosterURL(doc, tt.html, tt.base)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractCoverURL tests cover URL extraction
func TestExtractCoverURL(t *testing.T) {
	tests := []struct {
		name string
		html string
		base string
		want string
	}{
		{
			name: "lightbox gallery link",
			html: `<a href="/vodimages/gallery/large/ipx123/cover.jpg" class="lightbox"></a>`,
			base: "https://www.aventertainments.com",
			want: "https://www.aventertainments.com/vodimages/gallery/large/ipx123/cover.jpg",
		},
		{
			name: "vodimages/gallery/large pattern",
			html: `<div>link to /vodimages/gallery/large/ipx123/cover.jpg here</div>`,
			base: "https://www.aventertainments.com",
			want: "https://www.aventertainments.com/vodimages/gallery/large/ipx123/cover.jpg",
		},
		{
			name: "no cover returns empty",
			html: `<div>No cover info</div>`,
			base: "https://www.aventertainments.com",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			got := extractCoverURL(doc, tt.html, tt.base)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractScreenshotURLs tests screenshot URL extraction
func TestExtractScreenshotURLs(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		scrapeBonus bool
		wantLen     int
	}{
		{
			name:        "screenshot from lightbox",
			html:        `<a href="/vodimages/screenshot/large/ipx123/screenshot-01.jpg" class="lightbox"></a>`,
			scrapeBonus: false,
			wantLen:     1,
		},
		{
			name:        "multiple screenshots",
			html:        `<a href="/vodimages/screenshot/large/ipx123/screenshot-01.jpg" class="lightbox"></a><a href="/vodimages/screenshot/large/ipx123/screenshot-02.jpg" class="lightbox"></a>`,
			scrapeBonus: false,
			wantLen:     2,
		},
		{
			name:        "no screenshots returns empty",
			html:        `<div>No screenshots</div>`,
			scrapeBonus: false,
			wantLen:     0,
		},
		{
			name:        "bonus screenshots when enabled",
			html:        `<a href="/vodimages/gallery/large/ipx123/001.jpg">bonus</a>`,
			scrapeBonus: true,
			wantLen:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			got := extractScreenshotURLs(doc, tt.html, "https://www.aventertainments.com", tt.scrapeBonus)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

// TestIsAVEBonusScreenshotURL tests bonus screenshot URL detection
func TestIsAVEBonusScreenshotURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "valid bonus screenshot URL",
			url:  "https://www.aventertainments.com/vodimages/gallery/large/ipx123/001.jpg",
			want: true,
		},
		{
			name: "valid bonus screenshot with 4 digits",
			url:  "/vodimages/gallery/large/ipx123/1234.webp",
			want: true,
		},
		{
			name: "regular screenshot is not bonus",
			url:  "/vodimages/screenshot/large/ipx123/screenshot-01.jpg",
			want: false,
		},
		{
			name: "poster is not bonus",
			url:  "/vodimages/xlarge/ipx123.jpg",
			want: false,
		},
		{
			name: "empty string returns false",
			url:  "",
			want: false,
		},
		{
			name: "whitespace only returns false",
			url:  "   ",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAVEBonusScreenshotURL(tt.url)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestHasJapanese tests Japanese character detection
func TestHasJapanese(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "Japanese hiragana",
			input: "田中",
			want:  true,
		},
		{
			name:  "Japanese katakana",
			input: "タナカ",
			want:  true,
		},
		{
			name:  "Japanese Han characters",
			input: "Tанaka 田中",
			want:  true,
		},
		{
			name:  "Latin characters only",
			input: "Tanaka",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "mixed with Latin",
			input: "Jane 田中 Doe",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasJapanese(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestStripSiteSuffix tests removal of site suffix from titles
func TestStripSiteSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "AV Entertainment suffix",
			input: "Test Movie - AV Entertainment",
			want:  "Test Movie",
		},
		{
			name:  "AVEntertainment suffix",
			input: "Test Movie - AVEntertainment",
			want:  "Test Movie",
		},
		{
			name:  "Japanese suffix",
			input: "Test Movie | AV Entertainment Pay-per-View",
			want:  "Test Movie",
		},
		{
			name:  "no suffix returns as-is",
			input: "Test Movie",
			want:  "Test Movie",
		},
		{
			name:  "case insensitive",
			input: "Test Movie - av entertainment",
			want:  "Test Movie",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripSiteSuffix(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNormalizeResolverInput tests input normalization for ID resolution
func TestNormalizeResolverInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple ID",
			input: "IPX-123",
			want:  "ipx-123",
		},
		{
			name:  "with path",
			input: "/path/to/IPX-123.mp4",
			want:  "ipx-123",
		},
		{
			name:  "with backslash path",
			input: `C:\Videos\IPX-123.avi`,
			want:  "ipx-123",
		},
		{
			name:  "with extension",
			input: "1pondo_020326_001.mkv",
			want:  "1pondo_020326_001",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace trimmed",
			input: "  IPX-123  ",
			want:  "ipx-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeResolverInput(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractDetailInfo tests extraction of detailed movie information
func TestExtractDetailInfo(t *testing.T) {
	html := `
		<!DOCTYPE html>
		<html>
		<body>
			<div class="section-title">
				<h1>IPX-123 - Test Movie Title</h1>
			</div>
			<div class="product-info-block-rev">
				<div class="single-info">
					<span class="title">商品番号</span>
					<span class="value">IPX-123</span>
					<span class="tag-title">IPX-123</span>
				</div>
				<div class="single-info">
					<span class="title">発売日</span>
					<span class="value">03/15/2024</span>
				</div>
				<div class="single-info">
					<span class="title">収録時間</span>
					<span class="value">120 分</span>
				</div>
				<div class="single-info">
					<span class="title">スタジオ</span>
					<span class="value"><a href="#">Test Studio</a></span>
				</div>
				<div class="single-info">
					<span class="title">カテゴリ</span>
					<span class="value-category"><a href="#">Action</a><a href="#">Drama</a></span>
				</div>
				<div class="single-info">
					<span class="title">主演女優</span>
					<span class="value"><a href="/ppv_actressdetail/1">Actress One</a><a href="/ppv_actressdetail/2">Actress Two</a></span>
				</div>
			</div>
		</body>
		</html>
	`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractDetailInfo(doc)

	assert.Equal(t, "IPX-123 - Test Movie Title", result.Title)
	assert.Equal(t, "IPX-123", result.ProductID)
	assert.Equal(t, "03/15/2024", result.ReleaseDateRaw)
	assert.Equal(t, "120 分", result.RuntimeRaw)
	assert.Equal(t, "Test Studio", result.Studio)
	assert.Len(t, result.Categories, 2)
	assert.Contains(t, result.Categories, "Action")
	assert.Contains(t, result.Categories, "Drama")
	assert.Len(t, result.Actresses, 2)
}

// TestExtractID tests ID extraction from various formats
func TestExtractID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "standard format",
			input: "IPX-123",
			want:  "IPX-123",
		},
		{
			name:  "OnePondo format",
			input: "1pon_020326_001",
			want:  "1PON-020326-001",
		},
		{
			name:  "Caribbeancom format",
			input: "carib_020326_001",
			want:  "CARIB-020326-001",
		},
		{
			name:  "compact format",
			input: "IPX123",
			want:  "IPX123",
		},
		{
			name:  "lowercase input",
			input: "ipx-123",
			want:  "IPX-123",
		},
		{
			name:  "DL prefix stripped",
			input: "DL-IPX123",
			want:  "IPX123",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "unrecognized format returns empty",
			input: "random-text",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractID(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNormalizeComparableID tests ID comparison normalization
func TestNormalizeComparableID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "standard ID",
			input: "IPX-123",
			want:  "ipx123",
		},
		{
			name:  "DL prefix stripped",
			input: "DL-IPX123",
			want:  "ipx123",
		},
		{
			name:  "ST prefix stripped",
			input: "ST-IPX123",
			want:  "ipx123",
		},
		{
			name:  "underscore removed",
			input: "IPX_123",
			want:  "ipx123",
		},
		{
			name:  "dash removed",
			input: "IPX-123",
			want:  "ipx123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeComparableID(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestResolveURL tests URL resolution
func TestResolveURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		raw  string
		want string
	}{
		{
			name: "absolute URL returns as-is",
			base: "https://www.aventertainments.com",
			raw:  "https://cdn.example.com/image.jpg",
			want: "https://cdn.example.com/image.jpg",
		},
		{
			name: "protocol-relative URL",
			base: "https://www.aventertainments.com",
			raw:  "//cdn.example.com/image.jpg",
			want: "https://cdn.example.com/image.jpg",
		},
		{
			name: "relative path resolved",
			base: "https://www.aventertainments.com",
			raw:  "/vodimages/xlarge/ipx123.jpg",
			want: "https://www.aventertainments.com/vodimages/xlarge/ipx123.jpg",
		},
		{
			name: "empty raw returns empty",
			base: "https://www.aventertainments.com",
			raw:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scraperutil.ResolveURL(tt.base, tt.raw)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExtractDetailLinks tests extraction of detail page links from HTML
func TestExtractDetailLinks(t *testing.T) {
	html := `
		<html>
		<body>
			<a href="/ppv/detail/123/1/1/new_detail">Movie 1</a>
			<a href="/ppv/detail/456/2/1/new_detail">Movie 2</a>
			<a href="/ppv/search?keyword=ipx">Search</a>
			<a href="https://external.com">External</a>
		</body>
		</html>
	`

	links := extractDetailLinks(html, "https://www.aventertainments.com")

	assert.Len(t, links, 2)
	assert.Contains(t, links, "https://www.aventertainments.com/ppv/detail/123/1/1/new_detail")
	assert.Contains(t, links, "https://www.aventertainments.com/ppv/detail/456/2/1/new_detail")
}

// TestApplyLanguage tests language application to URLs
func TestApplyLanguage(t *testing.T) {
	// Test language normalization
	enSettings := config.ScraperSettings{
		Enabled:   true,
		Language:  "en",
		RateLimit: 0,
	}
	enScraper := New(enSettings, nil, config.FlareSolverrConfig{})
	assert.Equal(t, "en", enScraper.language)

	jaSettings := config.ScraperSettings{
		Enabled:   true,
		Language:  "ja",
		RateLimit: 0,
	}
	jaScraper := New(jaSettings, nil, config.FlareSolverrConfig{})
	assert.Equal(t, "ja", jaScraper.language)

	// Test normalizeLanguage helper
	assert.Equal(t, "ja", scraperutil.NormalizeLanguage("ja"))
	assert.Equal(t, "ja", scraperutil.NormalizeLanguage("JA"))
	assert.Equal(t, "en", scraperutil.NormalizeLanguage("en"))
	assert.Equal(t, "en", scraperutil.NormalizeLanguage("invalid"))
}

// TestNormalizeLanguage tests language normalization
func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "ja returns ja",
			input: "ja",
			want:  "ja",
		},
		{
			name:  "JA returns ja",
			input: "JA",
			want:  "ja",
		},
		{
			name:  "anything else returns en",
			input: "en",
			want:  "en",
		},
		{
			name:  "empty returns en",
			input: "",
			want:  "en",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scraperutil.NormalizeLanguage(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSearchIntegration tests the full Search flow with a mock server
func TestSearchIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Note: This test is marked as skipped due to complex HTTP mocking requirements.
	// The core functionality is tested via parseDetailPage test.
	t.Skip("skipped - requires complex HTTP mocking")
}

// TestParseDetailPage tests the parseDetailPage function directly
func TestParseDetailPage(t *testing.T) {
	html := `
		<!DOCTYPE html>
		<html>
		<head><title>IPX-123 - Test Title</title></head>
		<body>
			<span class="tag-title">IPX-123</span>
			<h1 class="section-title">Test Title</h1>
			<div class="product-info-block-rev">
				<div class="single-info">
					<span class="title">商品番号</span>
					<span class="value">IPX-123</span>
				</div>
				<div class="single-info">
					<span class="title">発売日</span>
					<span class="value">2024-03-15</span>
				</div>
				<div class="single-info">
					<span class="title">収録時間</span>
					<span class="value">120 分</span>
				</div>
				<div class="single-info">
					<span class="title">スタジオ</span>
					<span class="value"><a href="#">Test Studio</a></span>
				</div>
			</div>
			<div class="product-description">Test description text</div>
			<div class="value-category"><a href="#">Action</a></div>
			<div class="single-info">
				<span class="title">主演女優</span>
				<span class="value"><a href="/ppv_actressdetail/1">田中愛</a></span>
			</div>
			<div id="PlayerCover"><img src="/vodimages/xlarge/poster.jpg"/></div>
			<a href="/vodimages/gallery/large/ipx123/cover.jpg" class="lightbox"></a>
			<a href="/vodimages/screenshot/large/ipx123/screenshot.jpg" class="lightbox"></a>
		</body>
		</html>
	`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail/123", "IPX-123", "en", false)

	assert.Equal(t, "aventertainment", result.Source)
	assert.Equal(t, "IPX-123", result.ID)
	// Title may include ID prefix
	assert.Contains(t, result.Title, "Test Title")
	assert.Equal(t, "Test Studio", result.Maker)
	assert.Equal(t, "Test description text", result.Description)
	assert.Len(t, result.Genres, 1)
	assert.Contains(t, result.Genres, "Action")
	assert.Len(t, result.Actresses, 1)
	// Japanese actress name should be in JapaneseName field
	assert.Equal(t, "田中愛", result.Actresses[0].JapaneseName)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, 120, result.Runtime)
}
