package nfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNFO_MinimalNFO(t *testing.T) {
	result, err := ParseNFO("../../testdata/nfo/minimal.nfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)

	movie := result.Movie
	assert.Equal(t, "Minimal Test Movie", movie.Title)
	assert.Equal(t, "IPX-001", movie.ID)
	assert.Equal(t, "IPX-001", movie.ContentID)
	assert.Equal(t, "nfo", movie.SourceName)
}

func TestParseNFO_CompleteNFO(t *testing.T) {
	result, err := ParseNFO("../../testdata/nfo/complete.nfo")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)

	movie := result.Movie

	// Basic fields
	assert.Equal(t, "Complete Test Movie", movie.Title)
	assert.Equal(t, "完全なテスト映画", movie.OriginalTitle)
	assert.Equal(t, "IPX-123", movie.ID)
	assert.Equal(t, "IPX-123", movie.ContentID)
	assert.Equal(t, "This is a complete test movie with all metadata fields populated for comprehensive testing.", movie.Description)

	// Time fields
	assert.Equal(t, 120, movie.Runtime)
	assert.Equal(t, 2024, movie.ReleaseYear)
	require.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, 2024, movie.ReleaseDate.Year())
	assert.Equal(t, time.Month(1), movie.ReleaseDate.Month())
	assert.Equal(t, 15, movie.ReleaseDate.Day())

	// Rating
	assert.Equal(t, 8.5, movie.RatingScore)
	assert.Equal(t, 1250, movie.RatingVotes)

	// Production info
	assert.Equal(t, "Test Director", movie.Director)
	assert.Equal(t, "Test Maker", movie.Maker)
	assert.Equal(t, "Test Label", movie.Label)
	assert.Equal(t, "Test Series", movie.Series)

	// Actresses
	require.Len(t, movie.Actresses, 2)
	assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
	assert.Equal(t, "Hatano", movie.Actresses[0].LastName)
	assert.Equal(t, "波多野結衣", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "https://example.com/actress1.jpg", movie.Actresses[0].ThumbURL)

	assert.Equal(t, "Ai", movie.Actresses[1].FirstName)
	assert.Equal(t, "Sayama", movie.Actresses[1].LastName)
	assert.Equal(t, "佐山愛", movie.Actresses[1].JapaneseName)

	// Genres
	require.Len(t, movie.Genres, 3)
	genreNames := []string{movie.Genres[0].Name, movie.Genres[1].Name, movie.Genres[2].Name}
	assert.Contains(t, genreNames, "Drama")
	assert.Contains(t, genreNames, "Romance")
	assert.Contains(t, genreNames, "Comedy")

	// Media URLs
	assert.Equal(t, "https://example.com/poster.jpg", movie.CoverURL)
	assert.Equal(t, "https://example.com/trailer.mp4", movie.TrailerURL)
	require.Len(t, movie.Screenshots, 3)
	assert.Contains(t, movie.Screenshots, "https://example.com/screenshot1.jpg")

	// Original filename
	assert.Equal(t, "IPX-123-original.mp4", movie.OriginalFileName)
}

func TestParseNFO_MalformedXML(t *testing.T) {
	result, err := ParseNFO("../../testdata/nfo/malformed.xml")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse NFO XML")
}

func TestParseNFO_FileNotFound(t *testing.T) {
	result, err := ParseNFO("../../testdata/nfo/nonexistent.nfo")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to read NFO file")
}

func TestParseNFO_EmptyFields(t *testing.T) {
	result, err := ParseNFO("../../testdata/nfo/empty_fields.nfo")
	require.NoError(t, err)
	require.NotNil(t, result)

	movie := result.Movie
	assert.Equal(t, "Movie With Empty Fields", movie.Title)
	assert.Equal(t, "IPX-456", movie.ID)

	// Empty fields should remain empty, not cause errors
	assert.Equal(t, "", movie.Description)
	assert.Equal(t, "", movie.Director)
	assert.Equal(t, "", movie.Maker)

	// Date should be parsed correctly
	require.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, 2024, movie.ReleaseDate.Year())
	assert.Equal(t, time.Month(6), movie.ReleaseDate.Month())
}

func TestNFOToMovie_RatingExtraction(t *testing.T) {
	nfo := &Movie{
		Title: "Test Movie",
		Ratings: Ratings{
			Rating: []Rating{
				{Name: "imdb", Max: 10, Default: false, Value: 7.5, Votes: 100},
				{Name: "themoviedb", Max: 10, Default: true, Value: 8.0, Votes: 200},
			},
		},
	}

	movie, warnings := NFOToMovie(nfo)
	assert.Empty(t, warnings)

	// Should use the default rating
	assert.Equal(t, 8.0, movie.RatingScore)
	assert.Equal(t, 200, movie.RatingVotes)
}

