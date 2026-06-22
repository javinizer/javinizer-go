package javlibrary

import (
	"testing"
)

func TestExtractTitleFinal(t *testing.T) {
	s := &scraper{}
	tests := []struct {
		html string
		id   string
		want string
	}{
		{"<title>ABC-123 My Movie Title - JAVLibrary</title>", "ABC-123", "My Movie Title"},
		{"<title>ABC-123 Only Title</title>", "ABC-123", "Only Title"},
		{"<title>No Info</title>", "XYZ-999", "No Info"},
		{"no title tag", "ABC-123", ""},
	}
	for _, tt := range tests {
		got := s.extractTitle(tt.html, tt.id)
		if got != tt.want {
			t.Errorf("extractTitle() = %q, want %q", got, tt.want)
		}
	}
}

func TestExtractCoverURLFinal(t *testing.T) {
	s := &scraper{}
	tests := []struct {
		html string
		want string
	}{
		{`id="video_jacket_img" src="https://pics.dmm.co.jp/cover.jpg"`, "https://pics.dmm.co.jp/cover.jpg"},
		{`id="video_jacket" href="//pics.dmm.co.jp/cover.jpg"`, "https://pics.dmm.co.jp/cover.jpg"},
		{"no cover info", ""},
	}
	for _, tt := range tests {
		got := s.extractCoverURL(tt.html)
		if got != tt.want {
			t.Errorf("extractCoverURL() = %q, want %q", got, tt.want)
		}
	}
}

func TestExtractReleaseDateFinal(t *testing.T) {
	s := &scraper{}
	tests := []struct {
		html string
		want bool // want non-nil
	}{
		{`id="video_date"><span class="text">2024-01-15</span>`, true},
		{`Release Date: 2024-03-20`, true},
		{"no date info", false},
	}
	for _, tt := range tests {
		got := s.extractReleaseDate(tt.html)
		if (got != nil) != tt.want {
			t.Errorf("extractReleaseDate() non-nil=%v, want %v", got != nil, tt.want)
		}
	}
}

func TestExtractRuntimeFinal(t *testing.T) {
	s := &scraper{}
	tests := []struct {
		html string
		want int
	}{
		{`id="video_length"><span class="text">120</span>`, 120},
		{"Length: 90 min", 90},
		{"no runtime info", 0},
	}
	for _, tt := range tests {
		got := s.extractRuntime(tt.html)
		if got != tt.want {
			t.Errorf("extractRuntime() = %d, want %d", got, tt.want)
		}
	}
}

func TestExtractFieldFinal(t *testing.T) {
	s := &scraper{}
	tests := []struct {
		html  string
		divID string
		want  string
	}{
		{`id="video_director"><a>Director Name</a></div>`, "video_director", "Director Name"},
		{`id="video_maker"><a>Maker X</a></div>`, "video_maker", "Maker X"},
		{`id="video_label"><a>Label Y</a></div>`, "video_label", "Label Y"},
		{"no matching div", "video_director", ""},
	}
	for _, tt := range tests {
		got := s.extractField(tt.html, tt.divID)
		if got != tt.want {
			t.Errorf("extractField(%q) = %q, want %q", tt.divID, got, tt.want)
		}
	}
}

func TestExtractGenresFinal(t *testing.T) {
	s := &scraper{}
	html := `<span class="genre"><a href="/genre/1">Comedy</a></span><span class="genre"><a href="/genre/2">Drama</a></span>`
	genres := s.extractGenres(html)
	if len(genres) != 2 {
		t.Fatalf("expected 2 genres, got %d", len(genres))
	}
	if genres[0] != "Comedy" || genres[1] != "Drama" {
		t.Errorf("unexpected genres: %v", genres)
	}
}

func TestExtractActressesFinal(t *testing.T) {
	s := &scraper{}
	html := `<span class="star"><a href="/star/1">Jane Doe</a></span><span class="star"><a href="/star/2">Mary Smith</a></span>`
	actresses := s.extractActresses(html)
	if len(actresses) != 2 {
		t.Fatalf("expected 2 actresses, got %d", len(actresses))
	}
	if actresses[0].FirstName != "Jane" {
		t.Errorf("expected Jane, got %s", actresses[0].FirstName)
	}
}

