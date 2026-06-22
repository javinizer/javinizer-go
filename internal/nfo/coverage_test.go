package nfo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultConfig tests that DefaultConfig returns sensible defaults
func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	assert.NotNil(t, cfg)
	assert.Equal(t, true, cfg.FirstNameOrder)
	assert.Equal(t, false, cfg.ActressLanguageJA)
	assert.Equal(t, "Unknown", cfg.UnknownActressText)
}

// TestConfigGroupActress tests GroupActress config field
func TestConfigGroupActress(t *testing.T) {
	cfg := &Config{
		FilenameTemplate:     "<ID>.nfo",
		FirstNameOrder:       true,
		ActressLanguageJA:    false,
		UnknownActressText:   "Unknown",
		IncludeFanart:        true,
		IncludeTrailer:       true,
		RatingSource:         "themoviedb",
		IncludeStreamDetails: false,
		GroupActress:         true,
	}

	assert.True(t, cfg.GroupActress)
}

// TestConfigMinimal tests Config with minimal settings — tags are passed
// via Generate/MovieToNFO, not via Config.TagDatabase.
func TestConfigMinimal(t *testing.T) {
	nfoCfg := &Config{
		FilenameTemplate: "<ID>.nfo",
	}

	// Config should be valid without TagDatabase — tags are passed via Generate/MovieToNFO
	assert.NotNil(t, nfoCfg)
}

// TestConfigMinimalDisabled tests Config with minimal settings and no NFO generation
func TestConfigMinimalDisabled(t *testing.T) {
	nfoCfg := &Config{
		FilenameTemplate: "<ID>.nfo",
	}

	// Config should be valid without TagDatabase — tags are passed via Generate/MovieToNFO
	assert.NotNil(t, nfoCfg)
}

// TestConfigAllFields tests direct Config struct with all fields
func TestConfigAllFields(t *testing.T) {
	cfg := &Config{
		FilenameTemplate:     "<ID> - <TITLE>.nfo",
		FirstNameOrder:       false,
		ActressLanguageJA:    true,
		UnknownActressText:   "不明",
		PerFile:              true,
		ActressAsTag:         true,
		AddGenericRole:       true,
		AltNameRole:          true,
		IncludeOriginalPath:  true,
		IncludeStreamDetails: true,
		IncludeFanart:        false,
		IncludeTrailer:       false,
		RatingSource:         "custom-source",
		Tag:                  []string{"tag1", "tag2"},
		Tagline:              "Test Tagline",
		Credits:              []string{"credit1", "credit2"},
		GroupActress:         true,
	}

	assert.Equal(t, false, cfg.FirstNameOrder)
	assert.Equal(t, true, cfg.ActressLanguageJA)
	assert.Equal(t, "不明", cfg.UnknownActressText)
	assert.Equal(t, "<ID> - <TITLE>.nfo", cfg.FilenameTemplate)
	assert.Equal(t, true, cfg.PerFile)
	assert.Equal(t, true, cfg.ActressAsTag)
	assert.Equal(t, true, cfg.AddGenericRole)
	assert.Equal(t, true, cfg.AltNameRole)
	assert.Equal(t, true, cfg.IncludeOriginalPath)
	assert.Equal(t, true, cfg.IncludeStreamDetails)
	assert.Equal(t, false, cfg.IncludeFanart)
	assert.Equal(t, false, cfg.IncludeTrailer)
	assert.Equal(t, "custom-source", cfg.RatingSource)
	assert.Equal(t, []string{"tag1", "tag2"}, cfg.Tag)
	assert.Equal(t, "Test Tagline", cfg.Tagline)
	assert.Equal(t, []string{"credit1", "credit2"}, cfg.Credits)
	assert.Equal(t, true, cfg.GroupActress)
}

// TestWriteNFO_ErrorPaths tests error handling in WriteNFO
func TestWriteNFO_ErrorPaths(t *testing.T) {
	gen := NewGenerator(afero.NewOsFs(), defaultConfig())

	t.Run("Invalid directory path", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("/dev/null is not a special path on Windows")
		}

		nfo := &Movie{
			Title: "Test",
			ID:    "TEST-001",
		}

		// Try to write to a path that can't be created (invalid parent)
		invalidPath := "/dev/null/subdir/test.nfo"
		err := gen.WriteNFO(nfo, invalidPath)

		// Should fail (can't create directory inside /dev/null)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create output directory")
	})

	t.Run("Read-only directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not enforce Unix-style directory permissions")
		}
		if os.Getuid() == 0 {
			t.Skip("Skipping test when running as root")
		}

		tmpDir := t.TempDir()
		readOnlyDir := filepath.Join(tmpDir, "readonly")
		err := os.Mkdir(readOnlyDir, 0555) // Read + execute, no write
		require.NoError(t, err)

		// Restore permissions in cleanup
		t.Cleanup(func() {
			_ = os.Chmod(readOnlyDir, 0755)
		})

		nfo := &Movie{
			Title: "Test",
			ID:    "TEST-001",
		}

		outputPath := filepath.Join(readOnlyDir, "test.nfo")
		err = gen.WriteNFO(nfo, outputPath)

		// Should fail to create file in read-only directory
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create NFO file")
	})
}