func TestNFOToMovie_RatingExtraction_NoDefault(t *testing.T) {
	nfo := &Movie{
		Title: "Test Movie",
		Ratings: Ratings{
			Rating: []Rating{
				{Name: "imdb", Max: 10, Default: false, Value: 7.5, Votes: 100},
				{Name: "themoviedb", Max: 10, Default: false, Value: 8.0, Votes: 200},
			},
		},
	}

	movie, warnings := NFOToMovie(nfo)
	assert.Empty(t, warnings)

	// Should use first rating when no default specified
	assert.Equal(t, 7.5, movie.RatingScore)
	assert.Equal(t, 100, movie.RatingVotes)
}

func TestSplitActorName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantFirst string
		wantLast  string
	}{
		{
			name:      "Two names",
			input:     "Yui Hatano",
			wantFirst: "Yui",
			wantLast:  "Hatano",
		},
		{
			name:      "Single name",
			input:     "Madonna",
			wantFirst: "Madonna",
			wantLast:  "",
		},
		{
			name:      "Three names",
			input:     "Mary Jane Watson",
			wantFirst: "Mary",
			wantLast:  "Jane Watson",
		},
		{
			name:      "Empty string",
			input:     "",
			wantFirst: "",
			wantLast:  "",
		},
		{
			name:      "Whitespace only",
			input:     "   ",
			wantFirst: "",
			wantLast:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, last := splitActorName(tt.input)
			assert.Equal(t, tt.wantFirst, first)
			assert.Equal(t, tt.wantLast, last)
		})
	}
}

func TestContainsJapanese(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"Hiragana", "ひらがな", true},
		{"Katakana", "カタカナ", true},
		{"Kanji", "漢字", true},
		{"Mixed", "波多野結衣", true},
		{"English", "Yui Hatano", false},
		{"Numbers", "12345", false},
		{"Empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsJapanese(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantY   int
		wantM   time.Month
		wantD   int
	}{
		{
			name:    "ISO 8601",
			input:   "2024-01-15",
			wantErr: false,
			wantY:   2024,
			wantM:   time.January,
			wantD:   15,
		},
		{
			name:    "Slash format",
			input:   "2024/06/30",
			wantErr: false,
			wantY:   2024,
			wantM:   time.June,
			wantD:   30,
		},
		{
			name:    "With timestamp",
			input:   "2024-03-15 10:30:45",
			wantErr: false,
			wantY:   2024,
			wantM:   time.March,
			wantD:   15,
		},
		{
			name:    "Empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "Invalid format",
			input:   "not a date",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDate(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantY, got.Year())
				assert.Equal(t, tt.wantM, got.Month())
				assert.Equal(t, tt.wantD, got.Day())
			}
		})
	}
}

