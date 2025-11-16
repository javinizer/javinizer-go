package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPercentage(t *testing.T) {
	tests := []struct {
		name     string
		part     int64
		total    int64
		expected float64
	}{
		{
			name:     "normal case 50%",
			part:     50,
			total:    100,
			expected: 50.0,
		},
		{
			name:     "normal case 25%",
			part:     25,
			total:    100,
			expected: 25.0,
		},
		{
			name:     "zero total returns zero",
			part:     10,
			total:    0,
			expected: 0.0,
		},
		{
			name:     "zero part returns zero",
			part:     0,
			total:    100,
			expected: 0.0,
		},
		{
			name:     "decimal result",
			part:     1,
			total:    3,
			expected: 33.33333333333333,
		},
		{
			name:     "100 percent",
			part:     100,
			total:    100,
			expected: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := percentage(tt.part, tt.total)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPrintMovie_BasicFields(t *testing.T) {
	// Create a test movie with basic fields
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Test Movie Title",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Test Director",
		Maker:       "Test Maker",
		Label:       "Test Label",
		Series:      "Test Series",
		RatingScore: 8.5,
		RatingVotes: 100,
		Description: "This is a test description",
	}

	// Capture stdout - printMovie writes to stdout
	// We just verify it doesn't panic
	assert.NotPanics(t, func() {
		printMovie(movie, nil)
	})
}

func TestPrintMovie_WithActresses(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		Actresses: []models.Actress{
			{
				FirstName:    "Test",
				LastName:     "Actress",
				JapaneseName: "テスト女優",
			},
			{
				FirstName:    "Another",
				LastName:     "Actress",
				JapaneseName: "",
			},
		},
	}

	assert.NotPanics(t, func() {
		printMovie(movie, nil)
	})
}

func TestPrintMovie_WithGenres(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
			{Name: "Action"},
		},
	}

	assert.NotPanics(t, func() {
		printMovie(movie, nil)
	})
}

func TestPrintMovie_WithTranslations(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		Translations: []models.MovieTranslation{
			{
				Language:   "en",
				Title:      "English Title",
				SourceName: "r18dev",
			},
			{
				Language:   "ja",
				Title:      "Japanese Title",
				SourceName: "dmm",
			},
		},
		SourceName: "r18dev",
	}

	assert.NotPanics(t, func() {
		printMovie(movie, nil)
	})
}

func TestPrintMovie_WithMedia(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		CoverURL:    "https://example.com/cover.jpg",
		PosterURL:   "https://example.com/poster.jpg",
		TrailerURL:  "https://example.com/trailer.mp4",
		Screenshots: []string{
			"https://example.com/screen1.jpg",
			"https://example.com/screen2.jpg",
		},
	}

	assert.NotPanics(t, func() {
		printMovie(movie, nil)
	})
}

func TestPrintMovie_WithScraperResults(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
	}

	results := []*models.ScraperResult{
		{
			Source:    "r18dev",
			SourceURL: "https://r18.dev/movies/IPX-535",
			Title:     "Test from R18",
		},
		{
			Source:    "dmm",
			SourceURL: "https://dmm.co.jp/digital/video/-/detail/=/cid=ipx00535",
			Title:     "Test from DMM",
		},
	}

	assert.NotPanics(t, func() {
		printMovie(movie, results)
	})
}

func TestPrintMovie_ManyActresses(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		Actresses:   make([]models.Actress, 10),
	}

	// Fill with test actresses
	for i := 0; i < 10; i++ {
		movie.Actresses[i] = models.Actress{
			FirstName: "Actress",
			LastName:  string(rune('A' + i)),
		}
	}

	assert.NotPanics(t, func() {
		printMovie(movie, nil)
	})
}

func TestPrintMovie_ManyGenres(t *testing.T) {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:          "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		Genres:      make([]models.Genre, 15),
	}

	// Fill with test genres
	for i := 0; i < 15; i++ {
		movie.Genres[i] = models.Genre{
			Name: "Genre" + string(rune('A'+i)),
		}
	}

	assert.NotPanics(t, func() {
		printMovie(movie, nil)
	})
}

func TestLoadConfig_InvalidFile(t *testing.T) {
	// Save original config file
	originalCfgFile := cfgFile

	// Create temp dir for test
	tmpDir := t.TempDir()

	// Set config file to non-existent path
	cfgFile = filepath.Join(tmpDir, "nonexistent.yaml")

	// Reset after test
	defer func() {
		cfgFile = originalCfgFile
		cfg = nil
	}()

	err := loadConfig()

	// loadConfig uses LoadOrCreate which creates a default config if missing
	// So we expect no error
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestLoadConfig_ValidFile(t *testing.T) {
	// Save original config file
	originalCfgFile := cfgFile

	// Create temp dir for test
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	validConfig := `
server:
  host: "localhost"
  port: 8080

database:
  type: "sqlite"
  dsn: "data/test.db"

scrapers:
  user_agent: "Javinizer/Test"
  priority:
    - "r18dev"
    - "dmm"
  proxy:
    enabled: false
  r18dev:
    enabled: true
  dmm:
    enabled: true
    scrape_actress: true

output:
  folder_format: "<ID> - <TITLE>"
  file_format: "<ID>"
  download_cover: true
  download_extrafanart: false
  download_proxy:
    enabled: false

logging:
  level: "info"
  format: "text"
  output: "stdout"
`

	require.NoError(t, os.WriteFile(configPath, []byte(validConfig), 0644))

	// Set config file
	cfgFile = configPath

	// Reset after test
	defer func() {
		cfgFile = originalCfgFile
		cfg = nil
	}()

	err := loadConfig()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "data/test.db", cfg.Database.DSN)
	assert.Contains(t, cfg.Scrapers.Priority, "r18dev")
	assert.Contains(t, cfg.Scrapers.Priority, "dmm")
}

