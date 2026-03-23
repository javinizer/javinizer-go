package nfo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNFOXMLGeneration tests complete XML generation from Movie model
func TestNFOXMLGeneration(t *testing.T) {
	tests := []struct {
		name   string
		movie  *models.Movie
		config *Config
		checks []func(t *testing.T, nfo *Movie)
	}{
		{
			name: "complete movie with all fields",
			movie: &models.Movie{
				ID:            "IPX-123",
				ContentID:     "ipx00123",
				Title:         "Test Movie Title",
				OriginalTitle: "テストムービータイトル",
				Description:   "This is a comprehensive test movie description with multiple lines.\nSecond line of description.",
				ReleaseDate:   timePtr(time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)),
				Runtime:       150,
				Director:      "Test Director Name",
				Maker:         "Test Studio",
				Label:         "Test Label",
				Series:        "Test Series Name",
				RatingScore:   8.7,
				RatingVotes:   250,
				CoverURL:      "https://example.com/cover.jpg",
				TrailerURL:    "https://example.com/trailer.mp4",
				Screenshots: []string{
					"https://example.com/screenshot1.jpg",
					"https://example.com/screenshot2.jpg",
					"https://example.com/screenshot3.jpg",
				},
				Actresses: []models.Actress{
					{
						FirstName:    "Yui",
						LastName:     "Hatano",
						JapaneseName: "波多野結衣",
						ThumbURL:     "https://example.com/actress1.jpg",
					},
					{
						FirstName:    "Momo",
						LastName:     "Sakura",
						JapaneseName: "桜空もも",
						ThumbURL:     "https://example.com/actress2.jpg",
					},
				},
				Genres: []models.Genre{
					{Name: "Drama"},
					{Name: "Romance"},
					{Name: "4HR+"},
				},
			},
			config: DefaultConfig(),
			checks: []func(t *testing.T, nfo *Movie){
				func(t *testing.T, nfo *Movie) {
					assert.Equal(t, "IPX-123", nfo.ID)
					assert.Equal(t, "Test Movie Title", nfo.Title)
					assert.Equal(t, "テストムービータイトル", nfo.OriginalTitle)
					assert.Equal(t, "IPX-123", nfo.SortTitle)
				},
				func(t *testing.T, nfo *Movie) {
					assert.Contains(t, nfo.Plot, "comprehensive test movie description")
					assert.Contains(t, nfo.Plot, "Second line of description")
				},
				func(t *testing.T, nfo *Movie) {
					assert.Equal(t, 2023, nfo.Year)
					assert.Equal(t, "2023-05-15", nfo.ReleaseDate)
					assert.Equal(t, "2023-05-15", nfo.Premiered)
				},
				func(t *testing.T, nfo *Movie) {
					assert.Equal(t, 150, nfo.Runtime)
					assert.Equal(t, "Test Director Name", nfo.Director)
					assert.Equal(t, "Test Studio", nfo.Studio)
					assert.Equal(t, "Test Studio", nfo.Maker)
					assert.Equal(t, "Test Label", nfo.Label)
					assert.Equal(t, "Test Series Name", nfo.Set)
				},
				func(t *testing.T, nfo *Movie) {
					require.Len(t, nfo.UniqueID, 1)
					assert.Equal(t, "contentid", nfo.UniqueID[0].Type)
					assert.Equal(t, "ipx00123", nfo.UniqueID[0].Value)
					assert.True(t, nfo.UniqueID[0].Default)
				},
				func(t *testing.T, nfo *Movie) {
					require.Len(t, nfo.Ratings.Rating, 1)
					assert.Equal(t, "themoviedb", nfo.Ratings.Rating[0].Name)
					assert.Equal(t, 8.7, nfo.Ratings.Rating[0].Value)
					assert.Equal(t, 250, nfo.Ratings.Rating[0].Votes)
					assert.Equal(t, 10, nfo.Ratings.Rating[0].Max)
					assert.True(t, nfo.Ratings.Rating[0].Default)
				},
				func(t *testing.T, nfo *Movie) {
					require.Len(t, nfo.Actors, 2)
					assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
					assert.Equal(t, 0, nfo.Actors[0].Order)
					assert.Equal(t, "https://example.com/actress1.jpg", nfo.Actors[0].Thumb)
					assert.Equal(t, "Momo Sakura", nfo.Actors[1].Name)
					assert.Equal(t, 1, nfo.Actors[1].Order)
				},
				func(t *testing.T, nfo *Movie) {
					require.Len(t, nfo.Genres, 3)
					assert.Contains(t, nfo.Genres, "Drama")
					assert.Contains(t, nfo.Genres, "Romance")
					assert.Contains(t, nfo.Genres, "4HR+")
				},
				func(t *testing.T, nfo *Movie) {
					require.Len(t, nfo.Thumb, 1)
					assert.Equal(t, "poster", nfo.Thumb[0].Aspect)
					assert.Equal(t, "https://example.com/cover.jpg", nfo.Thumb[0].Value)
				},
				func(t *testing.T, nfo *Movie) {
					require.NotNil(t, nfo.Fanart)
					require.Len(t, nfo.Fanart.Thumbs, 3)
					assert.Equal(t, "https://example.com/screenshot1.jpg", nfo.Fanart.Thumbs[0].Value)
				},
				func(t *testing.T, nfo *Movie) {
					assert.Equal(t, "https://example.com/trailer.mp4", nfo.Trailer)
				},
			},
		},
		{
			name: "minimal movie with required fields only",
			movie: &models.Movie{
				ID:    "MIN-001",
				Title: "Minimal Movie",
			},
			config: DefaultConfig(),
			checks: []func(t *testing.T, nfo *Movie){
				func(t *testing.T, nfo *Movie) {
					assert.Equal(t, "MIN-001", nfo.ID)
					assert.Equal(t, "Minimal Movie", nfo.Title)
					assert.Empty(t, nfo.OriginalTitle)
					assert.Zero(t, nfo.Year)
					assert.Zero(t, nfo.Runtime)
					assert.Empty(t, nfo.Actors)
					assert.Empty(t, nfo.Genres)
				},
			},
		},
		{
			name: "movie with special characters and unicode",
			movie: &models.Movie{
				ID:            "UNI-001",
				Title:         "Test with <Special> & \"Characters\"",
				OriginalTitle: "テスト映画 & 特殊文字 <test>",
				Description:   "Description with special chars: &<>\"'",
				Director:      "Director with émojis 🎬",
			},
			config: DefaultConfig(),
			checks: []func(t *testing.T, nfo *Movie){
				func(t *testing.T, nfo *Movie) {
					// Verify struct contains unescaped characters
					assert.Contains(t, nfo.Title, "<Special>")
					assert.Contains(t, nfo.Title, "&")
					assert.Contains(t, nfo.OriginalTitle, "テスト映画")
					assert.Contains(t, nfo.Director, "🎬")

					// Marshal to XML and verify proper escaping
					xmlData, err := xml.MarshalIndent(nfo, "", "  ")
					require.NoError(t, err)
					xmlStr := string(xmlData)

					// Verify XML escaping is applied
					assert.Contains(t, xmlStr, "&lt;Special&gt;")
					assert.Contains(t, xmlStr, "&amp;")
					assert.Contains(t, xmlStr, "テスト映画") // Unicode should be preserved
					assert.Contains(t, xmlStr, "🎬")     // Emoji should be preserved
					assert.NotContains(t, xmlStr, "& ") // Raw ampersand followed by space should be escaped
				},
			},
		},
		{
			name: "movie with zero rating (should not include rating)",
			movie: &models.Movie{
				ID:          "ZER-001",
				Title:       "Zero Rating Movie",
				RatingScore: 0,
				RatingVotes: 0,
			},
			config: DefaultConfig(),
			checks: []func(t *testing.T, nfo *Movie){
				func(t *testing.T, nfo *Movie) {
					assert.Empty(t, nfo.Ratings.Rating)
				},
			},
		},
		{
			name: "movie with empty content ID",
			movie: &models.Movie{
				ID:        "EMP-001",
				Title:     "Empty Content ID",
				ContentID: "",
			},
			config: DefaultConfig(),
			checks: []func(t *testing.T, nfo *Movie){
				func(t *testing.T, nfo *Movie) {
					assert.Empty(t, nfo.UniqueID)
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt // Rebind for safe parallel execution
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(afero.NewOsFs(), tt.config)
			nfo := gen.MovieToNFO(tt.movie, "")

			// Run all checks
			for _, check := range tt.checks {
				check(t, nfo)
			}
		})
	}
}

