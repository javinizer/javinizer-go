package javbus

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestCanHandleURL(t *testing.T) {
	s := &Scraper{baseURL: "https://www.javbus.com"}

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"javbus.com", "https://www.javbus.com/ABC-123", true},
		{"javbus.org", "https://www.javbus.org/ABC-123", true},
		{"with language", "https://www.javbus.com/ja/ABC-123", true},
		{"other site", "https://www.example.com/ABC-123", false},
		{"malformed URL", "not-a-url", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.CanHandleURL(tt.url)
			if got != tt.expected {
				t.Errorf("CanHandleURL(%q) = %v, want %v", tt.url, got, tt.expected)
			}
		})
	}
}

func TestExtractIDFromURL(t *testing.T) {
	s := &Scraper{baseURL: "https://www.javbus.com"}

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"standard", "https://www.javbus.com/ABC-123", "ABC-123", false},
		{"with language en", "https://www.javbus.com/en/ABC-123", "ABC-123", false},
		{"with language ja", "https://www.javbus.com/ja/ABC-123", "ABC-123", false},
		{"javbus.org", "https://www.javbus.org/ABC-123", "ABC-123", false},
		{"empty path", "https://www.javbus.com/", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ExtractIDFromURL(%q) expected error, got nil", tt.url)
				}
			} else {
				if err != nil {
					t.Errorf("ExtractIDFromURL(%q) unexpected error: %v", tt.url, err)
				}
				if got != tt.expected {
					t.Errorf("ExtractIDFromURL(%q) = %q, want %q", tt.url, got, tt.expected)
				}
			}
		})
	}
}

func TestScraperInterfaceCompliance(t *testing.T) {
	s := &Scraper{baseURL: "https://www.javbus.com"}
	var _ models.Scraper = s
	var _ models.URLHandler = s
	var _ models.DirectURLScraper = s
}

func TestExtractCoverURL_PrefersBigImageHref(t *testing.T) {
	html := `
<html><body>
  <a class="bigImage" href="/pics/cover/abc_b.jpg">
    <img src="/pics/cover/abc_s.jpg" />
  </a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to build doc: %v", err)
	}

	got := extractCoverURL(doc, "https://www.javbus.com/ja/ABC-001")
	want := "https://www.javbus.com/pics/cover/abc_b.jpg"
	if got != want {
		t.Fatalf("extractCoverURL() = %q, want %q", got, want)
	}
}

func TestExtractScreenshotURLs_PrefersSampleBoxHref(t *testing.T) {
	html := `
<html><body>
  <div id="sample-waterfall">
    <a class="sample-box" href="https://pics.dmm.co.jp/digital/video/abc001/abc001jp-1.jpg">
      <img src="/pics/sample/abc_1.jpg" />
    </a>
    <a class="sample-box" href="https://pics.dmm.co.jp/digital/video/abc001/abc001jp-2.jpg">
      <img src="/pics/sample/abc_2.jpg" />
    </a>
  </div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to build doc: %v", err)
	}

	got := extractScreenshotURLs(doc, "https://www.javbus.com/ja/ABC-001")
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 screenshots from hrefs, got %d: %#v", len(got), got)
	}

	if got[0] != "https://pics.dmm.co.jp/digital/video/abc001/abc001jp-1.jpg" {
		t.Fatalf("expected first href screenshot, got %q", got[0])
	}
	if got[1] != "https://pics.dmm.co.jp/digital/video/abc001/abc001jp-2.jpg" {
		t.Fatalf("expected second href screenshot, got %q", got[1])
	}
}