// TestRoundTrip tests that we can write an NFO and then parse it back
func TestRoundTrip(t *testing.T) {
	// Create a test movie
	releaseDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	originalMovie := &models.Movie{
		ContentID:     "IPX-789",
		ID:            "IPX-789",
		Title:         "Round Trip Test",
		OriginalTitle: "ラウンドトリップテスト",
		Description:   "Testing round-trip conversion",
		Director:      "Test Director",
		Maker:         "Test Studio",
		Label:         "Test Label",
		Series:        "Test Series",
		ReleaseDate:   &releaseDate,
		ReleaseYear:   2024,
		Runtime:       90,
		RatingScore:   7.8,
		RatingVotes:   500,
		CoverURL:      "https://example.com/cover.jpg",
		TrailerURL:    "https://example.com/trailer.mp4",
		Screenshots:   []string{"https://example.com/shot1.jpg", "https://example.com/shot2.jpg"},
		Actresses: []models.Actress{
			{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
				ThumbURL:     "https://example.com/actress.jpg",
			},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		},
	}

	// Generate NFO
	generator := NewGenerator(DefaultConfig())
	generator.config.IncludeFanart = true
	generator.config.IncludeTrailer = true
	generator.config.AltNameRole = true // Include Japanese names in role field for round-trip
	nfoStruct := generator.MovieToNFO(originalMovie, "")

	// Write to temp file
	tmpDir := t.TempDir()
	nfoPath := filepath.Join(tmpDir, "roundtrip.nfo")
	err := generator.WriteNFO(nfoStruct, nfoPath)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(nfoPath)
	require.NoError(t, err)

	// Parse it back
	result, err := ParseNFO(nfoPath)
	require.NoError(t, err)
	require.NotNil(t, result)

	parsedMovie := result.Movie

	// Verify key fields match
	assert.Equal(t, originalMovie.Title, parsedMovie.Title)
	assert.Equal(t, originalMovie.OriginalTitle, parsedMovie.OriginalTitle)
	assert.Equal(t, originalMovie.ID, parsedMovie.ID)
	assert.Equal(t, originalMovie.ContentID, parsedMovie.ContentID)
	assert.Equal(t, originalMovie.Description, parsedMovie.Description)
	assert.Equal(t, originalMovie.Director, parsedMovie.Director)
	assert.Equal(t, originalMovie.Maker, parsedMovie.Maker)
	assert.Equal(t, originalMovie.Runtime, parsedMovie.Runtime)
	assert.Equal(t, originalMovie.ReleaseYear, parsedMovie.ReleaseYear)

	// Verify date (might lose some precision, so check date components)
	require.NotNil(t, parsedMovie.ReleaseDate)
	assert.Equal(t, originalMovie.ReleaseDate.Year(), parsedMovie.ReleaseDate.Year())
	assert.Equal(t, originalMovie.ReleaseDate.Month(), parsedMovie.ReleaseDate.Month())
	assert.Equal(t, originalMovie.ReleaseDate.Day(), parsedMovie.ReleaseDate.Day())

	// Verify rating
	assert.InDelta(t, originalMovie.RatingScore, parsedMovie.RatingScore, 0.01)
	assert.Equal(t, originalMovie.RatingVotes, parsedMovie.RatingVotes)

	// Verify actresses
	require.Len(t, parsedMovie.Actresses, 1)
	assert.Equal(t, "Yui", parsedMovie.Actresses[0].FirstName)
	assert.Equal(t, "Hatano", parsedMovie.Actresses[0].LastName)
	assert.Equal(t, "波多野結衣", parsedMovie.Actresses[0].JapaneseName)

	// Verify genres
	require.Len(t, parsedMovie.Genres, 2)
	genreNames := []string{parsedMovie.Genres[0].Name, parsedMovie.Genres[1].Name}
	assert.Contains(t, genreNames, "Drama")
	assert.Contains(t, genreNames, "Romance")

	// Verify media URLs
	assert.Equal(t, originalMovie.CoverURL, parsedMovie.CoverURL)
	assert.Equal(t, originalMovie.TrailerURL, parsedMovie.TrailerURL)
	assert.Equal(t, originalMovie.Screenshots, parsedMovie.Screenshots)
}

func TestParseActorToActress(t *testing.T) {
	tests := []struct {
		name          string
		actor         Actor
		wantFirstName string
		wantLastName  string
		wantJapanese  string
	}{
		{
			name: "English name with Japanese role",
			actor: Actor{
				Name: "Yui Hatano",
				Role: "波多野結衣",
			},
			wantFirstName: "Yui",
			wantLastName:  "Hatano",
			wantJapanese:  "波多野結衣",
		},
		{
			name: "English name with English role",
			actor: Actor{
				Name: "Yui Hatano",
				Role: "Actress",
			},
			wantFirstName: "Yui",
			wantLastName:  "Hatano",
			wantJapanese:  "",
		},
		{
			name: "Single name",
			actor: Actor{
				Name: "Madonna",
			},
			wantFirstName: "Madonna",
			wantLastName:  "",
			wantJapanese:  "",
		},
		{
			name: "Japanese name in Name field (no Role)",
			actor: Actor{
				Name: "波多野結衣",
			},
			wantFirstName: "", // Japanese detected, not used for romanized names
			wantLastName:  "",
			wantJapanese:  "波多野結衣",
		},
		{
			name: "English name with Japanese in Name (fallback when Role empty)",
			actor: Actor{
				Name: "Yui Hatano 波多野結衣",
				Role: "",
			},
			wantFirstName: "", // Japanese detected, entire field treated as Japanese
			wantLastName:  "",
			wantJapanese:  "Yui Hatano 波多野結衣",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actress := parseActorToActress(tt.actor)
			assert.Equal(t, tt.wantFirstName, actress.FirstName)
			assert.Equal(t, tt.wantLastName, actress.LastName)
			assert.Equal(t, tt.wantJapanese, actress.JapaneseName)
		})
	}
}
