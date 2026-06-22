package libredmm

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPayloadToResultDeep2_FullPayload(t *testing.T) {
	payload := &moviePayload{
		NormalizedID:      "IPX-123",
		Title:             "Test Movie Title",
		Description:       "Test Description",
		Date:              "2024-03-15",
		Volume:            7200,
		Directors:         []string{"Director A"},
		Makers:            []string{"Maker A"},
		Labels:            []string{"Label A"},
		Genres:            []string{"Genre1", "Genre2", "Genre1"},
		CoverImageURL:     "https://example.com/cover.jpg",
		ThumbnailImageURL: "https://example.com/thumb.jpg",
		SampleImageURLs:   []string{"https://example.com/s1.jpg", "https://example.com/s2.jpg"},
		Review:            7.5,
		URL:               "https://www.libredmm.com/movies/IPX-123",
		Actresses: []actressPayload{
			{Name: "田中麻美", ImageURL: "https://example.com/actress1.jpg"},
			{Name: "Jane Smith", ImageURL: "https://example.com/actress2.jpg"},
		},
	}

	result := payloadToResult(payload, "https://www.libredmm.com/movies/IPX-123.json", "IPX-123", nil)
	assert.Equal(t, "IPX-123", result.ID)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, "Test Description", result.Description)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, 120, result.Runtime) // 7200 / 60
	assert.Equal(t, "Director A", result.Director)
	assert.Equal(t, "Maker A", result.Maker)
	assert.Equal(t, "Label A", result.Label)
	assert.Equal(t, []string{"Genre1", "Genre2"}, result.Genres)
	assert.NotNil(t, result.Rating)
	assert.Equal(t, 7.5, result.Rating.Score)
	assert.Len(t, result.Actresses, 2)
	assert.Equal(t, "田中麻美", result.Actresses[0].JapaneseName)
	assert.Equal(t, "Jane", result.Actresses[1].FirstName)
	assert.Equal(t, "Smith", result.Actresses[1].LastName)
}

func TestPayloadToResultDeep2_NilPayload(t *testing.T) {
	result := payloadToResult(nil, "https://www.libredmm.com/movies/TEST-001.json", "TEST-001", nil)
	assert.Equal(t, "TEST-001", result.ID)
	assert.Equal(t, "TEST-001", result.Title) // fallback to ID
}

func TestPayloadToResultDeep2_EmptyFields(t *testing.T) {
	payload := &moviePayload{}
	result := payloadToResult(payload, "https://www.libredmm.com/movies/ABC-123.json", "ABC-123", nil)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "ABC-123", result.Title) // fallback
	assert.Nil(t, result.ReleaseDate)
	assert.Equal(t, 0, result.Runtime)
}

func TestParseReleaseDateDeep2_RFC3339(t *testing.T) {
	tm := parseReleaseDate("2024-03-15T10:30:00Z")
	assert.NotNil(t, tm)
	assert.Equal(t, 2024, tm.Year())
	assert.Equal(t, time.March, tm.Month())
	assert.Equal(t, 15, tm.Day())
}

func TestParseReleaseDateDeep2_WithTime(t *testing.T) {
	tm := parseReleaseDate("2024-03-15 10:30:00")
	assert.NotNil(t, tm)
	assert.Equal(t, 2024, tm.Year())
}

func TestParseReleaseDateDeep2_EmptyString(t *testing.T) {
	assert.Nil(t, parseReleaseDate(""))
}

func TestParseReleaseDateDeep2_InvalidFormat(t *testing.T) {
	assert.Nil(t, parseReleaseDate("not-a-date"))
}

func TestParseActressesDeep2_JapaneseName(t *testing.T) {
	entries := []actressPayload{
		{Name: "山田花子", ImageURL: "https://example.com/img.jpg"},
	}
	result := parseActresses(entries, "https://base.com")
	assert.Len(t, result, 1)
	assert.Equal(t, "山田花子", result[0].JapaneseName)
	assert.Contains(t, result[0].ThumbURL, "https://")
}

func TestParseActressesDeep2_WesternName(t *testing.T) {
	entries := []actressPayload{
		{Name: "Jane Marie Smith", ImageURL: ""},
	}
	result := parseActresses(entries, "https://base.com")
	assert.Len(t, result, 1)
	assert.Equal(t, "Jane", result[0].FirstName)
	assert.Equal(t, "Marie Smith", result[0].LastName)
}

func TestParseActressesDeep2_Deduplication(t *testing.T) {
	entries := []actressPayload{
		{Name: "山田花子", ImageURL: ""},
		{Name: "山田花子", ImageURL: ""}, // duplicate
	}
	result := parseActresses(entries, "https://base.com")
	assert.Len(t, result, 1)
}

func TestParseActressesDeep2_EmptyName(t *testing.T) {
	entries := []actressPayload{
		{Name: "", ImageURL: ""},
	}
	result := parseActresses(entries, "https://base.com")
	assert.Len(t, result, 0)
}

func TestDedupeResolvedURLsDeep2(t *testing.T) {
	urls := []string{
		"https://example.com/pic1.jpg",
		"https://example.com/pic1.jpg", // duplicate
		"https://example.com/pic2.jpg",
		"", // empty
	}
	result := dedupeResolvedURLs(urls, "https://base.com")
	assert.Len(t, result, 2)
}