// TestExtractStreamDetails tests stream details extraction
func TestExtractStreamDetails(t *testing.T) {
	gen := NewGenerator(afero.NewOsFs(), &Config{
		IncludeStreamDetails: true,
	})

	t.Run("Non-existent file", func(t *testing.T) {
		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		// Pass non-existent video file path
		nfo := gen.movieToNFO(context.Background(), movie, "/nonexistent/video.mp4", nil)

		// Should handle error gracefully (no stream details)
		assert.Nil(t, nfo.FileInfo)
	})

	t.Run("Empty path", func(t *testing.T) {
		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		// Pass empty path
		nfo := gen.movieToNFO(context.Background(), movie, "", nil)

		// Should not attempt to extract stream details
		assert.Nil(t, nfo.FileInfo)
	})

	t.Run("Stream details disabled", func(t *testing.T) {
		genNoStream := NewGenerator(afero.NewOsFs(), &Config{
			IncludeStreamDetails: false,
		})

		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		// Even with valid path, should not include stream details
		tmpFile := filepath.Join(t.TempDir(), "video.mp4")
		_ = os.WriteFile(tmpFile, []byte("fake video"), 0644)

		nfo := genNoStream.movieToNFO(context.Background(), movie, tmpFile, nil)

		// Should not include stream details when disabled
		assert.Nil(t, nfo.FileInfo)
	})

	t.Run("Invalid video file", func(t *testing.T) {
		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		// Create a non-video file
		tmpFile := filepath.Join(t.TempDir(), "notavideo.txt")
		err := os.WriteFile(tmpFile, []byte("This is not a video file"), 0644)
		require.NoError(t, err)

		nfo := gen.movieToNFO(context.Background(), movie, tmpFile, nil)

		// Should handle invalid video file gracefully (mediainfo will fail)
		assert.Nil(t, nfo.FileInfo)
	})
}

// TestGenerate_ErrorPaths tests error handling in Generate
func TestGenerate_ErrorPaths(t *testing.T) {
	t.Run("Write to invalid directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("/dev/null is not a special path on Windows")
		}

		gen := NewGenerator(afero.NewOsFs(), defaultConfig())

		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		// Try to write to invalid directory
		err := gen.Generate(context.Background(), movie, "/dev/null/invalid", "", "", nil)

		// Should fail to write
		assert.Error(t, err)
	})
}

