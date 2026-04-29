// Additional test cases for libredmm scraper coverage

package libredmm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseReleaseDate tests release date parsing
func TestParseReleaseDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		hasWant bool
	}{
		{
			name:    "RFC3339 format",
			input:   "2024-01-15T00:00:00Z",
			want:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			hasWant: true,
		},
		{
			name:    "RFC3339 with fractional seconds",
			input:   "2024-01-15T12:30:45.123456Z",
			want:    time.Date(2024, 1, 15, 12, 30, 45, 123456000, time.UTC),
			hasWant: true,
		},
		{
			name:    "Simple date format",
			input:   "2024-01-15",
			want:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			hasWant: true,
		},
		{
			name:    "DateTime format",
			input:   "2024-01-15 12:30:45",
			want:    time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC),
			hasWant: true,
		},
		{
			name:    "empty string",
			input:   "",
			hasWant: false,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			hasWant: false,
		},
		{
			name:    "Japanese format",
			input:   "2024/01/15",
			hasWant: false,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			hasWant: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseReleaseDate(tt.input)

			if tt.hasWant {
				require.NotNil(t, result)
				assert.Equal(t, tt.want.Unix(), result.Unix())
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

// TestParseActresses tests actress parsing
func TestParseActresses(t *testing.T) {
	tests := []struct {
		name    string
		entries []actressPayload
		base    string
		wantLen int
	}{
		{
			name: "single Western actress",
			entries: []actressPayload{
				{Name: "Alice Johnson", ImageURL: "https://example.com/alice.jpg"},
			},
			base:    "https://www.libredmm.com",
			wantLen: 1,
		},
		{
			name: "single Japanese actress",
			entries: []actressPayload{
				{Name: "山田花子", ImageURL: "https://example.com/hanako.jpg"},
			},
			base:    "https://www.libredmm.com",
			wantLen: 1,
		},
		{
			name: "multiple actresses",
			entries: []actressPayload{
				{Name: "Alice", ImageURL: "https://example.com/alice.jpg"},
				{Name: "Bob", ImageURL: "https://example.com/bob.jpg"},
			},
			base:    "https://www.libredmm.com",
			wantLen: 2,
		},
		{
			name: "duplicate actresses deduplicated",
			entries: []actressPayload{
				{Name: "Alice", ImageURL: "https://example.com/alice.jpg"},
				{Name: "Alice", ImageURL: "https://example.com/alice2.jpg"},
			},
			base:    "https://www.libredmm.com",
			wantLen: 1,
		},
		{
			name:    "empty entries",
			entries: []actressPayload{},
			base:    "https://www.libredmm.com",
			wantLen: 0,
		},
		{
			name: "empty name skipped",
			entries: []actressPayload{
				{Name: "", ImageURL: "https://example.com/empty.jpg"},
				{Name: "Valid", ImageURL: "https://example.com/valid.jpg"},
			},
			base:    "https://www.libredmm.com",
			wantLen: 1,
		},
		{
			name: "empty image URL",
			entries: []actressPayload{
				{Name: "Alice", ImageURL: ""},
			},
			base:    "https://www.libredmm.com",
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseActresses(tt.entries, tt.base)
			assert.Len(t, result, tt.wantLen)
		})
	}
}

// TestNormalizeMovieURL tests URL normalization
func TestNormalizeMovieURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		base  string
		want  string
		ok    bool
	}{
		{
			name:  "movie URL with JSON suffix",
			input: "https://www.libredmm.com/movies/IPX-535.json",
			base:  "https://www.libredmm.com",
			want:  "https://www.libredmm.com/movies/IPX-535.json",
			ok:    true,
		},
		{
			name:  "movie URL without JSON suffix",
			input: "https://www.libredmm.com/movies/IPX-535",
			base:  "https://www.libredmm.com",
			want:  "https://www.libredmm.com/movies/IPX-535.json",
			ok:    true,
		},
		{
			name:  "search URL with query",
			input: "https://www.libredmm.com/search?q=IPX535",
			base:  "https://www.libredmm.com",
			want:  "https://www.libredmm.com/search?q=IPX535&format=json",
			ok:    true,
		},
		{
			name:  "search URL with CID parameter",
			input: "https://www.libredmm.com/search?cid=IPX-535",
			base:  "https://www.libredmm.com",
			want:  "", // normalizeMovieURL only handles 'q' parameter, not 'cid'
			ok:    false,
		},
		{
			name:  "non-HTTP URL",
			input: "IPX535",
			base:  "https://www.libredmm.com",
			want:  "",
			ok:    false,
		},
		{
			name:  "non-libredmm URL",
			input: "https://www.mgstage.com/product/MIDE-123",
			base:  "https://www.libredmm.com",
			want:  "",
			ok:    false,
		},
		{
			name:  "invalid URL",
			input: "not-a-url",
			base:  "https://www.libredmm.com",
			want:  "",
			ok:    false,
		},
		{
			name:  "URL with special characters in path",
			input: "https://www.libredmm.com/movies/IPX%20535",
			base:  "https://www.libredmm.com",
			want:  "https://www.libredmm.com/movies/IPX%20535.json",
			ok:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := normalizeMovieURL(tt.input, tt.base)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestBuildSearchURL tests search URL building
func TestBuildSearchURL(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		query string
		want  string
	}{
		{
			name:  "standard query",
			base:  "https://www.libredmm.com",
			query: "IPX535",
			want:  "https://www.libredmm.com/search?q=IPX535&format=json",
		},
		{
			name:  "query with spaces",
			base:  "https://www.libredmm.com",
			query: "IPX 535",
			want:  "https://www.libredmm.com/search?q=IPX+535&format=json",
		},
		{
			name:  "base with trailing slash",
			base:  "https://www.libredmm.com/",
			query: "IPX535",
			want:  "https://www.libredmm.com/search?q=IPX535&format=json",
		},
		{
			name:  "empty query",
			base:  "https://www.libredmm.com",
			query: "",
			want:  "https://www.libredmm.com/search?q=&format=json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildSearchURL(tt.base, tt.query)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestExtractIDFromURL tests ID extraction from URLs
func TestExtractIDFromURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "movie URL singular (not supported)",
			url:  "https://www.libredmm.com/movie/ipx-535",
			want: "",
		},
		{
			name: "movie URL with JSON suffix singular (not supported)",
			url:  "https://www.libredmm.com/movie/ipx-535.json",
			want: "",
		},
		{
			name: "movies URL plural",
			url:  "https://www.libredmm.com/movies/IPX-535",
			want: "IPX-535",
		},
		{
			name: "search URL with q parameter (not supported, use cid)",
			url:  "https://www.libredmm.com/search?q=ipx-535",
			want: "",
		},
		{
			name: "search URL with cid parameter",
			url:  "https://www.libredmm.com/search?cid=ipx-535",
			want: "ipx-535",
		},
		{
			name: "URL with CID in path",
			url:  "https://www.libredmm.com/movie/?cid=ipx-535",
			want: "ipx-535",
		},
		{
			name: "empty URL",
			url:  "",
			want: "",
		},
		{
			name: "invalid URL",
			url:  "not-a-url",
			want: "",
		},
		{
			name: "URL with special characters",
			url:  "https://www.libredmm.com/movies/IPX%20535",
			want: "IPX 535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIDFromURL(tt.url)
			assert.Equal(t, tt.want, result)
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
			name: "absolute URL unchanged",
			base: "https://example.com/foo/bar",
			raw:  "https://other.com/x",
			want: "https://other.com/x",
		},
		{
			name: "protocol-relative URL",
			base: "https://example.com/foo/bar",
			raw:  "//cdn.example.com/image.jpg",
			want: "https://cdn.example.com/image.jpg",
		},
		{
			name: "root-relative URL",
			base: "https://example.com/foo/bar",
			raw:  "/baz",
			want: "https://example.com/baz",
		},
		{
			name: "relative path",
			base: "https://example.com/foo/bar",
			raw:  "qux",
			want: "https://example.com/foo/qux",
		},
		{
			name: "parent directory",
			base: "https://example.com/foo/bar",
			raw:  "../baz",
			want: "https://example.com/baz",
		},
		{
			name: "empty raw URL",
			base: "https://example.com/foo/bar",
			raw:  "",
			want: "",
		},
		{
			name: "whitespace only",
			base: "https://example.com/foo/bar",
			raw:  "   ",
			want: "",
		},
		{
			name: "URL with query params",
			base: "https://example.com/foo/bar",
			raw:  "image.jpg?foo=bar",
			// Shared ResolveURL correctly resolves relative URLs with query params
			want: "https://example.com/foo/image.jpg?foo=bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scraperutil.ResolveURL(tt.base, tt.raw)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestDedupeStrings tests string deduplication
func TestDedupeStrings(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "simple deduplication",
			input: []string{"a", "b", "a", "c"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "all duplicates",
			input: []string{"a", "a", "a"},
			want:  []string{"a"},
		},
		{
			name:  "empty input",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "empty strings filtered out",
			input: []string{"", "a", "", "b", ""},
			want:  []string{"a", "b"},
		},
		{
			name:  "whitespace strings filtered out",
			input: []string{"  ", "a", "   ", "b"},
			want:  []string{"a", "b"},
		},
		{
			name:  "case sensitive",
			input: []string{"A", "a", "B"},
			want:  []string{"A", "a", "B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dedupeStrings(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestFirstNonEmpty tests first non-empty string
func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name:  "first non-empty",
			input: []string{"", "", "hello", ""},
			want:  "hello",
		},
		{
			name:  "all empty",
			input: []string{"", "", ""},
			want:  "",
		},
		{
			name:  "empty input",
			input: []string{},
			want:  "",
		},
		{
			name:  "first is non-empty",
			input: []string{"first", "second", "third"},
			want:  "first",
		},
		{
			name:  "whitespace filtered",
			input: []string{"", "  ", "valid"},
			want:  "valid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := firstNonEmpty(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestCleanString tests string cleaning
func TestCleanString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "whitespace trimmed",
			input: "  hello world  ",
			want:  "hello world",
		},
		{
			name:  "non-breaking spaces replaced",
			input: "hello\u00a0world",
			want:  "hello world",
		},
		{
			name:  "multiple spaces collapsed",
			input: "hello    world",
			want:  "hello world",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only whitespace",
			input: "   \t  ",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scraperutil.CleanString(tt.input)
			assert.Equal(t, tt.want, result)
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
			name:  "kanji only",
			input: "山田",
			want:  true,
		},
		{
			name:  "hiragana only",
			input: "やまだ",
			want:  true,
		},
		{
			name:  "katakana only",
			input: "ヤマダ",
			want:  true,
		},
		{
			name:  "mixed Japanese and English",
			input: "山田 Alice",
			want:  true,
		},
		{
			name:  "English only",
			input: "Alice Johnson",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "numbers and symbols",
			input: "123-456",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scraperutil.HasJapanese(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestToHTTPS tests HTTP to HTTPS conversion
func TestToHTTPS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "http converted to https",
			input: "http://example.com/image.jpg",
			want:  "https://example.com/image.jpg",
		},
		{
			name:  "https unchanged",
			input: "https://example.com/image.jpg",
			want:  "https://example.com/image.jpg",
		},
		{
			name:  "already https with path",
			input: "https://cdn.example.com/foo/bar.jpg",
			want:  "https://cdn.example.com/foo/bar.jpg",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace trimmed and converted",
			input: "  http://example.com  ",
			want:  "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toHTTPS(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestIsHTTPURL tests HTTP URL detection
func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "http URL",
			input: "http://example.com",
			want:  true,
		},
		{
			name:  "https URL",
			input: "https://example.com",
			want:  true,
		},
		{
			name:  "ftp URL",
			input: "ftp://example.com",
			want:  false,
		},
		{
			name:  "no scheme",
			input: "example.com",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "invalid URL",
			input: "not-a-url",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scraperutil.IsHTTPURL(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestWaitForRateLimit tests rate limiting
// TestPayloadToResult tests result conversion from payload
func TestPayloadToResult(t *testing.T) {
	tests := []struct {
		name       string
		payload    *moviePayload
		sourceURL  string
		fallbackID string
		wantErr    bool
	}{
		{
			name: "full payload",
			payload: &moviePayload{
				NormalizedID:      "IPX-535",
				Subtitle:          "ipx535",
				Title:             "Test Movie",
				Description:       "Test description",
				Date:              "2024-01-15T00:00:00Z",
				Directors:         []string{"Director A"},
				Makers:            []string{"Maker A"},
				Labels:            []string{"Label A"},
				Genres:            []string{"Genre A"},
				Actresses:         []actressPayload{{Name: "Actress A", ImageURL: "https://example.com/a.jpg"}},
				CoverImageURL:     "https://example.com/cover.jpg",
				ThumbnailImageURL: "https://example.com/thumb.jpg",
				SampleImageURLs:   []string{"https://example.com/s1.jpg"},
				Review:            8.5,
				URL:               "https://www.libredmm.com/movies/IPX-535",
			},
			sourceURL:  "https://www.libredmm.com/movies/IPX-535.json",
			fallbackID: "IPX535",
			wantErr:    false,
		},
		{
			name:       "nil payload",
			payload:    nil,
			sourceURL:  "https://www.libredmm.com/search?q=IPX535",
			fallbackID: "IPX535",
			wantErr:    false,
		},
		{
			name:       "empty payload uses fallback",
			payload:    &moviePayload{},
			sourceURL:  "https://www.libredmm.com/movies/EMPTY",
			fallbackID: "FALLBACK-ID",
			wantErr:    false,
		},
		{
			name: "review zero not set",
			payload: &moviePayload{
				NormalizedID: "IPX-535",
				Subtitle:     "ipx535",
				Title:        "Test",
				Review:       0,
			},
			sourceURL:  "https://www.libredmm.com/movies/IPX-535.json",
			fallbackID: "IPX535",
			wantErr:    false,
		},
		{
			name: "volume zero runtime not set",
			payload: &moviePayload{
				NormalizedID: "IPX-535",
				Subtitle:     "ipx535",
				Title:        "Test",
				Volume:       0,
			},
			sourceURL:  "https://www.libredmm.com/movies/IPX-535.json",
			fallbackID: "IPX535",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := payloadToResult(tt.payload, tt.sourceURL, tt.fallbackID, nil)

			if tt.wantErr {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, "libredmm", result.Source)

				if tt.payload != nil {
					if tt.payload.NormalizedID != "" {
						assert.Equal(t, tt.payload.NormalizedID, result.ID)
					}
					if tt.payload.Title != "" {
						assert.Equal(t, tt.payload.Title, result.Title)
					}
					if tt.payload.Description != "" {
						assert.Equal(t, tt.payload.Description, result.Description)
					}
				}
			}
		})
	}
}

// TestResolveSearchQuery tests search query resolution
func TestResolveSearchQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{
			name:  "libredmm movie URL",
			input: "https://www.libredmm.com/movies/IPX-535",
			want:  "https://www.libredmm.com/movies/IPX-535.json",
			ok:    true,
		},
		{
			name:  "libredmm search URL",
			input: "https://www.libredmm.com/search?q=IPX535",
			want:  "https://www.libredmm.com/search?q=IPX535&format=json",
			ok:    true,
		},
		{
			name:  "non-libredmm URL",
			input: "https://www.mgstage.com/product/MIDE-123",
			want:  "",
			ok:    false,
		},
		{
			name:  "plain ID",
			input: "IPX535",
			want:  "",
			ok:    false,
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := testSettings("https://www.libredmm.com")
			scraper := New(settings, nil, config.FlareSolverrConfig{})

			result, ok := scraper.ResolveSearchQuery(tt.input)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestGetURLErrorPaths tests GetURL error scenarios
func TestGetURLErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "empty ID",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "plain ID returns search URL",
			input:   "IPX535",
			wantErr: false,
		},
		{
			name:    "movie ID with hyphen",
			input:   "IPX-535",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := testSettings("https://www.libredmm.com")
			scraper := New(settings, nil, config.FlareSolverrConfig{})

			url, err := scraper.GetURL(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, url)
			} else {
				assert.NoError(t, err)
				assert.Contains(t, url, "libredmm.com")
			}
		})
	}
}

// TestSearchProcessingTimeout tests timeout during processing
func TestSearchProcessingTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			// The Search function builds search URL with the ID passed in
			assert.Equal(t, "ZZZ-99999", r.URL.Query().Get("q"))
			assert.Equal(t, "json", r.URL.Query().Get("format"))
			http.Redirect(w, r, "/movies/ZZZ-99999.json", http.StatusFound)
		case "/movies/ZZZ-99999.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"err":"still processing"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	settings := testSettings(server.URL)

	scraper := New(settings, nil, config.FlareSolverrConfig{})
	scraper.maxPollAttempts = 2
	scraper.pollInterval = 1 * time.Millisecond

	result, err := scraper.Search(context.Background(), "ZZZ-99999")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "still processing")
}

// TestPayloadToResultCoverURLFallback tests cover URL fallback when poster probe fails
func TestPayloadToResult_CoverURLFallback(t *testing.T) {
	coverURL := "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880pl.jpg"
	payload := &moviePayload{
		CoverImageURL:     coverURL,
		ThumbnailImageURL: "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880ps.jpg",
		NormalizedID:      "ABP-880",
		Subtitle:          "118abp880",
		Title:             "Movie Title",
	}

	// Force poster probe to fail immediately
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network disabled")
		}),
	}

	result := payloadToResult(payload, "https://www.libredmm.com/movies/ABP-880.json", "ABP-880", client)
	require.NotNil(t, result)
	assert.Equal(t, coverURL, result.CoverURL)
	assert.Equal(t, coverURL, result.PosterURL)
}