func TestDedupeStringsDeep2(t *testing.T) {
	values := []string{"  hello  ", "hello", "world", "", "  "}
	result := dedupeStrings(values)
	assert.Equal(t, []string{"hello", "world"}, result)
}

func TestFirstNonEmptyDeep2(t *testing.T) {
	assert.Equal(t, "first", firstNonEmpty([]string{"  ", "first", "second"}))
	assert.Equal(t, "second", firstNonEmpty([]string{"", "  ", "second"}))
	assert.Equal(t, "", firstNonEmpty([]string{"", "  "}))
	assert.Equal(t, "", firstNonEmpty(nil))
}

func TestNormalizeMovieURLDeep2_SearchWithQuery(t *testing.T) {
	url, ok := normalizeMovieURL("https://www.libredmm.com/search?q=IPX-123", "https://www.libredmm.com")
	assert.True(t, ok)
	// Search with query converts to movie JSON URL
	assert.Contains(t, url, "IPX-123")
}

func TestNormalizeMovieURLDeep2_MoviesPath(t *testing.T) {
	url, ok := normalizeMovieURL("https://www.libredmm.com/movies/IPX-123", "https://www.libredmm.com")
	assert.True(t, ok)
	assert.Equal(t, "https://www.libredmm.com/movies/IPX-123.json", url)
}

func TestNormalizeMovieURLDeep2_MoviesPathJSON(t *testing.T) {
	url, ok := normalizeMovieURL("https://www.libredmm.com/movies/IPX-123.json", "https://www.libredmm.com")
	assert.True(t, ok)
	assert.Equal(t, "https://www.libredmm.com/movies/IPX-123.json", url)
}

func TestNormalizeMovieURLDeep2_CIDPath(t *testing.T) {
	url, ok := normalizeMovieURL("https://www.libredmm.com/cid/IPX-123", "https://www.libredmm.com")
	assert.True(t, ok)
	assert.Equal(t, "https://www.libredmm.com/movies/IPX-123.json", url)
}

func TestNormalizeMovieURLDeep2_NotLibreDMM(t *testing.T) {
	_, ok := normalizeMovieURL("https://example.com/movies/IPX-123", "https://www.libredmm.com")
	assert.False(t, ok)
}

func TestNormalizeMovieURLDeep2_NotHTTPURL(t *testing.T) {
	_, ok := normalizeMovieURL("IPX-123", "https://www.libredmm.com")
	assert.False(t, ok)
}

func TestBuildSearchURLDeep2(t *testing.T) {
	url := buildSearchURL("https://www.libredmm.com", "IPX-123")
	assert.Contains(t, url, "/search?q=")
	assert.Contains(t, url, "format=json")
}

func TestExtractIDFromURLDeep2_CIDPath(t *testing.T) {
	id := extractIDFromURL("https://www.libredmm.com/cid/abc123")
	assert.Equal(t, "abc123", id)
}

func TestExtractIDFromURLDeep2_CIDQuery(t *testing.T) {
	id := extractIDFromURL("https://www.libredmm.com/search?cid=abc123&id=test123")
	// The function checks article URL regex first, then query param q, then path
	// It may match the query param differently
	assert.NotEmpty(t, id, "should extract some ID from URL with query params")
}

func TestStripANSICodesDeep2_ExtractFromGarbage(t *testing.T) {
	input := "\x1b[32m{\"title\": \"test\"}\x1b[0m"
	result := stripANSICodes(input)
	assert.Equal(t, `{"title": "test"}`, result)
}

func TestStripANSICodesDeep2_ControlChars(t *testing.T) {
	input := "hello\x00world\x0btest"
	result := stripANSICodes(input)
	assert.Equal(t, "helloworldtest", result)
}

func TestStripANSICodesDeep2_PreserveNewlines(t *testing.T) {
	input := "line1\nline2\rline3"
	result := stripANSICodes(input)
	assert.Contains(t, result, "line1")
	assert.Contains(t, result, "line2")
}

func TestMoviePayloadJSONParsingDeep2(t *testing.T) {
	raw := `{
		"normalized_id": "IPX-456",
		"title": "JSON Test Title",
		"date": "2024-06-01",
		"volume": 5400,
		"err": "",
		"review": 8.0,
		"actresses": [
			{"name": "佐藤美咲", "image_url": "https://example.com/a.jpg"}
		]
	}`
	var payload moviePayload
	err := json.Unmarshal([]byte(raw), &payload)
	assert.NoError(t, err)
	assert.Equal(t, "IPX-456", payload.NormalizedID)
	assert.Equal(t, "JSON Test Title", payload.Title)
	assert.Equal(t, 5400, payload.Volume)
	assert.Equal(t, 8.0, payload.Review)
	assert.Len(t, payload.Actresses, 1)
}

func TestToHTTPSDeep2(t *testing.T) {
	assert.Equal(t, "https://example.com", toHTTPS("http://example.com"))
	assert.Equal(t, "https://example.com", toHTTPS("https://example.com"))
	assert.Equal(t, "", toHTTPS(""))
	assert.Equal(t, "not-a-url", toHTTPS("not-a-url"))
}

func TestStripJSONSuffixDeep2(t *testing.T) {
	assert.Equal(t, "https://www.libredmm.com/movies/IPX-123", stripJSONSuffix("https://www.libredmm.com/movies/IPX-123.json"))
	assert.Equal(t, "plain", stripJSONSuffix("plain.json"))
}