func TestLoadConfig_WithProxyEnabled(t *testing.T) {
	originalCfgFile := cfgFile
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configWithProxy := `
server:
  host: "localhost"
  port: 8080

database:
  type: "sqlite"
  dsn: "data/test.db"

scrapers:
  priority:
    - "r18dev"
  proxy:
    enabled: true
    url: "http://proxy.example.com:8080"
    username: "user"
    password: "pass"
  r18dev:
    enabled: true
  dmm:
    enabled: false

output:
  download_proxy:
    enabled: false

logging:
  level: "info"
  format: "text"
  output: "stdout"
`

	require.NoError(t, os.WriteFile(configPath, []byte(configWithProxy), 0644))

	cfgFile = configPath

	defer func() {
		cfgFile = originalCfgFile
		cfg = nil
	}()

	err := loadConfig()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.True(t, cfg.Scrapers.Proxy.Enabled)
	assert.Equal(t, "http://proxy.example.com:8080", cfg.Scrapers.Proxy.URL)
}

func TestLoadConfig_ProxyEnabledButEmptyURL(t *testing.T) {
	originalCfgFile := cfgFile
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configWithBadProxy := `
server:
  host: "localhost"
  port: 8080

database:
  type: "sqlite"
  dsn: "data/test.db"

scrapers:
  priority:
    - "r18dev"
  proxy:
    enabled: true
    url: ""
  r18dev:
    enabled: true
  dmm:
    enabled: false

output:
  download_proxy:
    enabled: false

logging:
  level: "info"
  format: "text"
  output: "stdout"
`

	require.NoError(t, os.WriteFile(configPath, []byte(configWithBadProxy), 0644))

	cfgFile = configPath

	defer func() {
		cfgFile = originalCfgFile
		cfg = nil
	}()

	err := loadConfig()

	// loadConfig should disable proxy if URL is empty
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.False(t, cfg.Scrapers.Proxy.Enabled) // Should be disabled
}

func TestLoadConfig_WithVerboseFlag(t *testing.T) {
	originalCfgFile := cfgFile
	originalVerboseFlag := verboseFlag

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	validConfig := `
server:
  host: "localhost"
  port: 8080

database:
  type: "sqlite"
  dsn: "data/test.db"

scrapers:
  priority:
    - "r18dev"
  proxy:
    enabled: false
  r18dev:
    enabled: true
  dmm:
    enabled: false

output:
  download_proxy:
    enabled: false

logging:
  level: "info"
  format: "text"
  output: "stdout"
`

	require.NoError(t, os.WriteFile(configPath, []byte(validConfig), 0644))

	cfgFile = configPath
	verboseFlag = true

	defer func() {
		cfgFile = originalCfgFile
		verboseFlag = originalVerboseFlag
		cfg = nil
	}()

	err := loadConfig()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	// Logger should be initialized with debug level due to verbose flag
	// We can't easily verify this without exposing logger state, but we verify no error
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	originalCfgFile := cfgFile

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	malformedConfig := `
server:
  host: "localhost"
  port: "not-a-number"  # Invalid - port should be int
`

	require.NoError(t, os.WriteFile(configPath, []byte(malformedConfig), 0644))

	cfgFile = configPath

	defer func() {
		cfgFile = originalCfgFile
		cfg = nil
	}()

	err := loadConfig()

	// Should get error due to malformed YAML
	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "config")
}

// Tests for applyScrapeFlagOverrides

func TestApplyScrapeFlagOverrides_ScrapeActress(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		value    bool
		expected bool
	}{
		{
			name:     "scrape-actress true",
			flagName: "scrape-actress",
			value:    true,
			expected: true,
		},
		{
			name:     "scrape-actress false",
			flagName: "scrape-actress",
			value:    false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						ScrapeActress: false, // Default
					},
				},
			}

			cmd := &cobra.Command{}
			cmd.Flags().Bool(tt.flagName, false, "test flag")
			require.NoError(t, cmd.Flags().Set(tt.flagName, "true"))
			if !tt.value {
				require.NoError(t, cmd.Flags().Set(tt.flagName, "false"))
			}

			applyScrapeFlagOverrides(cmd, cfg)

			assert.Equal(t, tt.expected, cfg.Scrapers.DMM.ScrapeActress)
		})
	}
}

func TestApplyScrapeFlagOverrides_NoScrapeActress(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				ScrapeActress: true, // Start with true
			},
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-scrape-actress", false, "test flag")
	require.NoError(t, cmd.Flags().Set("no-scrape-actress", "true"))

	applyScrapeFlagOverrides(cmd, cfg)

	assert.False(t, cfg.Scrapers.DMM.ScrapeActress)
}

func TestApplyScrapeFlagOverrides_Headless(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		value    bool
		expected bool
	}{
		{
			name:     "headless true",
			flagName: "headless",
			value:    true,
			expected: true,
		},
		{
			name:     "headless false",
			flagName: "headless",
			value:    false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						EnableHeadless: false, // Default
					},
				},
			}

			cmd := &cobra.Command{}
			cmd.Flags().Bool(tt.flagName, false, "test flag")
			require.NoError(t, cmd.Flags().Set(tt.flagName, "true"))
			if !tt.value {
				require.NoError(t, cmd.Flags().Set(tt.flagName, "false"))
			}

			applyScrapeFlagOverrides(cmd, cfg)

			assert.Equal(t, tt.expected, cfg.Scrapers.DMM.EnableHeadless)
		})
	}
}