// TestStripJSONSuffix tests JSON suffix stripping
func TestStripJSONSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "JSON suffix removed",
			input: "https://www.libredmm.com/movies/IPX-535.json",
			want:  "https://www.libredmm.com/movies/IPX-535",
		},
		{
			name:  "no JSON suffix unchanged",
			input: "https://www.libredmm.com/movies/IPX-535",
			want:  "https://www.libredmm.com/movies/IPX-535",
		},
		{
			name:  "JSON in filename not removed",
			input: "https://example.com/movies.JSON/file.json",
			want:  "https://example.com/movies.JSON/file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripJSONSuffix(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestNormalizeLibredmmScreenshotURL_DMMHosts tests DMM screenshot normalization
func TestNormalizeLibredmmScreenshotURL_DMMHosts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "DMM sample image gets jp marker",
			input: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg?foo=bar",
			want:  "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name:  "prefixed content ID normalized",
			input: "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880jp-2.jpg",
			want:  "https://pics.dmm.co.jp/digital/video/118abp880/118abp880jp-2.jpg",
		},
		{
			name:  "atwsimgsrc.dmm.co.jp domain redirected",
			input: "https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx-123/image.jpg",
			want:  "https://pics.dmm.co.jp/video/ipx-123/image.jpg",
		},
		{
			name:  "non-DMM URL unchanged",
			input: "https://example.com/image.jpg",
			want:  "https://example.com/image.jpg",
		},
		{
			name:  "query params removed",
			input: "https://pics.dmm.co.jp/digital/video/test/image.jpg?param=value",
			want:  "https://pics.dmm.co.jp/digital/video/test/image.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeLibredmmScreenshotURL(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestCanonicalizeDMMPrefixedContentID tests content ID canonicalization
func TestCanonicalizeDMMPrefixedContentID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "prefix with leading zeros removed - 3+ digit prefix preserved",
			input: "118abp00880jp-1.jpg",
			// Regex ^(\d{3,}[a-z]+)0+(\d+.*)$ matches 118abp (prefix), 00 (zeros), 880jp-1 (tail)
			// Since tail starts with 3 digits, no padding needed
			want: "118abp880jp-1.jpg",
		},
		{
			name:  "jp marker preserved",
			input: "ipx-535jp.jpg",
			want:  "ipx-535jp.jpg",
		},
		{
			name:  "pl suffix preserved",
			input: "abp-123pl.jpg",
			want:  "abp-123pl.jpg",
		},
		{
			name:  "ps suffix preserved",
			input: "abw-456ps.jpg",
			want:  "abw-456ps.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeDMMPrefixedContentID(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestHasJapanese_FullNames tests full Japanese name detection
func TestHasJapanese_FullNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "full name with kanji",
			input: "山田花子",
			want:  true,
		},
		{
			name:  "full name with hiragana",
			input: "やまだはなこ",
			want:  true,
		},
		{
			name:  "full name with katakana",
			input: "ヤマダハナコ",
			want:  true,
		},
		{
			name:  "full name with mixed scripts",
			input: "山田 ハナコ",
			want:  true,
		},
		{
			name:  "English name",
			input: "Alice Johnson",
			want:  false,
		},
		{
			name:  "mixed English and Japanese",
			input: "Alice 山田",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scraperutil.HasJapanese(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

// ========== Helper types and functions for testing ==========

// testSettings creates test scraper settings for LibreDMM
func testSettings(baseURL string) config.ScraperSettings {
	return config.ScraperSettings{
		Enabled:   true,
		RateLimit: 0,
		BaseURL:   baseURL,
	}
}

// roundTripperFunc is a test helper for HTTP round tripper
type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
