package libredmm

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestPayloadToResultFinal_FullPayload(t *testing.T) {
	now := time.Now()
	payload := &moviePayload{
		NormalizedID:      "ABC-123",
		Title:             "Test Movie Title",
		Description:       "Test description",
		Date:              now.Format(time.RFC3339),
		Volume:            7200, // 120 min
		CoverImageURL:     "https://example.com/cover.jpg",
		ThumbnailImageURL: "https://example.com/thumb.jpg",
		Directors:         []string{"Director X"},
		Makers:            []string{"Maker X"},
		Labels:            []string{"Label X"},
		Genres:            []string{"Genre1", "Genre2"},
		Actresses: []actressPayload{
			{Name: "Actress A", ImageURL: "https://example.com/actress.jpg"},
		},
		SampleImageURLs: []string{"https://example.com/sample1.jpg"},
		Review:          4.5,
		URL:             "https://www.libredmm.com/movies/abc-123",
	}
	result := payloadToResult(payload, "https://www.libredmm.com/movies/abc-123.json", "ABC-123", &http.Client{})
	if result.ID != "ABC-123" {
		t.Errorf("expected ID ABC-123, got %s", result.ID)
	}
	if result.Title != "Test Movie Title" {
		t.Errorf("expected Test Movie Title, got %s", result.Title)
	}
	if result.Runtime != 120 {
		t.Errorf("expected runtime 120, got %d", result.Runtime)
	}
	if result.Director != "Director X" {
		t.Errorf("expected Director X, got %s", result.Director)
	}
	if result.Maker != "Maker X" {
		t.Errorf("expected Maker X, got %s", result.Maker)
	}
	if result.Label != "Label X" {
		t.Errorf("expected Label X, got %s", result.Label)
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
	if len(result.Actresses) != 1 {
		t.Errorf("expected 1 actress, got %d", len(result.Actresses))
	}
	if result.ReleaseDate == nil {
		t.Error("expected release date")
	}
	if result.Rating == nil || result.Rating.Score != 4.5 {
		t.Errorf("expected rating score 4.5, got %v", result.Rating)
	}
}

func TestPayloadToResultFinal_NilPayload(t *testing.T) {
	result := payloadToResult(nil, "https://www.libredmm.com/movies/abc-123.json", "ABC-123", &http.Client{})
	if result.ID != "abc-123" {
		t.Errorf("expected ID from URL abc-123, got %s", result.ID)
	}
}

func TestPayloadToResultFinal_EmptyPayload(t *testing.T) {
	payload := &moviePayload{}
	result := payloadToResult(payload, "https://www.libredmm.com/movies/abc-123.json", "ABC-123", &http.Client{})
	if result.ID != "abc-123" {
		t.Errorf("expected normalized ID abc-123, got %s", result.ID)
	}
}

func TestParseReleaseDateFinal(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{time.Now().Format(time.RFC3339Nano), true},
		{time.Now().Format(time.RFC3339), true},
		{"2024-01-15", true},
		{"2024-01-15 10:30:00", true},
		{"invalid", false},
		{"", false},
	}
	for _, tt := range tests {
		result := parseReleaseDate(tt.input)
		if (result != nil) != tt.want {
			t.Errorf("parseReleaseDate(%q) non-nil=%v, want %v", tt.input, result != nil, tt.want)
		}
	}
}

func TestParseActressesFinal(t *testing.T) {
	entries := []actressPayload{
		{Name: "波多野結衣", ImageURL: "https://example.com/a.jpg"},
		{Name: "Jane Doe", ImageURL: "https://example.com/b.jpg"},
		{Name: "", ImageURL: ""},
	}
	result := parseActresses(entries, "https://www.libredmm.com")
	if len(result) != 2 {
		t.Fatalf("expected 2 actresses (empty skipped), got %d", len(result))
	}
	if result[0].JapaneseName != "波多野結衣" {
		t.Errorf("expected Japanese name, got %s", result[0].JapaneseName)
	}
	if result[1].FirstName != "Jane" {
		t.Errorf("expected FirstName Jane, got %s", result[1].FirstName)
	}
}

func TestDedupeResolvedURLsFinal(t *testing.T) {
	urls := []string{
		"https://example.com/1.jpg",
		"https://example.com/2.jpg",
		"https://example.com/1.jpg",
	}
	result := dedupeResolvedURLs(urls, "https://www.libredmm.com")
	if len(result) != 2 {
		t.Fatalf("expected 2 deduplicated URLs, got %d", len(result))
	}
}

func TestDedupeStringsFinal(t *testing.T) {
	values := []string{"A", "B", "A", "", "B", "C"}
	result := dedupeStrings(values)
	if len(result) != 3 {
		t.Fatalf("expected 3 deduplicated values, got %d: %v", len(result), result)
	}
}