// TestNFOXMLMarshalling tests that generated NFO can be marshalled and unmarshalled
func TestNFOXMLMarshalling(t *testing.T) {
	releaseDate := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "MAR-001",
		ContentID:   "mar00001",
		Title:       "Marshalling Test",
		Description: "Test XML marshalling",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		RatingScore: 7.5,
		RatingVotes: 50,
		Actresses: []models.Actress{
			{FirstName: "Test", LastName: "Actress"},
		},
		Genres: []models.Genre{
			{Name: "Test Genre"},
		},
	}

	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
	nfo := gen.MovieToNFO(movie, "")

	// Marshal to XML
	xmlData, err := xml.MarshalIndent(nfo, "", "  ")
	require.NoError(t, err)
	require.NotEmpty(t, xmlData)

	// Verify XML structure
	assert.Contains(t, string(xmlData), "<movie>")
	assert.Contains(t, string(xmlData), "<title>Marshalling Test</title>")
	assert.Contains(t, string(xmlData), "<id>MAR-001</id>")

	// Unmarshal back
	var parsed Movie
	err = xml.Unmarshal(xmlData, &parsed)
	require.NoError(t, err)

	// Verify key fields
	assert.Equal(t, "MAR-001", parsed.ID)
	assert.Equal(t, "Marshalling Test", parsed.Title)
	assert.Equal(t, "Test XML marshalling", parsed.Plot)
	assert.Equal(t, 120, parsed.Runtime)
}