func TestExtractScreenshotURLs_FallbackToPhotoFrameImages(t *testing.T) {
	html := `
<html><body>
  <div id="sample-waterfall">
    <div class="photo-frame"><img src="/pics/sample/abc_1.jpg" /></div>
    <div class="photo-frame"><img data-src="/pics/sample/abc_2.jpg" /></div>
  </div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to build doc: %v", err)
	}

	got := extractScreenshotURLs(doc, "https://www.javbus.com/ja/ABC-001")
	if len(got) != 2 {
		t.Fatalf("expected 2 fallback screenshots, got %d: %#v", len(got), got)
	}

	if got[0] != "https://www.javbus.com/pics/sample/abc_1.jpg" {
		t.Fatalf("expected fallback photo-frame image, got %q", got[0])
	}
	if got[1] != "https://www.javbus.com/pics/sample/abc_2.jpg" {
		t.Fatalf("expected fallback photo-frame image, got %q", got[1])
	}
}

func TestIsLikelyImageURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{url: "https://example.com/a.jpg", want: true},
		{url: "https://example.com/a.webp?x=1", want: true},
		{url: "https://example.com/a.php?id=1", want: false},
		{url: "javascript:void(0)", want: false},
		{url: "", want: false},
	}

	for _, tt := range tests {
		if got := isLikelyImageURL(tt.url); got != tt.want {
			t.Fatalf("isLikelyImageURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestNormalizeJavbusImageURL_DMMScreenshotJPFix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "adds jp prefix for dmm screenshot index url",
			in:   "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg",
			want: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name: "canonicalizes prefixed dmm content id with padded number",
			in:   "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880jp-1.jpg",
			want: "https://pics.dmm.co.jp/digital/video/118abp880/118abp880jp-1.jpg",
		},
		{
			name: "canonicalizes prefixed dmm cover path",
			in:   "https://pics.dmm.co.jp/mono/movie/adult/118abp00880/118abp00880pl.jpg",
			want: "https://pics.dmm.co.jp/mono/movie/adult/118abp880/118abp880pl.jpg",
		},
		{
			name: "keeps existing jp prefix",
			in:   "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
			want: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name: "keeps pl cover suffix",
			in:   "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
			want: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name: "normalizes awsimgsrc",
			in:   "https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-2.jpg?x=1",
			want: "https://pics.dmm.co.jp/video/ipx00535/ipx00535jp-2.jpg",
		},
		{
			name: "non-dmm untouched except query removal",
			in:   "https://www.javbus.com/pics/sample/77dp_1.jpg?x=1",
			want: "https://www.javbus.com/pics/sample/77dp_1.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeJavbusImageURL(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeJavbusImageURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCanonicalizeDMMPrefixedContentID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "118abp00880", want: "118abp880"},
		{in: "118abp00880jp-1", want: "118abp880jp-1"},
		{in: "118abp00880pl", want: "118abp880pl"},
		{in: "ipx00535", want: "ipx00535"},
		{in: "118abp880", want: "118abp880"},
	}

	for _, tt := range tests {
		got := canonicalizeDMMPrefixedContentID(tt.in)
		if got != tt.want {
			t.Fatalf("canonicalizeDMMPrefixedContentID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractActresses_SkipsMalformedPlaceholderNames(t *testing.T) {
	html := `
<html><body>
  <div id="star-div">
    <div id="avatar-waterfall">
      <a class="avatar-box" href="https://www.javbus.com/star/12no"><span>画像を拡大</span></a>
      <a class="avatar-box" href="https://www.javbus.com/star/12np"><span><img</span></a>
      <a class="avatar-box" href="https://www.javbus.com/star/12nq"><span><i</span></a>
    </div>
  </div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to build doc: %v", err)
	}

	got := extractActresses(doc)
	if len(got) != 0 {
		t.Fatalf("expected 0 actresses for malformed placeholders, got %d: %#v", len(got), got)
	}
}

func TestExtractActresses_ParsesValidStarNames(t *testing.T) {
	html := `
<html><body>
  <div id="star-div">
    <div id="avatar-waterfall">
      <a class="avatar-box" href="https://www.javbus.com/star/abc">
        <div class="photo-frame"><img src="https://img.example/star.jpg" title="河合あすな"></div>
      </a>
    </div>
  </div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to build doc: %v", err)
	}

	got := extractActresses(doc)
	if len(got) != 1 {
		t.Fatalf("expected 1 actress, got %d: %#v", len(got), got)
	}
	if got[0].JapaneseName != "河合あすな" {
		t.Fatalf("expected Japanese actress name, got %#v", got[0])
	}
	if got[0].ThumbURL != "https://img.example/star.jpg" {
		t.Fatalf("expected actress thumbnail url, got %#v", got[0])
	}
}

func TestIsJavbusChallengePage(t *testing.T) {
	tests := []struct {
		name string
		html string
		want bool
	}{
		{
			name: "driver verify canonical url",
			html: `<html><head><title>Age Verification JavBus - JavBus</title><link rel="canonical" href="https://www.javbus.com/doc/driver-verify?referer=https%3A%2F%2Fwww.javbus.com%2F"></head></html>`,
			want: true,
		},
		{
			name: "regular search page",
			html: `<html><head><title>IPX-535 - 搜尋 - 影片 - JavBus</title></head><body><a class="movie-box" href="/ABP-001"></a></body></html>`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isJavbusChallengePage(tt.html)
			if got != tt.want {
				t.Fatalf("isJavbusChallengePage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildCookieHeader_UsesBuiltInVerificationCookiesOnly(t *testing.T) {
	got := buildCookieHeader()
	want := "age=verified; dv=1; existmag=mag"
	if got != want {
		t.Fatalf("buildCookieHeader() = %q, want %q", got, want)
	}
	if strings.Contains(strings.ToLower(got), "phpsessid=") {
		t.Fatalf("buildCookieHeader() should not include PHPSESSID, got %q", got)
	}
}

func TestJavbusChallengeErrorType(t *testing.T) {
	err := models.NewScraperChallengeError("JavBus", "JavBus returned a driver verification challenge page")
	if scraperErr, ok := models.AsScraperError(err); !ok || scraperErr.Kind != models.ScraperErrorKindBlocked {
		t.Fatalf("expected blocked scraper error kind, got %#v (ok=%v)", scraperErr, ok)
	}
}