func TestApplyScrapeFlagOverrides_NoHeadless(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				EnableHeadless: true, // Start with true
			},
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-headless", false, "test flag")
	require.NoError(t, cmd.Flags().Set("no-headless", "true"))

	applyScrapeFlagOverrides(cmd, cfg)

	assert.False(t, cfg.Scrapers.DMM.EnableHeadless)
}

func TestApplyScrapeFlagOverrides_HeadlessTimeout(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		expected int
	}{
		{
			name:     "valid timeout 30",
			value:    30,
			expected: 30,
		},
		{
			name:     "valid timeout 60",
			value:    60,
			expected: 60,
		},
		{
			name:     "zero timeout ignored",
			value:    0,
			expected: 10, // Should keep default
		},
		{
			name:     "negative timeout ignored",
			value:    -5,
			expected: 10, // Should keep default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						HeadlessTimeout: 10, // Default
					},
				},
			}

			cmd := &cobra.Command{}
			cmd.Flags().Int("headless-timeout", 0, "test flag")
			require.NoError(t, cmd.Flags().Set("headless-timeout", fmt.Sprintf("%d", tt.value)))

			applyScrapeFlagOverrides(cmd, cfg)

			assert.Equal(t, tt.expected, cfg.Scrapers.DMM.HeadlessTimeout)
		})
	}
}

func TestApplyScrapeFlagOverrides_ActressDB(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		value    bool
		expected bool
	}{
		{
			name:     "actress-db true",
			flagName: "actress-db",
			value:    true,
			expected: true,
		},
		{
			name:     "actress-db false",
			flagName: "actress-db",
			value:    false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					ActressDatabase: config.ActressDatabaseConfig{
						Enabled: false, // Default
					},
				},
			}

			cmd := &cobra.Command{}
			cmd.Flags().Bool(tt.flagName, false, "test flag")
			require.NoError(t, cmd.Flags().Set(tt.flagName, "true"))
			if !tt.value {
				require.NoError(t, cmd.Flags().Set(tt.flagName, "false"))
			}

			applyScrapeFlagOverrides(cmd, cfg)

			assert.Equal(t, tt.expected, cfg.Metadata.ActressDatabase.Enabled)
		})
	}
}

func TestApplyScrapeFlagOverrides_NoActressDB(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled: true, // Start with true
			},
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-actress-db", false, "test flag")
	require.NoError(t, cmd.Flags().Set("no-actress-db", "true"))

	applyScrapeFlagOverrides(cmd, cfg)

	assert.False(t, cfg.Metadata.ActressDatabase.Enabled)
}

func TestApplyScrapeFlagOverrides_GenreReplacement(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		value    bool
		expected bool
	}{
		{
			name:     "genre-replacement true",
			flagName: "genre-replacement",
			value:    true,
			expected: true,
		},
		{
			name:     "genre-replacement false",
			flagName: "genre-replacement",
			value:    false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					GenreReplacement: config.GenreReplacementConfig{
						Enabled: false, // Default
					},
				},
			}

			cmd := &cobra.Command{}
			cmd.Flags().Bool(tt.flagName, false, "test flag")
			require.NoError(t, cmd.Flags().Set(tt.flagName, "true"))
			if !tt.value {
				require.NoError(t, cmd.Flags().Set(tt.flagName, "false"))
			}

			applyScrapeFlagOverrides(cmd, cfg)

			assert.Equal(t, tt.expected, cfg.Metadata.GenreReplacement.Enabled)
		})
	}
}

func TestApplyScrapeFlagOverrides_NoGenreReplacement(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true, // Start with true
			},
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-genre-replacement", false, "test flag")
	require.NoError(t, cmd.Flags().Set("no-genre-replacement", "true"))

	applyScrapeFlagOverrides(cmd, cfg)

	assert.False(t, cfg.Metadata.GenreReplacement.Enabled)
}

func TestApplyScrapeFlagOverrides_NoFlagsSet(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				ScrapeActress:   false,
				EnableHeadless:  false,
				HeadlessTimeout: 10,
			},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled: false,
			},
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: false,
			},
		},
	}

	// Create a config copy for comparison
	originalScrapeActress := cfg.Scrapers.DMM.ScrapeActress
	originalHeadless := cfg.Scrapers.DMM.EnableHeadless
	originalTimeout := cfg.Scrapers.DMM.HeadlessTimeout
	originalActressDB := cfg.Metadata.ActressDatabase.Enabled
	originalGenreRepl := cfg.Metadata.GenreReplacement.Enabled

	cmd := &cobra.Command{}
	// Define flags but don't set them
	cmd.Flags().Bool("scrape-actress", false, "")
	cmd.Flags().Bool("headless", false, "")
	cmd.Flags().Int("headless-timeout", 0, "")
	cmd.Flags().Bool("actress-db", false, "")
	cmd.Flags().Bool("genre-replacement", false, "")

	applyScrapeFlagOverrides(cmd, cfg)

	// Config should remain unchanged
	assert.Equal(t, originalScrapeActress, cfg.Scrapers.DMM.ScrapeActress)
	assert.Equal(t, originalHeadless, cfg.Scrapers.DMM.EnableHeadless)
	assert.Equal(t, originalTimeout, cfg.Scrapers.DMM.HeadlessTimeout)
	assert.Equal(t, originalActressDB, cfg.Metadata.ActressDatabase.Enabled)
	assert.Equal(t, originalGenreRepl, cfg.Metadata.GenreReplacement.Enabled)
}