// TestNFOFileWriteAndRead tests writing NFO to disk and reading it back
func TestNFOFileWriteAndRead(t *testing.T) {
	tests := []struct {
		name     string
		movie    *models.Movie
		filename string
	}{
		{
			name: "simple write and read",
			movie: &models.Movie{
				ID:          "WR-001",
				Title:       "Write Read Test",
				Description: "Test file I/O",
				Runtime:     90,
			},
			filename: "WR-001.nfo",
		},
		{
			name: "filename with spaces",
			movie: &models.Movie{
				ID:          "WR-002",
				Title:       "File With Spaces",
				Description: "Test spaces in filename",
			},
			filename: "WR 002 File With Spaces.nfo",
		},
		{
			name: "filename with unicode",
			movie: &models.Movie{
				ID:          "WR-003",
				Title:       "Unicode Test テスト",
				Description: "Test unicode in filename",
			},
			filename: "WR-003-テスト.nfo",
		},
	}

	for _, tt := range tests {
		tt := tt // Rebind for safe parallel execution
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
			nfo := gen.MovieToNFO(tt.movie, "")

			// Write to temp directory
			tmpDir := t.TempDir()
			outputPath := filepath.Join(tmpDir, tt.filename)

			err := gen.WriteNFO(nfo, outputPath)
			require.NoError(t, err)

			// Verify file exists
			_, err = os.Stat(outputPath)
			require.NoError(t, err)

			// Read file content
			content, err := os.ReadFile(outputPath)
			require.NoError(t, err)
			require.NotEmpty(t, content)

			// Verify XML header
			assert.Contains(t, string(content), xml.Header)

			// Parse XML
			var parsed Movie
			err = xml.Unmarshal(content, &parsed)
			require.NoError(t, err)

			// Verify content matches
			assert.Equal(t, tt.movie.ID, parsed.ID)
			assert.Equal(t, tt.movie.Title, parsed.Title)
			assert.Equal(t, tt.movie.Description, parsed.Plot)
		})
	}
}

