package aventertainment

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestResolveSearchQuery(t *testing.T) {
	s := &Scraper{}

	tests := []struct {
		name      string
		input     string
		wantQuery string
		wantMatch bool
	}{
		{
			name:      "onepon format with underscore",
			input:     "1pon_020326_001",
			wantQuery: "1pon_020326_001",
			wantMatch: true,
		},
		{
			name:      "onepon format from filename",
			input:     "/media/unsorted/1pon-020326-001.mp4",
			wantQuery: "1pon_020326_001",
			wantMatch: true,
		},
		{
			name:      "carib format",
			input:     "carib_020326_001",
			wantQuery: "carib_020326_001",
			wantMatch: true,
		},
		{
			name:      "onepon date token in noisy filename",
			input:     "/media/unsorted/050419_844-1pondo/050419_844-1pon-1080p.mp4",
			wantQuery: "1pon_050419_844",
			wantMatch: true,
		},
		{
			name:      "bare onepon-style date token",
			input:     "050419_844",
			wantQuery: "1pon_050419_844",
			wantMatch: true,
		},
		{
			name:      "carib date token in noisy filename",
			input:     "/media/unsorted/021226_001-carib/021226_001-carib-720p.mp4",
			wantQuery: "carib_021226_001",
			wantMatch: true,
		},
		{
			name:      "standard id not handled by resolver hook",
			input:     "IPX-535",
			wantQuery: "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQuery, gotMatch := s.ResolveSearchQuery(tt.input)
			if gotMatch != tt.wantMatch {
				t.Fatalf("ResolveSearchQuery(%q) matched=%v, want %v", tt.input, gotMatch, tt.wantMatch)
			}
			if gotQuery != tt.wantQuery {
				t.Fatalf("ResolveSearchQuery(%q) query=%q, want %q", tt.input, gotQuery, tt.wantQuery)
			}
		})
	}
}

func TestExtractDetailLinksIncludesPPVDetail(t *testing.T) {
	html := `<div>
		<a href="/ppv/detail?pro=100126&amp;lang=1&amp;culture=en-US&amp;v=1">Match</a>
		<a href="/ppv/detail?pro=100126&amp;lang=1&amp;culture=en-US&amp;v=1">Duplicate</a>
		<a href="/ppv/index?lang=1&amp;culture=en-US&amp;v=1">Ignore</a>
	</div>`
	got := extractDetailLinks(html, "https://www.aventertainments.com")
	if len(got) != 1 {
		t.Fatalf("extractDetailLinks() len=%d, want 1", len(got))
	}
	if got[0] != "https://www.aventertainments.com/ppv/detail?pro=100126&lang=1&culture=en-US&v=1" {
		t.Fatalf("extractDetailLinks() got=%q", got[0])
	}
}