// Tests for applyEnvironmentOverrides

func TestApplyEnvironmentOverrides_LogLevel(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "debug level",
			envValue: "debug",
			expected: "debug",
		},
		{
			name:     "info level",
			envValue: "info",
			expected: "info",
		},
		{
			name:     "warn level",
			envValue: "warn",
			expected: "warn",
		},
		{
			name:     "error level",
			envValue: "error",
			expected: "error",
		},
		{
			name:     "uppercase DEBUG",
			envValue: "DEBUG",
			expected: "debug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Logging: config.LoggingConfig{
					Level: "info", // Default
				},
			}

			t.Setenv("LOG_LEVEL", tt.envValue)

			applyEnvironmentOverrides(cfg)

			assert.Equal(t, tt.expected, cfg.Logging.Level)
		})
	}
}

func TestApplyEnvironmentOverrides_Umask(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "umask 0022",
			envValue: "0022",
			expected: "0022",
		},
		{
			name:     "umask 0002",
			envValue: "0002",
			expected: "0002",
		},
		{
			name:     "umask 0077",
			envValue: "0077",
			expected: "0077",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				System: config.SystemConfig{
					Umask: "", // Default
				},
			}

			t.Setenv("UMASK", tt.envValue)

			applyEnvironmentOverrides(cfg)

			assert.Equal(t, tt.expected, cfg.System.Umask)
		})
	}
}

func TestApplyEnvironmentOverrides_JavinizerDB(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "custom db path",
			envValue: "/custom/path/javinizer.db",
			expected: "/custom/path/javinizer.db",
		},
		{
			name:     "relative db path",
			envValue: "data/custom.db",
			expected: "data/custom.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Database: config.DatabaseConfig{
					DSN: "data/javinizer.db", // Default
				},
			}

			t.Setenv("JAVINIZER_DB", tt.envValue)

			applyEnvironmentOverrides(cfg)

			assert.Equal(t, tt.expected, cfg.Database.DSN)
		})
	}
}

func TestApplyEnvironmentOverrides_JavinizerLogDir(t *testing.T) {
	tests := []struct {
		name           string
		originalOutput string
		envValue       string
		expected       string
	}{
		{
			name:           "single file output",
			originalOutput: "logs/app.log",
			envValue:       "/var/log",
			expected:       "/var/log/app.log",
		},
		{
			name:           "stdout preserved",
			originalOutput: "stdout",
			envValue:       "/var/log",
			expected:       "stdout",
		},
		{
			name:           "stderr preserved",
			originalOutput: "stderr",
			envValue:       "/var/log",
			expected:       "stderr",
		},
		{
			name:           "multiple outputs with stdout",
			originalOutput: "stdout,logs/app.log",
			envValue:       "/var/log",
			expected:       "stdout,/var/log/app.log",
		},
		{
			name:           "multiple file outputs",
			originalOutput: "logs/app.log,logs/error.log",
			envValue:       "/custom/logs",
			expected:       "/custom/logs/app.log,/custom/logs/error.log",
		},
		{
			name:           "mixed outputs",
			originalOutput: "stdout,logs/app.log,stderr",
			envValue:       "/var/log",
			expected:       "stdout,/var/log/app.log,stderr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Logging: config.LoggingConfig{
					Output: tt.originalOutput,
				},
			}

			t.Setenv("JAVINIZER_LOG_DIR", tt.envValue)

			applyEnvironmentOverrides(cfg)

			assert.Equal(t, tt.expected, cfg.Logging.Output)
		})
	}
}

func TestApplyEnvironmentOverrides_DockerAutoDetection(t *testing.T) {
	t.Run("media directory exists", func(t *testing.T) {
		// We can't easily test /media detection without mocking os.Stat
		// This test documents the expected behavior
		// In a real Docker environment, /media would exist and be auto-detected
		// The function checks if os.Stat("/media") succeeds and sets AllowedDirectories to ["/media"]
	})

	t.Run("allowed directories already set", func(t *testing.T) {
		cfg := &config.Config{
			API: config.APIConfig{
				Security: config.SecurityConfig{
					AllowedDirectories: []string{"/custom/path"},
				},
			},
		}

		originalDirs := cfg.API.Security.AllowedDirectories

		applyEnvironmentOverrides(cfg)

		// Should not override existing allowed directories
		assert.Equal(t, originalDirs, cfg.API.Security.AllowedDirectories)
	})
}

func TestApplyEnvironmentOverrides_NoEnvironmentVariables(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level:  "info",
			Output: "stdout",
		},
		System: config.SystemConfig{
			Umask: "0022",
		},
		Database: config.DatabaseConfig{
			DSN: "data/javinizer.db",
		},
	}

	originalLogLevel := cfg.Logging.Level
	originalOutput := cfg.Logging.Output
	originalUmask := cfg.System.Umask
	originalDSN := cfg.Database.DSN

	// Don't set any environment variables
	applyEnvironmentOverrides(cfg)

	// Config should remain unchanged
	assert.Equal(t, originalLogLevel, cfg.Logging.Level)
	assert.Equal(t, originalOutput, cfg.Logging.Output)
	assert.Equal(t, originalUmask, cfg.System.Umask)
	assert.Equal(t, originalDSN, cfg.Database.DSN)
}