func TestExtractSeriesFinal(t *testing.T) {
	s := &scraper{}
	html := `id="video_series"><a href="/series/1">Series Name</a>`
	series := s.extractSeries(html)
	if series != "Series Name" {
		t.Errorf("expected Series Name, got %q", series)
	}
}

func TestExtractRatingFinal(t *testing.T) {
	s := &scraper{}
	html := `<div id="video_rating"><span class="num">4.5</span> / 5.0</div>`
	rating := s.extractRating(html, mustParseDoc(t, html))
	if rating == nil {
		t.Fatal("expected non-nil rating")
	}
	if rating.Score != 4.5 {
		t.Errorf("expected 4.5, got %f", rating.Score)
	}
}

func TestExtractRatingFinal_Normalized(t *testing.T) {
	s := &scraper{}
	// New implementation reads raw score from $rating JS var or span.num — no normalization
	html := `<div id="video_rating"><span class="num">8.0</span> / 10.0</div>`
	rating := s.extractRating(html, mustParseDoc(t, html))
	if rating == nil {
		t.Fatal("expected non-nil rating")
	}
	if rating.Score != 8.0 {
		t.Errorf("expected 8.0 (raw score, no normalization), got %f", rating.Score)
	}
}

func TestExtractTrailerURLFinal(t *testing.T) {
	s := &scraper{}
	html := `src="https://www.javlibrary.com/sample/movie.mp4"`
	url := s.extractTrailerURL(html)
	if url != "https://www.javlibrary.com/sample/movie.mp4" {
		t.Errorf("expected trailer URL, got %q", url)
	}
}

func TestExtractMovieURLFromHTMLFinal_CurrentFormat(t *testing.T) {
	s := &scraper{}
	html := `<div class="video" id="vid_javliat76u"><div class="id">ABC-123</div></div>`
	result := s.extractMovieURLFromHTML(html, "ABC-123")
	if result != "?v=javliat76u" {
		t.Errorf("expected ?v=javliat76u, got %q", result)
	}
}

func TestExtractMovieURLFromHTMLFinal_LegacyFormat(t *testing.T) {
	s := &scraper{}
	html := `<a href="/en/?v=javliat76u">ABC-123</a>`
	result := s.extractMovieURLFromHTML(html, "ABC-123")
	if result == "" {
		t.Error("expected non-empty URL from legacy format")
	}
}

func TestExtractDescriptionFinal_MetaTag(t *testing.T) {
	s := &scraper{}
	html := `<meta name="description" content="This is the movie description">`
	desc := s.extractDescription(html)
	if desc != "This is the movie description" {
		t.Errorf("expected description, got %q", desc)
	}
}

func TestIsValidLanguageFinal(t *testing.T) {
	tests := []struct {
		lang string
		want bool
	}{
		{"en", true},
		{"ja", true},
		{"cn", true},
		{"tw", true},
		{"fr", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isValidLanguage(tt.lang)
		if got != tt.want {
			t.Errorf("isValidLanguage(%q) = %v, want %v", tt.lang, got, tt.want)
		}
	}
}

func TestExtractScreenshotURLsFinal_FiltersPlaceholders(t *testing.T) {
	s := &scraper{}
	html := `<img src="https://pics.dmm.co.jp/sample1.jpg">
<img src="https://example.com/loading.jpg">
<img src="https://example.com/blank.jpg">
<img src="https://pics.dmm.co.jp/pl.jpg">`
	urls := s.extractScreenshotURLs(html)
	// pl.jpg (cover) and loading/blank should be filtered
	for _, u := range urls {
		if u == "https://example.com/loading.jpg" || u == "https://example.com/blank.jpg" || u == "https://pics.dmm.co.jp/pl.jpg" {
			t.Errorf("expected placeholder/cover to be filtered: %s", u)
		}
	}
}