// TestNFOWithEmptyOptionalFields tests handling of empty/nil optional fields
func TestNFOWithEmptyOptionalFields(t *testing.T) {
	movie := &models.Movie{
		ID:          "EMP-001",
		Title:       "Empty Fields Test",
		ContentID:   "",
		Description: "",
		ReleaseDate: nil,
		Runtime:     0,
		Director:    "",
		Maker:       "",
		Label:       "",
		Series:      "",
		RatingScore: 0,
		RatingVotes: 0,
		CoverURL:    "",
		TrailerURL:  "",
		Screenshots: nil,
		Actresses:   nil,
		Genres:      nil,
	}

	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
	nfo := gen.MovieToNFO(movie, "")

	// Verify empty fields are not populated
	assert.Equal(t, "EMP-001", nfo.ID)
	assert.Equal(t, "Empty Fields Test", nfo.Title)
	assert.Empty(t, nfo.Plot)
	assert.Zero(t, nfo.Year)
	assert.Zero(t, nfo.Runtime)
	assert.Empty(t, nfo.Director)
	assert.Empty(t, nfo.Studio)
	assert.Empty(t, nfo.Maker)
	assert.Empty(t, nfo.Label)
	assert.Empty(t, nfo.Set)
	assert.Empty(t, nfo.UniqueID)
	assert.Empty(t, nfo.Ratings.Rating)
	assert.Empty(t, nfo.Actors)
	assert.Empty(t, nfo.Genres)
	assert.Empty(t, nfo.Thumb)
	assert.Nil(t, nfo.Fanart)
	assert.Empty(t, nfo.Trailer)
}

// TestNFOWithMultipleActresses tests handling of multiple actresses
func TestNFOWithMultipleActresses(t *testing.T) {
	actresses := make([]models.Actress, 10)
	for i := 0; i < 10; i++ {
		actresses[i] = models.Actress{
			FirstName:    "FirstName" + strconv.Itoa(i),
			LastName:     "LastName",
			JapaneseName: "日本名",
			ThumbURL:     "https://example.com/actress.jpg",
		}
	}

	movie := &models.Movie{
		ID:        "MULT-001",
		Title:     "Multiple Actresses",
		Actresses: actresses,
	}

	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
	nfo := gen.MovieToNFO(movie, "")

	require.Len(t, nfo.Actors, 10)
	for i, actor := range nfo.Actors {
		assert.Equal(t, "FirstName"+strconv.Itoa(i)+" LastName", actor.Name)
		assert.Equal(t, i, actor.Order)
		assert.Equal(t, "https://example.com/actress.jpg", actor.Thumb)
	}
}

// TestNFOWithManyGenres tests handling of many genres
func TestNFOWithManyGenres(t *testing.T) {
	genres := []models.Genre{
		{Name: "Drama"}, {Name: "Romance"}, {Name: "Comedy"},
		{Name: "Action"}, {Name: "Thriller"}, {Name: "Horror"},
		{Name: "Sci-Fi"}, {Name: "Fantasy"}, {Name: "Documentary"},
	}

	movie := &models.Movie{
		ID:     "GEN-001",
		Title:  "Many Genres",
		Genres: genres,
	}

	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
	nfo := gen.MovieToNFO(movie, "")

	require.Len(t, nfo.Genres, 9)
	assert.Contains(t, nfo.Genres, "Drama")
	assert.Contains(t, nfo.Genres, "Sci-Fi")
	assert.Contains(t, nfo.Genres, "Documentary")
}

// TestNFOWithManyScreenshots tests handling of many screenshots
func TestNFOWithManyScreenshots(t *testing.T) {
	screenshots := make([]string, 15)
	for i := 0; i < 15; i++ {
		screenshots[i] = "https://example.com/screenshot.jpg"
	}

	movie := &models.Movie{
		ID:          "SCR-001",
		Title:       "Many Screenshots",
		Screenshots: screenshots,
	}

	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
	nfo := gen.MovieToNFO(movie, "")

	require.NotNil(t, nfo.Fanart)
	require.Len(t, nfo.Fanart.Thumbs, 15)
}

// TestNFOFanartDisabled tests fanart generation when disabled
func TestNFOFanartDisabled(t *testing.T) {
	movie := &models.Movie{
		ID:    "FAN-001",
		Title: "No Fanart",
		Screenshots: []string{
			"https://example.com/screenshot1.jpg",
			"https://example.com/screenshot2.jpg",
		},
	}

	// Test disabled
	cfg := DefaultConfig()
	cfg.IncludeFanart = false

	gen := NewGenerator(afero.NewOsFs(), cfg)
	nfo := gen.MovieToNFO(movie, "")

	assert.Nil(t, nfo.Fanart)

	// Test enabled (default behavior)
	cfgEnabled := DefaultConfig()
	genEnabled := NewGenerator(afero.NewOsFs(), cfgEnabled)
	nfoEnabled := genEnabled.MovieToNFO(movie, "")

	require.NotNil(t, nfoEnabled.Fanart)
	assert.Len(t, nfoEnabled.Fanart.Thumbs, 2)
}