func TestApplyEnvironmentOverrides_AllVariables(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level:  "info",
			Output: "stdout,logs/app.log",
		},
		System: config.SystemConfig{
			Umask: "0022",
		},
		Database: config.DatabaseConfig{
			DSN: "data/javinizer.db",
		},
	}

	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("UMASK", "0002")
	t.Setenv("JAVINIZER_DB", "/custom/db/javinizer.db")
	t.Setenv("JAVINIZER_LOG_DIR", "/var/log/javinizer")

	applyEnvironmentOverrides(cfg)

	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "0002", cfg.System.Umask)
	assert.Equal(t, "/custom/db/javinizer.db", cfg.Database.DSN)
	assert.Equal(t, "stdout,/var/log/javinizer/app.log", cfg.Logging.Output)
}

// TestDownloadMediaFiles_Success tests successful media download
func TestDownloadMediaFiles_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test movie and match
	movie := createTestMovie("IPX-123", "Test Movie")
	matches := []matcher.MatchResult{
		{
			ID:   "IPX-123",
			File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	// Create mock downloader with successful results
	mockDownloader := NewMockDownloader([]downloader.DownloadResult{
		{Type: downloader.MediaTypeCover, Downloaded: true, Size: 1024},
		{Type: downloader.MediaTypePoster, Downloaded: true, Size: 512},
	}, nil)

	// Create minimal config for organizer
	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// TestDownloadMediaFiles_DryRun tests dry run mode
func TestDownloadMediaFiles_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	movie.CoverURL = "http://example.com/cover.jpg"
	movie.Screenshots = []string{"http://example.com/shot1.jpg", "http://example.com/shot2.jpg"}

	matches := []matcher.MatchResult{
		{
			ID:   "IPX-123",
			File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	// Mock downloader should not be called in dry run
	mockDownloader := NewMockDownloader(nil, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	// Capture output to verify dry run messages
	stdout, _ := captureOutput(t, func() {
		count, err := downloadMediaFiles(
			movies, matches, mockDownloader, fileOrganizer,
			true, true, false, tmpDir, false, true, // dryRun = true
		)

		require.NoError(t, err)
		assert.Equal(t, 0, count, "dry run should not download files")
	})

	assert.Contains(t, stdout, "would download")
}

// TestDownloadMediaFiles_EmptyMovies tests with no movies
func TestDownloadMediaFiles_EmptyMovies(t *testing.T) {
	tmpDir := t.TempDir()
	mockDownloader := NewMockDownloader(nil, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		map[string]*models.Movie{}, // empty movies
		[]matcher.MatchResult{},
		mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestDownloadMediaFiles_NoMatches tests when no matches exist for movies
func TestDownloadMediaFiles_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	movies := map[string]*models.Movie{"IPX-123": movie}

	// No matches for the movie
	matches := []matcher.MatchResult{}

	mockDownloader := NewMockDownloader(nil, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestDownloadMediaFiles_MoveToFolder tests downloading to organized folder
func TestDownloadMediaFiles_MoveToFolder(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "organized")

	movie := createTestMovie("IPX-123", "Test Movie")
	matches := []matcher.MatchResult{
		{
			ID:   "IPX-123",
			File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	mockDownloader := NewMockDownloader([]downloader.DownloadResult{
		{Type: downloader.MediaTypeCover, Downloaded: true, Size: 1024},
	}, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, true, destDir, false, false, // moveToFolder = true
	)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestDownloadMediaFiles_MultiPart tests multi-part file handling
func TestDownloadMediaFiles_MultiPart(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	matches := []matcher.MatchResult{
		{
			ID:          "IPX-123",
			IsMultiPart: true,
			PartNumber:  1,
			File:        scanner.FileInfo{Dir: tmpDir, Name: "IPX-123-pt1.mp4", Path: filepath.Join(tmpDir, "IPX-123-pt1.mp4")},
		},
		{
			ID:          "IPX-123",
			IsMultiPart: true,
			PartNumber:  2,
			File:        scanner.FileInfo{Dir: tmpDir, Name: "IPX-123-pt2.mp4", Path: filepath.Join(tmpDir, "IPX-123-pt2.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	mockDownloader := NewMockDownloader([]downloader.DownloadResult{
		{Type: downloader.MediaTypeCover, Downloaded: true, Size: 1024},
	}, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestDownloadMediaFiles_DownloadError tests handling of download errors
func TestDownloadMediaFiles_DownloadError(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	matches := []matcher.MatchResult{
		{
			ID:   "IPX-123",
			File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	// Mock downloader that returns error
	mockDownloader := NewMockDownloader(nil, fmt.Errorf("network error"))

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	// Should not return error - errors are logged but not propagated
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestDownloadMediaFiles_SkippedFiles tests handling of skipped files
func TestDownloadMediaFiles_SkippedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	matches := []matcher.MatchResult{
		{
			ID:   "IPX-123",
			File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	// Mock downloader with skipped result (already exists)
	mockDownloader := NewMockDownloader([]downloader.DownloadResult{
		{Type: downloader.MediaTypeCover, Downloaded: false, Size: 0, Error: nil}, // Skipped
	}, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 0, count, "skipped files should not be counted")
}

// TestDownloadMediaFiles_FailedFiles tests handling of failed downloads
func TestDownloadMediaFiles_FailedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	matches := []matcher.MatchResult{
		{
			ID:   "IPX-123",
			File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	// Mock downloader with failed result
	mockDownloader := NewMockDownloader([]downloader.DownloadResult{
		{Type: downloader.MediaTypeCover, Downloaded: false, Error: fmt.Errorf("404 not found")},
	}, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 0, count, "failed files should not be counted")
}

// TestDownloadMediaFiles_MixedResults tests handling of mixed download results
func TestDownloadMediaFiles_MixedResults(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	matches := []matcher.MatchResult{
		{
			ID:   "IPX-123",
			File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")},
		},
	}
	movies := map[string]*models.Movie{"IPX-123": movie}

	// Mock downloader with mixed results: 1 success, 1 skip, 1 failure
	mockDownloader := NewMockDownloader([]downloader.DownloadResult{
		{Type: downloader.MediaTypeCover, Downloaded: true, Size: 1024},
		{Type: downloader.MediaTypePoster, Downloaded: false, Size: 0, Error: nil}, // Skipped
		{Type: downloader.MediaTypeExtrafanart, Downloaded: false, Error: fmt.Errorf("timeout")},
	}, nil)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := downloadMediaFiles(
		movies, matches, mockDownloader, fileOrganizer,
		true, false, false, tmpDir, false, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, count, "only downloaded files should be counted")
}

// TestScrapeMetadata_Success tests successful metadata scraping
func TestScrapeMetadata_Success(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Create test matches
		matches := []matcher.MatchResult{
			{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
		}

		// Setup mock scraper with result
		mockResult := &models.ScraperResult{
			ID:        "IPX-123",
			ContentID: "ipx00123",
			Title:     "Test Movie",
		}
		mockScraper := NewMockScraper("testscraper")
		mockScraper.AddResult("IPX-123", mockResult)

		registry := models.NewScraperRegistry()
		registry.Register(mockScraper)

		agg := aggregator.NewWithDatabase(cfg, deps.DB)
		movieRepo := database.NewMovieRepository(deps.DB)

		movies, scrapedCount, cachedCount, err := scrapeMetadata(
			matches, movieRepo, registry, agg, []string{"testscraper"}, false,
		)

		require.NoError(t, err)
		assert.Equal(t, 1, len(movies))
		assert.Equal(t, 1, scrapedCount)
		assert.Equal(t, 0, cachedCount)
		assert.NotNil(t, movies["IPX-123"], "movie should be in result map")
	})

	_ = dbPath
}

// TestScrapeMetadata_CacheHit tests metadata retrieval from cache
func TestScrapeMetadata_CacheHit(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Pre-populate cache
		movieRepo := database.NewMovieRepository(deps.DB)
		cachedMovie := createTestMovie("IPX-123", "Cached Movie")
		err = movieRepo.Upsert(cachedMovie)
		require.NoError(t, err)

		// Create test matches
		matches := []matcher.MatchResult{
			{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
		}

		registry := models.NewScraperRegistry()
		agg := aggregator.NewWithDatabase(cfg, deps.DB)

		movies, scrapedCount, cachedCount, err := scrapeMetadata(
			matches, movieRepo, registry, agg, []string{}, false, // forceRefresh = false
		)

		require.NoError(t, err)
		assert.Equal(t, 1, len(movies))
		assert.Equal(t, 0, scrapedCount, "should not scrape when cache hit")
		assert.Equal(t, 1, cachedCount, "should return cached movie")
		assert.Equal(t, "Cached Movie", movies["IPX-123"].Title)
	})

	_ = dbPath
}

// TestScrapeMetadata_ForceRefresh tests cache clearing with force refresh
func TestScrapeMetadata_ForceRefresh(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Pre-populate cache
		movieRepo := database.NewMovieRepository(deps.DB)
		cachedMovie := createTestMovie("IPX-123", "Cached Movie")
		err = movieRepo.Upsert(cachedMovie)
		require.NoError(t, err)

		// Create test matches
		matches := []matcher.MatchResult{
			{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
		}

		// Setup mock scraper with fresh result
		mockResult := &models.ScraperResult{
			ID:        "IPX-123",
			ContentID: "ipx00123",
			Title:     "Fresh Movie",
		}
		mockScraper := NewMockScraper("testscraper")
		mockScraper.AddResult("IPX-123", mockResult)

		registry := models.NewScraperRegistry()
		registry.Register(mockScraper)

		agg := aggregator.NewWithDatabase(cfg, deps.DB)

		movies, scrapedCount, cachedCount, err := scrapeMetadata(
			matches, movieRepo, registry, agg, []string{"testscraper"}, true, // forceRefresh = true
		)

		require.NoError(t, err)
		assert.Equal(t, 1, len(movies))
		assert.Equal(t, 1, scrapedCount, "should scrape when force refresh")
		assert.Equal(t, 0, cachedCount, "should not use cache when force refresh")
		assert.NotNil(t, movies["IPX-123"], "movie should be freshly scraped")
	})

	_ = dbPath
}

// TestScrapeMetadata_EmptyMatches tests handling of empty matches
func TestScrapeMetadata_EmptyMatches(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		movieRepo := database.NewMovieRepository(deps.DB)
		registry := models.NewScraperRegistry()
		agg := aggregator.NewWithDatabase(cfg, deps.DB)

		stdout, _ := captureOutput(t, func() {
			movies, scrapedCount, cachedCount, err := scrapeMetadata(
				[]matcher.MatchResult{}, // Empty matches
				movieRepo, registry, agg, []string{}, false,
			)

			require.NoError(t, err)
			assert.Nil(t, movies)
			assert.Equal(t, 0, scrapedCount)
			assert.Equal(t, 0, cachedCount)
		})

		assert.Contains(t, stdout, "No metadata found")
	})

	_ = dbPath
}

// TestScrapeMetadata_NoResults tests when no scrapers return results
func TestScrapeMetadata_NoResults(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Create test matches
		matches := []matcher.MatchResult{
			{ID: "IPX-999", File: scanner.FileInfo{Name: "IPX-999.mp4", Path: "/test/IPX-999.mp4"}},
		}

		// Setup mock scraper that returns error (no results)
		mockScraper := NewMockScraper("testscraper")
		mockScraper.AddError("IPX-999", fmt.Errorf("not found"))

		registry := models.NewScraperRegistry()
		registry.Register(mockScraper)

		agg := aggregator.NewWithDatabase(cfg, deps.DB)
		movieRepo := database.NewMovieRepository(deps.DB)

		stdout, _ := captureOutput(t, func() {
			movies, scrapedCount, cachedCount, err := scrapeMetadata(
				matches, movieRepo, registry, agg, []string{"testscraper"}, false,
			)

			require.NoError(t, err)
			assert.Nil(t, movies)
			assert.Equal(t, 0, scrapedCount)
			assert.Equal(t, 0, cachedCount)
		})

		assert.Contains(t, stdout, "not found")
	})

	_ = dbPath
}

// TestScrapeMetadata_MultipleIDs tests scraping multiple movies
func TestScrapeMetadata_MultipleIDs(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Create test matches for multiple IDs
		matches := []matcher.MatchResult{
			{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
			{ID: "IPX-456", File: scanner.FileInfo{Name: "IPX-456.mp4", Path: "/test/IPX-456.mp4"}},
			{ID: "IPX-789", File: scanner.FileInfo{Name: "IPX-789.mp4", Path: "/test/IPX-789.mp4"}},
		}

		// Setup mock scraper with results for 2 out of 3
		mockScraper := NewMockScraper("testscraper")
		mockScraper.AddResult("IPX-123", &models.ScraperResult{ID: "IPX-123", ContentID: "ipx00123", Title: "Movie 1"})
		mockScraper.AddResult("IPX-456", &models.ScraperResult{ID: "IPX-456", ContentID: "ipx00456", Title: "Movie 2"})
		mockScraper.AddError("IPX-789", fmt.Errorf("not found"))

		registry := models.NewScraperRegistry()
		registry.Register(mockScraper)

		agg := aggregator.NewWithDatabase(cfg, deps.DB)
		movieRepo := database.NewMovieRepository(deps.DB)

		movies, scrapedCount, cachedCount, err := scrapeMetadata(
			matches, movieRepo, registry, agg, []string{"testscraper"}, false,
		)

		require.NoError(t, err)
		assert.Equal(t, 2, len(movies), "should find 2 out of 3")
		assert.Equal(t, 2, scrapedCount)
		assert.Equal(t, 0, cachedCount)
		assert.NotNil(t, movies["IPX-123"])
		assert.NotNil(t, movies["IPX-456"])
		assert.Nil(t, movies["IPX-789"], "failed movie should not be in map")
	})

	_ = dbPath
}

// TestScrapeMetadata_MixedCacheAndScrape tests mix of cached and fresh scrapes
func TestScrapeMetadata_MixedCacheAndScrape(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Pre-populate cache with one movie
		movieRepo := database.NewMovieRepository(deps.DB)
		cachedMovie := createTestMovie("IPX-123", "Cached Movie")
		err = movieRepo.Upsert(cachedMovie)
		require.NoError(t, err)

		// Create test matches for multiple IDs
		matches := []matcher.MatchResult{
			{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
			{ID: "IPX-456", File: scanner.FileInfo{Name: "IPX-456.mp4", Path: "/test/IPX-456.mp4"}},
		}

		// Setup mock scraper for the uncached movie
		mockScraper := NewMockScraper("testscraper")
		mockScraper.AddResult("IPX-456", &models.ScraperResult{ID: "IPX-456", ContentID: "ipx00456", Title: "Fresh Movie"})

		registry := models.NewScraperRegistry()
		registry.Register(mockScraper)

		agg := aggregator.NewWithDatabase(cfg, deps.DB)

		movies, scrapedCount, cachedCount, err := scrapeMetadata(
			matches, movieRepo, registry, agg, []string{"testscraper"}, false,
		)

		require.NoError(t, err)
		assert.Equal(t, 2, len(movies))
		assert.Equal(t, 1, scrapedCount, "one movie scraped fresh")
		assert.Equal(t, 1, cachedCount, "one movie from cache")
		// Verify both movies are present
		assert.NotNil(t, movies["IPX-123"])
		assert.NotNil(t, movies["IPX-456"])
		// Cached movie should preserve its title
		assert.Equal(t, "Cached Movie", movies["IPX-123"].Title)
	})

	_ = dbPath
}

// TestGenerateNFOs_Disabled tests that no NFOs are generated when disabled
func TestGenerateNFOs_Disabled(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	movies := map[string]*models.Movie{"IPX-123": movie}
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")}},
	}

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, nil))
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := generateNFOs(
		movies, matches, nfoGenerator, fileOrganizer,
		false, false, false, tmpDir, false, false, // nfoEnabled = false
	)

	require.NoError(t, err)
	assert.Equal(t, 0, count, "should not generate NFOs when disabled")
}

// TestGenerateNFOs_DryRun tests dry run mode
func TestGenerateNFOs_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	movies := map[string]*models.Movie{"IPX-123": movie}
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")}},
	}

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, nil))
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	stdout, _ := captureOutput(t, func() {
		count, err := generateNFOs(
			movies, matches, nfoGenerator, fileOrganizer,
			true, false, false, tmpDir, false, true, // dryRun = true
		)

		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	assert.Contains(t, stdout, "would generate")
}

// TestGenerateNFOs_Success tests successful NFO generation
func TestGenerateNFOs_Success(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	movies := map[string]*models.Movie{"IPX-123": movie}
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")}},
	}

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, nil))
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	stdout, _ := captureOutput(t, func() {
		count, err := generateNFOs(
			movies, matches, nfoGenerator, fileOrganizer,
			true, false, false, tmpDir, false, false,
		)

		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	assert.Contains(t, stdout, "IPX-123.nfo")
	assert.Contains(t, stdout, "Generated 1 NFO")
}

// TestGenerateNFOs_MultiPart_PerFile tests per-file NFO for multi-part files
func TestGenerateNFOs_MultiPart_PerFile(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	movies := map[string]*models.Movie{"IPX-123": movie}
	matches := []matcher.MatchResult{
		{
			ID:          "IPX-123",
			IsMultiPart: true,
			PartNumber:  1,
			PartSuffix:  "-pt1",
			File:        scanner.FileInfo{Dir: tmpDir, Name: "IPX-123-pt1.mp4", Path: filepath.Join(tmpDir, "IPX-123-pt1.mp4")},
		},
		{
			ID:          "IPX-123",
			IsMultiPart: true,
			PartNumber:  2,
			PartSuffix:  "-pt2",
			File:        scanner.FileInfo{Dir: tmpDir, Name: "IPX-123-pt2.mp4", Path: filepath.Join(tmpDir, "IPX-123-pt2.mp4")},
		},
	}

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, nil))
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	stdout, _ := captureOutput(t, func() {
		count, err := generateNFOs(
			movies, matches, nfoGenerator, fileOrganizer,
			true, false, true, tmpDir, false, false, // perFileNFO = true
		)

		require.NoError(t, err)
		assert.Equal(t, 2, count, "should generate NFO for each part")
	})

	assert.Contains(t, stdout, "IPX-123-pt1.nfo")
	assert.Contains(t, stdout, "IPX-123-pt2.nfo")
	assert.Contains(t, stdout, "Generated 2 NFO")
}

// TestGenerateNFOs_MultiPart_Single tests single NFO for multi-part files
func TestGenerateNFOs_MultiPart_Single(t *testing.T) {
	tmpDir := t.TempDir()

	movie := createTestMovie("IPX-123", "Test Movie")
	movies := map[string]*models.Movie{"IPX-123": movie}
	matches := []matcher.MatchResult{
		{
			ID:          "IPX-123",
			IsMultiPart: true,
			PartNumber:  1,
			File:        scanner.FileInfo{Dir: tmpDir, Name: "IPX-123-pt1.mp4", Path: filepath.Join(tmpDir, "IPX-123-pt1.mp4")},
		},
		{
			ID:          "IPX-123",
			IsMultiPart: true,
			PartNumber:  2,
			File:        scanner.FileInfo{Dir: tmpDir, Name: "IPX-123-pt2.mp4", Path: filepath.Join(tmpDir, "IPX-123-pt2.mp4")},
		},
	}

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, nil))
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	stdout, _ := captureOutput(t, func() {
		count, err := generateNFOs(
			movies, matches, nfoGenerator, fileOrganizer,
			true, false, false, tmpDir, false, false, // perFileNFO = false
		)

		require.NoError(t, err)
		assert.Equal(t, 1, count, "should generate single NFO for multi-part")
	})

	assert.Contains(t, stdout, "IPX-123.nfo")
	assert.NotContains(t, stdout, "-pt1")
	assert.NotContains(t, stdout, "-pt2")
}