func TestFirstNonEmptyFinal(t *testing.T) {
	if v := firstNonEmpty([]string{"", "", "Hello", "World"}); v != "Hello" {
		t.Errorf("expected Hello, got %q", v)
	}
	if v := firstNonEmpty([]string{"", ""}); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

func TestBuildSearchURLFinal(t *testing.T) {
	url := buildSearchURL("https://www.libredmm.com", "ABC-123")
	if url != "https://www.libredmm.com/search?q=ABC-123&format=json" {
		t.Errorf("unexpected search URL: %s", url)
	}
}

func TestExtractIDFromURLFinal(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://www.libredmm.com/movies/ABC-123.json", "ABC-123"},
		{"https://www.libredmm.com/movies/ABC-123", "ABC-123"},
		{"https://www.libredmm.com/cid/ABC-123.json", "ABC-123"},
		{"https://www.libredmm.com/search?q=ABC-123", ""}, // search URL doesn't have path-based ID
		{"https://www.libredmm.com/other", ""},
	}
	for _, tt := range tests {
		got := extractIDFromURL(tt.url)
		if got != tt.want {
			t.Errorf("extractIDFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestStripANSICodesFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"\x1b[31mHello\x1b[0m", "Hello"},
		{"normal text", "normal text"},
		{"\x1bGarbage{\"key\":\"val\"}", "{\"key\":\"val\"}"},
		{"prefix\x1b[0m{\"key\":\"val\"}", "{\"key\":\"val\"}"},
	}
	for _, tt := range tests {
		got := stripANSICodes(tt.input)
		if got != tt.want {
			t.Errorf("stripANSICodes() = %q, want %q", got, tt.want)
		}
	}
}

func TestStripJSONSuffixFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://www.libredmm.com/movies/abc-123.json", "https://www.libredmm.com/movies/abc-123"},
		{"https://www.libredmm.com/movies/abc-123", "https://www.libredmm.com/movies/abc-123"},
	}
	for _, tt := range tests {
		got := stripJSONSuffix(tt.input)
		if got != tt.want {
			t.Errorf("stripJSONSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToHTTPSFinal(t *testing.T) {
	if toHTTPS("http://example.com/image.jpg") != "https://example.com/image.jpg" {
		t.Error("expected http to be converted to https")
	}
	if toHTTPS("https://example.com/image.jpg") != "https://example.com/image.jpg" {
		t.Error("expected https to remain https")
	}
}

func TestNormalizeMovieURLFinal(t *testing.T) {
	tests := []struct {
		raw  string
		base string
		ok   bool
	}{
		{"https://www.libredmm.com/movies/abc-123.json", "https://www.libredmm.com", true},
		{"https://www.libredmm.com/search?q=ABC-123", "https://www.libredmm.com", true},
		{"https://www.libredmm.com/cid/abc-123", "https://www.libredmm.com", true},
		{"https://other.com/movies/abc-123", "https://www.libredmm.com", false},
		{"not-a-url", "https://www.libredmm.com", false},
	}
	for _, tt := range tests {
		_, ok := normalizeMovieURL(tt.raw, tt.base)
		if ok != tt.ok {
			t.Errorf("normalizeMovieURL(%q) ok=%v, want %v", tt.raw, ok, tt.ok)
		}
	}
}

func TestMoviePayloadJSONParsingFinal(t *testing.T) {
	data := `{
		"normalized_id": "ABC-123",
		"title": "Test Title",
		"err": "",
		"actresses": [{"name": "Actress A", "image_url": "https://example.com/a.jpg"}],
		"cover_image_url": "https://example.com/cover.jpg",
		"date": "2024-01-15T00:00:00Z",
		"directors": ["Dir"],
		"genres": ["G1", "G2"],
		"makers": ["M1"],
		"labels": ["L1"],
		"volume": 7200,
		"review": 4.5,
		"sample_image_urls": ["https://example.com/s1.jpg"]
	}`
	var payload moviePayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if payload.NormalizedID != "ABC-123" {
		t.Errorf("expected ABC-123, got %s", payload.NormalizedID)
	}
	if payload.Volume != 7200 {
		t.Errorf("expected 7200, got %d", payload.Volume)
	}
}

func TestPayloadToResultFinal_SubtitleAsContentID(t *testing.T) {
	payload := &moviePayload{
		NormalizedID: "ABC-123",
		Subtitle:     "DEF-456",
		Title:        "Test",
	}
	result := payloadToResult(payload, "https://www.libredmm.com/movies/abc-123.json", "ABC-123", &http.Client{})
	if result.ContentID != "DEF-456" {
		t.Errorf("expected ContentID DEF-456 (from subtitle), got %s", result.ContentID)
	}
}