func TestExtractID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "DL1pon_020326_001", want: "1PON-020326-001"},
		{input: "DLGDRC-002", want: "GDRC-002"},
		{input: "carib_020326-001", want: "CARIB-020326-001"},
		{input: "https://www.aventertainments.com/ppv/detail?pro=100126", want: ""},
	}

	for _, tt := range tests {
		if got := extractID(tt.input); got != tt.want {
			t.Fatalf("extractID(%q)=%q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestApplyLanguage(t *testing.T) {
	sJa := &Scraper{language: "ja"}
	uJa := sJa.applyLanguage("https://www.aventertainments.com/ppv/detail?pro=100126")
	if !strings.Contains(uJa, "lang=2") || !strings.Contains(uJa, "culture=ja-JP") || !strings.Contains(uJa, "v=1") {
		t.Fatalf("applyLanguage(ja) returned %q", uJa)
	}

	sEn := &Scraper{language: "en"}
	uEn := sEn.applyLanguage("https://www.aventertainments.com/ppv/detail?pro=100126")
	if !strings.Contains(uEn, "lang=1") || !strings.Contains(uEn, "culture=en-US") || !strings.Contains(uEn, "v=1") {
		t.Fatalf("applyLanguage(en) returned %q", uEn)
	}
}

func TestParseDetailPage_UsesPosterSourceForCover(t *testing.T) {
	html := `<html><head><title>Test</title></head><body>
	<div id="PlayerCover"><img src="https://imgs02.aventertainments.com/vodimages/xlarge/1pon_020326_001.webp"></div>
	<a class="lightbox" href="https://imgs02.aventertainments.com/vodimages/gallery/large/1pon_020326_001.webp">gallery</a>
	<a class="lightbox" href="https://imgs02.aventertainments.com/vodimages/screenshot/large/1pon_020326_001/001.webp">s1</a>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse test html: %v", err)
	}

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail?pro=100126", "1pon_020326_001", "ja", false)
	want := "https://imgs02.aventertainments.com/vodimages/xlarge/1pon_020326_001.webp"

	if result.PosterURL != want {
		t.Fatalf("PosterURL=%q want %q", result.PosterURL, want)
	}
	if result.CoverURL != want {
		t.Fatalf("CoverURL=%q want %q", result.CoverURL, want)
	}
	if !result.ShouldCropPoster {
		t.Fatalf("ShouldCropPoster=%v want true", result.ShouldCropPoster)
	}
}

func TestExtractScreenshotURLs_WithBonusEnabled(t *testing.T) {
	html := `<html><body>
	<a class="lightbox" href="/vodimages/screenshot/large/1pon_020326_001/001.webp">s1</a>
	<a href="/vodimages/gallery/large/1pon_020326_001/001.webp">bonus1</a>
	<img src="/vodimages/gallery/large/1pon_020326_001/002.webp" />
	<a href="/vodimages/gallery/large/1pon_020326_001.webp">cover</a>
	<img src="/vodimages/large/carib_021226-001.webp" />
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse test html: %v", err)
	}

	withBonus := extractScreenshotURLs(doc, html, "https://www.aventertainments.com", true)
	withoutBonus := extractScreenshotURLs(doc, html, "https://www.aventertainments.com", false)

	if len(withoutBonus) != 1 {
		t.Fatalf("without bonus expected 1 screenshot, got %d (%v)", len(withoutBonus), withoutBonus)
	}
	if len(withBonus) != 3 {
		t.Fatalf("with bonus expected 3 screenshots, got %d (%v)", len(withBonus), withBonus)
	}

	containsBonus := func(url string) bool {
		for _, u := range withBonus {
			if strings.Contains(u, url) {
				return true
			}
		}
		return false
	}
	if !containsBonus("/vodimages/gallery/large/1pon_020326_001/001.webp") {
		t.Fatalf("missing expected bonus screenshot 001 in %v", withBonus)
	}
	if !containsBonus("/vodimages/gallery/large/1pon_020326_001/002.webp") {
		t.Fatalf("missing expected bonus screenshot 002 in %v", withBonus)
	}
}

func TestParseDetailPage_ExtractsStructuredJapaneseFields(t *testing.T) {
	html := `<html><body>
	<div class="section-title"><h3>結婚式NTR ~ 最低な元カレと ~ : 星野さやか</h3></div>
	<div class="product-description">
		<p>前半説明<span class='text-black'><a data-toggle='collapse' data-target='#category'>......すべて読む</a></span><div id='category' class='collapse'>後半説明</div></p>
	</div>
	<div class="product-info-block-rev">
		<div class="single-info"><span class="title">商品番号</span><span class="tag-title">DL1pon_020326_001</span></div>
		<div class="single-info"><span class="title">主演女優</span><span class="value"><a href='/ppv/idoldetail?id=1'>星野さやか</a></span></div>
		<div class="single-info"><span class="title">スタジオ</span><span class="value"><a href='/ppv/studio?studio=172'>一本道</a></span></div>
		<div class="single-info"><span class="title">カテゴリ</span><span class="value-category"><a href='/ppv/dept?cat=1'>A</a><a href='/ppv/dept?cat=2'>B</a></span></div>
		<div class="single-info"><span class="title">発売日</span><span class="value">02/03/2026 <span class="text-warning">(配信中)</span></span></div>
		<div class="single-info"><span class="title">収録時間</span><span class="value">0:56:34</span></div>
	</div>
	<div id="PlayerCover"><img src="https://imgs02.aventertainments.com/vodimages/xlarge/1pon_020326_001.webp"></div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse test html: %v", err)
	}

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail?pro=100126", "1pon_020326_001", "ja", false)

	if result.Title != "結婚式NTR ~ 最低な元カレと ~ : 星野さやか" {
		t.Fatalf("unexpected title: %q", result.Title)
	}
	if strings.Contains(result.Description, "すべて読む") {
		t.Fatalf("description should not include read-more marker: %q", result.Description)
	}
	if !strings.Contains(result.Description, "前半説明") || !strings.Contains(result.Description, "後半説明") {
		t.Fatalf("description missing expected combined content: %q", result.Description)
	}
	if result.ReleaseDate == nil {
		t.Fatalf("ReleaseDate should be parsed")
	}
	wantDate := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("unexpected release date: got %v want %v", result.ReleaseDate, wantDate)
	}
	if result.Runtime != 56 {
		t.Fatalf("unexpected runtime: got %d want 56", result.Runtime)
	}
	if result.Maker != "一本道" {
		t.Fatalf("unexpected maker: %q", result.Maker)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "A" || result.Genres[1] != "B" {
		t.Fatalf("unexpected genres: %#v", result.Genres)
	}
	if len(result.Actresses) != 1 || result.Actresses[0].JapaneseName != "星野さやか" {
		t.Fatalf("unexpected actresses: %#v", result.Actresses)
	}
}

func TestParseDetailPage_ExtractsStructuredEnglishFields(t *testing.T) {
	html := `<html><body>
	<div class="section-title"><h3>Wedding NTR ~ With My Worst Ex-Boyfriend ~ : Sayaka Hoshino</h3></div>
	<meta name="description" content="English fallback description">
	<div class="product-description"><p></p></div>
	<div class="product-info-block-rev">
		<div class="single-info"><span class="title">Item#</span><span class="tag-title">DL1pon_020326_001</span></div>
		<div class="single-info"><span class="title">Starring</span><span class="value"><a href='/ppv/idoldetail?id=1'>Sayaka Hoshino</a></span></div>
		<div class="single-info"><span class="title">Studio</span><span class="value"><a href='/ppv/studio?studio=172'>1pondo</a></span></div>
		<div class="single-info"><span class="title">Category</span><span class="value-category"><a href='/ppv/dept?cat=1'>All Sex</a><a href='/ppv/dept?cat=2'>Blowjob</a></span></div>
		<div class="single-info"><span class="title">Date</span><span class="value">02/03/2026 <span class="text-warning">(Available)</span></span></div>
		<div class="single-info"><span class="title">Play Time</span><span class="value">0:56:34</span></div>
	</div>
	<div id="PlayerCover"><img src="https://imgs02.aventertainments.com/vodimages/xlarge/1pon_020326_001.webp"></div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse test html: %v", err)
	}

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail?pro=100126", "1pon_020326_001", "en", false)

	if result.Title != "Wedding NTR ~ With My Worst Ex-Boyfriend ~ : Sayaka Hoshino" {
		t.Fatalf("unexpected title: %q", result.Title)
	}
	if result.Description != "English fallback description" {
		t.Fatalf("unexpected description: %q", result.Description)
	}
	if result.ReleaseDate == nil {
		t.Fatalf("ReleaseDate should be parsed")
	}
	wantDate := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("unexpected release date: got %v want %v", result.ReleaseDate, wantDate)
	}
	if result.Runtime != 56 {
		t.Fatalf("unexpected runtime: got %d want 56", result.Runtime)
	}
	if result.Maker != "1pondo" {
		t.Fatalf("unexpected maker: %q", result.Maker)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "All Sex" || result.Genres[1] != "Blowjob" {
		t.Fatalf("unexpected genres: %#v", result.Genres)
	}
	if len(result.Actresses) != 1 || result.Actresses[0].FirstName != "Sayaka" || result.Actresses[0].LastName != "Hoshino" {
		t.Fatalf("unexpected actresses: %#v", result.Actresses)
	}
}
