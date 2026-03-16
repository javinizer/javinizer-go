package nfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigFromAppConfig_NilConfig tests the nil config path
func TestConfigFromAppConfig_NilConfig(t *testing.T) {
	cfg := ConfigFromAppConfig(nil, nil, nil, nil)

	// Should return default config when nil
	assert.NotNil(t, cfg)
	assert.Equal(t, true, cfg.ActorFirstNameOrder)
	assert.Equal(t, false, cfg.ActorJapaneseNames)
	assert.Equal(t, "Unknown", cfg.UnknownActress)
}

// TestConfigFromAppConfig_WithGroupActress tests group actress config
func TestConfigFromAppConfig_WithGroupActress(t *testing.T) {
	appCfg := &config.NFOConfig{
		FilenameTemplate:     "<ID>.nfo",
		FirstNameOrder:       true,
		ActressLanguageJA:    false,
		UnknownActressText:   "Unknown",
		IncludeFanart:        true,
		IncludeTrailer:       true,
		RatingSource:         "themoviedb",
		IncludeStreamDetails: false,
	}

	outputCfg := &config.OutputConfig{
		GroupActress: true,
	}

	cfg := ConfigFromAppConfig(appCfg, outputCfg, nil, nil)

	assert.True(t, cfg.GroupActress)
}

// TestConfigFromAppConfig_WithDatabase tests database integration
func TestConfigFromAppConfig_WithDatabase(t *testing.T) {
	// Create in-memory database
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	appCfg := &config.NFOConfig{
		FilenameTemplate: "<ID>.nfo",
	}

	metadataCfg := &config.MetadataConfig{
		TagDatabase: config.TagDatabaseConfig{
			Enabled: true,
		},
	}

	nfoCfg := ConfigFromAppConfig(appCfg, nil, metadataCfg, db)

	// Should have tag database repository set
	assert.NotNil(t, nfoCfg.TagDatabase)
}

// TestConfigFromAppConfig_DatabaseDisabled tests when tag database is disabled
func TestConfigFromAppConfig_DatabaseDisabled(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	appCfg := &config.NFOConfig{
		FilenameTemplate: "<ID>.nfo",
	}

	metadataCfg := &config.MetadataConfig{
		TagDatabase: config.TagDatabaseConfig{
			Enabled: false, // Explicitly disabled
		},
	}

	nfoCfg := ConfigFromAppConfig(appCfg, nil, metadataCfg, db)

	// Should NOT have tag database repository set
	assert.Nil(t, nfoCfg.TagDatabase)
}

// TestConfigFromAppConfig_AllFields tests all field mappings
func TestConfigFromAppConfig_AllFields(t *testing.T) {
	appCfg := &config.NFOConfig{
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
	}

	outputCfg := &config.OutputConfig{
		GroupActress: true,
	}

	cfg := ConfigFromAppConfig(appCfg, outputCfg, nil, nil)

	// Verify all fields are mapped
	assert.Equal(t, false, cfg.ActorFirstNameOrder)
	assert.Equal(t, true, cfg.ActorJapaneseNames)
	assert.Equal(t, "不明", cfg.UnknownActress)
	assert.Equal(t, "<ID> - <TITLE>.nfo", cfg.NFOFilenameTemplate)
	assert.Equal(t, true, cfg.PerFile)
	assert.Equal(t, true, cfg.ActressAsTag)
	assert.Equal(t, true, cfg.AddGenericRole)
	assert.Equal(t, true, cfg.AltNameRole)
	assert.Equal(t, true, cfg.IncludeOriginalPath)
	assert.Equal(t, true, cfg.IncludeStreamDetails)
	assert.Equal(t, false, cfg.IncludeFanart)
	assert.Equal(t, false, cfg.IncludeTrailer)
	assert.Equal(t, "custom-source", cfg.DefaultRatingSource)
	assert.Equal(t, []string{"tag1", "tag2"}, cfg.StaticTags)
	assert.Equal(t, "Test Tagline", cfg.StaticTagline)
	assert.Equal(t, []string{"credit1", "credit2"}, cfg.StaticCredits)
	assert.Equal(t, true, cfg.GroupActress)
}

// TestWriteNFO_ErrorPaths tests error handling in WriteNFO
func TestWriteNFO_ErrorPaths(t *testing.T) {
	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())

	t.Run("Invalid directory path", func(t *testing.T) {
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
		nfo := gen.MovieToNFO(movie, "/nonexistent/video.mp4")

		// Should handle error gracefully (no stream details)
		assert.Nil(t, nfo.FileInfo)
	})

	t.Run("Empty path", func(t *testing.T) {
		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		// Pass empty path
		nfo := gen.MovieToNFO(movie, "")

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

		nfo := genNoStream.MovieToNFO(movie, tmpFile)

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

		nfo := gen.MovieToNFO(movie, tmpFile)

		// Should handle invalid video file gracefully (mediainfo will fail)
		assert.Nil(t, nfo.FileInfo)
	})
}

// TestGenerateFromScraperResult_ErrorPaths tests error handling
func TestGenerateFromScraperResult_ErrorPaths(t *testing.T) {
	t.Run("Invalid write location", func(t *testing.T) {
		gen := NewGenerator(afero.NewOsFs(), DefaultConfig())

		result := &models.ScraperResult{
			ID:    "TEST-001",
			Title: "Test Movie",
		}

		// Try to write to invalid location
		err := gen.GenerateFromScraperResult(result, "/dev/null/invalid")

		// Should fail to write
		assert.Error(t, err)
	})
}