// TestGenerateNFOs_MoveToFolder tests NFO generation to organized folder
func TestGenerateNFOs_MoveToFolder(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "organized")

	movie := createTestMovie("IPX-123", "Test Movie")
	movies := map[string]*models.Movie{"IPX-123": movie}
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")}},
	}

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, nil))
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	count, err := generateNFOs(
		movies, matches, nfoGenerator, fileOrganizer,
		true, true, false, destDir, false, false, // moveToFolder = true
	)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestGenerateNFOs_MultipleMovies tests NFO generation for multiple movies
func TestGenerateNFOs_MultipleMovies(t *testing.T) {
	tmpDir := t.TempDir()

	movies := map[string]*models.Movie{
		"IPX-123": createTestMovie("IPX-123", "Movie 1"),
		"IPX-456": createTestMovie("IPX-456", "Movie 2"),
		"IPX-789": createTestMovie("IPX-789", "Movie 3"),
	}
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-123.mp4", Path: filepath.Join(tmpDir, "IPX-123.mp4")}},
		{ID: "IPX-456", File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-456.mp4", Path: filepath.Join(tmpDir, "IPX-456.mp4")}},
		{ID: "IPX-789", File: scanner.FileInfo{Dir: tmpDir, Name: "IPX-789.mp4", Path: filepath.Join(tmpDir, "IPX-789.mp4")}},
	}

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
	}
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, nil))
	fileOrganizer := organizer.NewOrganizer(&cfg.Output)

	stdout, _ := captureOutput(t, func() {
		count, err := generateNFOs(
			movies, matches, nfoGenerator, fileOrganizer,
			true, false, false, tmpDir, false, false,
		)

		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	assert.Contains(t, stdout, "IPX-123.nfo")
	assert.Contains(t, stdout, "IPX-456.nfo")
	assert.Contains(t, stdout, "IPX-789.nfo")
	assert.Contains(t, stdout, "Generated 3 NFO")
}