// TestNFOTrailerDisabled tests trailer generation when disabled
func TestNFOTrailerDisabled(t *testing.T) {
	movie := &models.Movie{
		ID:         "TRL-001",
		Title:      "No Trailer",
		TrailerURL: "https://example.com/trailer.mp4",
	}

	// Test disabled
	cfg := DefaultConfig()
	cfg.IncludeTrailer = false

	gen := NewGenerator(afero.NewOsFs(), cfg)
	nfo := gen.MovieToNFO(movie, "")

	assert.Empty(t, nfo.Trailer)

	// Test enabled (default behavior)
	cfgEnabled := DefaultConfig()
	genEnabled := NewGenerator(afero.NewOsFs(), cfgEnabled)
	nfoEnabled := genEnabled.MovieToNFO(movie, "")

	assert.Equal(t, "https://example.com/trailer.mp4", nfoEnabled.Trailer)
}

// TestNFOCustomRatingSource tests custom rating source
func TestNFOCustomRatingSource(t *testing.T) {
	movie := &models.Movie{
		ID:          "RAT-001",
		Title:       "Custom Rating",
		RatingScore: 9.5,
		RatingVotes: 1000,
	}

	cfg := DefaultConfig()
	cfg.DefaultRatingSource = "imdb"

	gen := NewGenerator(afero.NewOsFs(), cfg)
	nfo := gen.MovieToNFO(movie, "")

	require.Len(t, nfo.Ratings.Rating, 1)
	assert.Equal(t, "imdb", nfo.Ratings.Rating[0].Name)
	assert.Equal(t, 9.5, nfo.Ratings.Rating[0].Value)
	assert.Equal(t, 1000, nfo.Ratings.Rating[0].Votes)
}

// TestGenerateWithComplexTemplate tests template generation with various tags
func TestGenerateWithComplexTemplate(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "TPL-001",
		Title:       "Template Test",
		ReleaseDate: &releaseDate,
		Maker:       "Test Studio",
	}

	cfg := DefaultConfig()
	cfg.NFOFilenameTemplate = "<ID> [<STUDIO>].nfo"

	gen := NewGenerator(afero.NewOsFs(), cfg)
	tmpDir := t.TempDir()

	err := gen.Generate(movie, tmpDir, "", "")
	require.NoError(t, err)

	// Verify file was created with correct template-based name
	expectedPath := filepath.Join(tmpDir, "TPL-001 [Test Studio].nfo")
	_, err = os.Stat(expectedPath)
	assert.NoError(t, err)
}

// TestScraperResultToNFO_EmptyRating tests scraper result with nil rating
func TestScraperResultToNFO_EmptyRating(t *testing.T) {
	result := &models.ScraperResult{
		ID:     "SCR-001",
		Title:  "No Rating",
		Rating: nil,
	}

	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
	nfo := gen.ScraperResultToNFO(result)

	assert.Empty(t, nfo.Ratings.Rating)
}

// TestScraperResultToNFO_WithActresses tests actress conversion from scraper result
func TestScraperResultToNFO_WithActresses(t *testing.T) {
	result := &models.ScraperResult{
		ID:    "SCR-002",
		Title: "Actresses Test",
		Actresses: []models.ActressInfo{
			{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
				ThumbURL:     "https://example.com/actress.jpg",
			},
		},
	}

	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())
	nfo := gen.ScraperResultToNFO(result)

	require.Len(t, nfo.Actors, 1)
	assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
	assert.Equal(t, "https://example.com/actress.jpg", nfo.Actors[0].Thumb)
}

// TestScraperResultToNFO_WithGenericRole tests actress role generation
func TestScraperResultToNFO_WithGenericRole(t *testing.T) {
	result := &models.ScraperResult{
		ID:    "SCR-003",
		Title: "Role Test",
		Actresses: []models.ActressInfo{
			{FirstName: "Test", LastName: "Actress"},
		},
	}

	cfg := DefaultConfig()
	cfg.AddGenericRole = true

	gen := NewGenerator(afero.NewOsFs(), cfg)
	nfo := gen.ScraperResultToNFO(result)

	require.Len(t, nfo.Actors, 1)
	assert.Equal(t, "Actress", nfo.Actors[0].Role)
}