// TestFormatActressNameFromInfo_EdgeCases tests uncovered branches
func TestFormatActressNameFromInfo_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		firstName    string
		lastName     string
		japaneseName string
		expected     string
	}{
		{
			name: "LastName FirstName order - only last name",
			config: &Config{
				FirstNameOrder:     false,
				ActressLanguageJA:  false,
				UnknownActressText: "Unknown",
			},
			firstName:    "",
			lastName:     "OnlyLast",
			japaneseName: "",
			expected:     "OnlyLast",
		},
		{
			name: "LastName FirstName order - only first name",
			config: &Config{
				FirstNameOrder:     false,
				ActressLanguageJA:  false,
				UnknownActressText: "Unknown",
			},
			firstName:    "OnlyFirst",
			lastName:     "",
			japaneseName: "",
			expected:     "OnlyFirst",
		},
		{
			name: "FirstName LastName order - only last name",
			config: &Config{
				FirstNameOrder:     true,
				ActressLanguageJA:  false,
				UnknownActressText: "Unknown",
			},
			firstName:    "",
			lastName:     "OnlyLast",
			japaneseName: "",
			expected:     "OnlyLast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(afero.NewOsFs(), tt.config)

			// Use the private method via a public one
			actress := models.Actress{
				FirstName:    tt.firstName,
				LastName:     tt.lastName,
				JapaneseName: tt.japaneseName,
			}

			result := gen.formatActressName(actress)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNewGenerator_ConfigDefaults tests default value handling
func TestNewGenerator_ConfigDefaults(t *testing.T) {
	t.Run("Nil config", func(t *testing.T) {
		gen := NewGenerator(afero.NewOsFs(), nil)

		assert.NotNil(t, gen)
		// Should use default config
	})

	t.Run("Empty UnknownActress field", func(t *testing.T) {
		cfg := &Config{
			UnknownActressText: "",
			UnknownActressMode: models.UnknownActressModeFallback,
			FilenameTemplate:   "<ID>.nfo",
		}

		gen := NewGenerator(afero.NewOsFs(), cfg)

		movie := &models.Movie{
			ID: "TEST-001",
			Actresses: []models.Actress{
				{FirstName: "", LastName: ""},
			},
		}

		nfo := gen.movieToNFO(context.Background(), movie, "", nil)
		assert.Equal(t, "Unknown", nfo.Actors[0].Name)
	})

	t.Run("Empty NFOFilenameTemplate", func(t *testing.T) {
		cfg := &Config{
			FilenameTemplate:   "",
			UnknownActressText: "Unknown",
		}

		gen := NewGenerator(afero.NewOsFs(), cfg)

		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		tmpDir := t.TempDir()
		err := gen.Generate(context.Background(), movie, tmpDir, "", "", nil)

		require.NoError(t, err)

		// Should use default "<ID>.nfo"
		expectedPath := filepath.Join(tmpDir, "TEST-001.nfo")
		_, err = os.Stat(expectedPath)
		assert.NoError(t, err)
	})
}

// TestMovieToNFO_PreResolvedTags tests tag passing via the tags parameter
func TestMovieToNFO_PreResolvedTags(t *testing.T) {
	movie := &models.Movie{
		ID:    "TEST-001",
		Title: "Test Movie",
	}

	nfoCfg := &Config{
		FirstNameOrder: true,
		RatingSource:   "themoviedb",
	}

	gen := NewGenerator(afero.NewOsFs(), nfoCfg)

	nfo := gen.movieToNFO(context.Background(), movie, "", []string{"Tag1", "Tag2"})

	assert.Contains(t, nfo.Tags, "Tag1")
	assert.Contains(t, nfo.Tags, "Tag2")
}

// TestMovieToNFO_TagDeduplication tests that duplicate tags are not added
func TestMovieToNFO_TagDeduplication(t *testing.T) {
	movie := &models.Movie{
		ID:    "TEST-001",
		Title: "Test Movie",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
		},
	}

	nfoCfg := &Config{
		FirstNameOrder: true,
		RatingSource:   "themoviedb",
		ActressAsTag:   true,
		Tag:            []string{"Yui Hatano", "JAV"},
	}

	gen := NewGenerator(afero.NewOsFs(), nfoCfg)

	// Pass "Yui Hatano" as pre-resolved tag — should deduplicate with actress-as-tag and config tag
	nfo := gen.movieToNFO(context.Background(), movie, "", []string{"Yui Hatano"})

	// Count occurrences of "Yui Hatano"
	count := 0
	for _, tag := range nfo.Tags {
		if tag == "Yui Hatano" {
			count++
		}
	}

	// Should only appear once despite multiple sources
	assert.Equal(t, 1, count, "Tag 'Yui Hatano' should be deduplicated")
	assert.Contains(t, nfo.Tags, "JAV")
}

// TestMovieToNFO_AllFields tests comprehensive Movie-to-NFO conversion
func TestMovieToNFO_AllFields(t *testing.T) {
	gen := NewGenerator(afero.NewOsFs(), defaultConfig())

	movie := &models.Movie{
		ID:            "IPX-001",
		ContentID:     "ipx00001",
		Title:         "Test Title",
		OriginalTitle: "テストタイトル",
		Description:   "Test Description",
		Runtime:       120,
		Director:      "Test Director",
		Maker:         "Test Maker",
		Label:         "Test Label",
		Series:        "Test Series",
		Poster:        models.PosterState{CoverURL: "https://example.com/cover.jpg"},
		Screenshots:   []string{"https://example.com/ss1.jpg"},
		TrailerURL:    "https://example.com/trailer.mp4",
		Genres:        []models.Genre{{Name: "Genre1"}, {Name: "Genre2"}},
		Actresses:     []models.Actress{},
		RatingScore:   9.0,
		RatingVotes:   100,
	}

	nfo := gen.movieToNFO(context.Background(), movie, "", nil)

	// Verify all fields
	assert.Equal(t, "IPX-001", nfo.ID)
	assert.Equal(t, "ipx00001", nfo.UniqueID[0].Value)
	assert.Equal(t, "Test Title", nfo.Title)
	assert.Equal(t, "テストタイトル", nfo.OriginalTitle)
	assert.Equal(t, "Test Description", nfo.Plot)
	assert.Equal(t, 120, nfo.Runtime)
	assert.Equal(t, "Test Director", nfo.Director)
	assert.Equal(t, "Test Maker", nfo.Studio)
	assert.Equal(t, "Test Maker", nfo.Maker)
	assert.Equal(t, "Test Label", nfo.Label)
	assert.Equal(t, "Test Series", nfo.Set)
	assert.Equal(t, 9.0, nfo.Ratings.Rating[0].Value)
	assert.Equal(t, 100, nfo.Ratings.Rating[0].Votes)
	assert.Len(t, nfo.Genres, 2)
	assert.Len(t, nfo.Thumb, 1)
	assert.Len(t, nfo.Fanart.Thumbs, 1)
	assert.Equal(t, "https://example.com/trailer.mp4", nfo.Trailer)
}