// TestGenerate_ErrorPaths tests error handling in Generate
func TestGenerate_ErrorPaths(t *testing.T) {
	t.Run("Write to invalid directory", func(t *testing.T) {
		gen := NewGenerator(afero.NewOsFs(), DefaultConfig())

		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		// Try to write to invalid directory
		err := gen.Generate(movie, "/dev/null/invalid", "", "")

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
				ActorFirstNameOrder: false,
				ActorJapaneseNames:  false,
				UnknownActress:      "Unknown",
			},
			firstName:    "",
			lastName:     "OnlyLast",
			japaneseName: "",
			expected:     "OnlyLast",
		},
		{
			name: "LastName FirstName order - only first name",
			config: &Config{
				ActorFirstNameOrder: false,
				ActorJapaneseNames:  false,
				UnknownActress:      "Unknown",
			},
			firstName:    "OnlyFirst",
			lastName:     "",
			japaneseName: "",
			expected:     "OnlyFirst",
		},
		{
			name: "FirstName LastName order - only last name",
			config: &Config{
				ActorFirstNameOrder: true,
				ActorJapaneseNames:  false,
				UnknownActress:      "Unknown",
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
			UnknownActress:      "", // Empty, should default
			NFOFilenameTemplate: "<ID>.nfo",
		}

		gen := NewGenerator(afero.NewOsFs(), cfg)

		// Should set default
		movie := &models.Movie{
			ID: "TEST-001",
			Actresses: []models.Actress{
				{FirstName: "", LastName: ""}, // Unknown actress
			},
		}

		nfo := gen.MovieToNFO(movie, "")
		assert.Equal(t, "Unknown", nfo.Actors[0].Name)
	})

	t.Run("Empty NFOFilenameTemplate", func(t *testing.T) {
		cfg := &Config{
			NFOFilenameTemplate: "", // Empty, should default
			UnknownActress:      "Unknown",
		}

		gen := NewGenerator(afero.NewOsFs(), cfg)

		movie := &models.Movie{
			ID:    "TEST-001",
			Title: "Test",
		}

		tmpDir := t.TempDir()
		err := gen.Generate(movie, tmpDir, "", "")

		require.NoError(t, err)

		// Should use default "<ID>.nfo"
		expectedPath := filepath.Join(tmpDir, "TEST-001.nfo")
		_, err = os.Stat(expectedPath)
		assert.NoError(t, err)
	})
}

// TestMovieToNFO_DatabaseTags tests tag database integration
func TestMovieToNFO_DatabaseTags(t *testing.T) {
	// Create in-memory database
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	// Create tag repository
	tagRepo := database.NewMovieTagRepository(db)

	// Insert test movie with tags
	movie := &models.Movie{
		ID:    "TEST-001",
		Title: "Test Movie",
	}

	// Add tags to database (use AddTag, not AddTagToMovie)
	err = tagRepo.AddTag(movie.ID, "Tag1")
	require.NoError(t, err)
	err = tagRepo.AddTag(movie.ID, "Tag2")
	require.NoError(t, err)

	// Create generator with tag database
	nfoCfg := &Config{
		ActorFirstNameOrder: true,
		DefaultRatingSource: "themoviedb",
		TagDatabase:         tagRepo,
	}

	gen := NewGenerator(afero.NewOsFs(), nfoCfg)

	nfo := gen.MovieToNFO(movie, "")

	// Should include database tags
	assert.Contains(t, nfo.Tags, "Tag1")
	assert.Contains(t, nfo.Tags, "Tag2")
}

// TestMovieToNFO_TagDeduplication tests that duplicate tags are not added
func TestMovieToNFO_TagDeduplication(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	tagRepo := database.NewMovieTagRepository(db)

	movie := &models.Movie{
		ID:    "TEST-001",
		Title: "Test Movie",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
		},
	}

	// Add actress name as database tag (should deduplicate)
	err = tagRepo.AddTag(movie.ID, "Yui Hatano")
	require.NoError(t, err)

	nfoCfg := &Config{
		ActorFirstNameOrder: true,
		DefaultRatingSource: "themoviedb",
		ActressAsTag:        true,
		StaticTags:          []string{"Yui Hatano", "JAV"}, // Also in static tags
		TagDatabase:         tagRepo,
	}

	gen := NewGenerator(afero.NewOsFs(), nfoCfg)

	nfo := gen.MovieToNFO(movie, "")

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

// TestScraperResultToNFO_AllFields tests comprehensive scraper result conversion
func TestScraperResultToNFO_AllFields(t *testing.T) {
	gen := NewGenerator(afero.NewOsFs(), DefaultConfig())

	result := &models.ScraperResult{
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
		CoverURL:      "https://example.com/cover.jpg",
		ScreenshotURL: []string{"https://example.com/ss1.jpg"},
		TrailerURL:    "https://example.com/trailer.mp4",
		Genres:        []string{"Genre1", "Genre2"},
		Actresses:     []models.ActressInfo{},
		Rating:        &models.Rating{Score: 9.0, Votes: 100},
	}

	nfo := gen.ScraperResultToNFO(result)

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