// TestScraperResultToNFO_WithAltNameRole tests alternate name in role field
func TestScraperResultToNFO_WithAltNameRole(t *testing.T) {
	result := &models.ScraperResult{
		ID:    "SCR-004",
		Title: "Alt Name Role Test",
		Actresses: []models.ActressInfo{
			{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
		},
	}

	cfg := DefaultConfig()
	cfg.AltNameRole = true

	gen := NewGenerator(afero.NewOsFs(), cfg)
	nfo := gen.ScraperResultToNFO(result)

	require.Len(t, nfo.Actors, 1)
	assert.Equal(t, "波多野結衣", nfo.Actors[0].Role)
}

// TestScraperResultToNFO_FanartDisabled tests scraper result without fanart
func TestScraperResultToNFO_FanartDisabled(t *testing.T) {
	result := &models.ScraperResult{
		ID:    "SCR-005",
		Title: "No Fanart",
		ScreenshotURL: []string{
			"https://example.com/screenshot.jpg",
		},
	}

	cfg := DefaultConfig()
	cfg.IncludeFanart = false

	gen := NewGenerator(afero.NewOsFs(), cfg)
	nfo := gen.ScraperResultToNFO(result)

	assert.Nil(t, nfo.Fanart)
}

// TestScraperResultToNFO_TrailerDisabled tests scraper result without trailer
func TestScraperResultToNFO_TrailerDisabled(t *testing.T) {
	result := &models.ScraperResult{
		ID:         "SCR-006",
		Title:      "No Trailer",
		TrailerURL: "https://example.com/trailer.mp4",
	}

	cfg := DefaultConfig()
	cfg.IncludeTrailer = false

	gen := NewGenerator(afero.NewOsFs(), cfg)
	nfo := gen.ScraperResultToNFO(result)

	assert.Empty(t, nfo.Trailer)
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}

// BenchmarkNFOGenerate measures the performance of NFO generation with 5 actresses and 10 genres
// This benchmark is for observation only - not a pass/fail gate
// Expected baseline: ~5ms per operation for full Movie
func BenchmarkNFOGenerate(b *testing.B) {
	// Setup: Create in-memory filesystem
	fs := afero.NewMemMapFs()
	cfg := DefaultConfig()
	gen := NewGenerator(fs, cfg)

	// Setup: Create test movie with 5 actresses and 10 genres
	releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:            "IPX-123",
		ContentID:     "ipx00123",
		Title:         "Test Movie Title",
		OriginalTitle: "テストムービータイトル",
		Description:   "This is a comprehensive test movie description for benchmark testing.",
		ReleaseDate:   &releaseDate,
		Runtime:       150,
		Director:      "Test Director",
		Maker:         "Test Studio",
		Label:         "Test Label",
		Series:        "Test Series",
		RatingScore:   8.7,
		RatingVotes:   250,
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
			{FirstName: "Momo", LastName: "Sakura", JapaneseName: "桜空もも"},
			{FirstName: "Aika", LastName: "Yumeno", JapaneseName: "夢乃あいか"},
			{FirstName: "Mia", LastName: "Nanasawa", JapaneseName: "七沢みあ"},
			{FirstName: "Sora", LastName: "Amakawa", JapaneseName: "天川そら"},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
			{Name: "4HR+"},
			{Name: "Big Tits"},
			{Name: "Beautiful Girl"},
			{Name: "Slender"},
			{Name: "Creampie"},
			{Name: "Blowjob"},
			{Name: "Threesome"},
			{Name: "Digital Mosaic"},
		},
	}

	outputPath := "/tmp/benchmark/test.nfo"
	partSuffix := ""
	videoFilePath := "/tmp/benchmark/IPX-123.mp4"

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Benchmark loop
	for i := 0; i < b.N; i++ {
		err := gen.Generate(movie, outputPath, partSuffix, videoFilePath)
		if err != nil {
			b.Fatalf("Generate failed: %v", err)
		}
	}
}
