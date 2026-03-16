package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveFileWriteError tests error handling when file write fails
func TestSaveFileWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping chmod-based permission test on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("Skipping write permission test when running as root")
	}

	tmpDir := t.TempDir()

	// Create a directory that will prevent file creation
	dirPath := filepath.Join(tmpDir, "readonly")
	err := os.Mkdir(dirPath, 0755)
	require.NoError(t, err)

	cfgPath := filepath.Join(dirPath, "config.yaml")

	// Create the file first
	err = os.WriteFile(cfgPath, []byte("test"), 0644)
	require.NoError(t, err)

	// Make the directory read-only
	err = os.Chmod(dirPath, 0444)
	require.NoError(t, err)

	// Restore permissions in cleanup
	defer func() { _ = os.Chmod(dirPath, 0755) }()

	cfg := DefaultConfig()
	err = Save(cfg, cfgPath)

	// Should fail due to write permissions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write config file")
}

// TestLoadOrCreateSaveError tests error handling when Save() fails in LoadOrCreate
func TestLoadOrCreateSaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping chmod-based permission test on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "subdir", "config.yaml")

	// Make the parent directory read-only to prevent subdirectory creation
	err := os.Chmod(tmpDir, 0555)
	require.NoError(t, err)

	// Restore permissions in cleanup
	defer func() { _ = os.Chmod(tmpDir, 0755) }()

	// Should fail due to permission error (either on read attempt or save attempt)
	_, err = LoadOrCreate(cfgPath)
	assert.Error(t, err, "Expected LoadOrCreate to fail with permission denied")
}

// TestLoadReadError tests error handling for read errors (non-ENOENT)
func TestLoadReadError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory with the config file name to trigger read error
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	err := os.Mkdir(cfgPath, 0755)
	require.NoError(t, err)

	// Trying to read a directory as a file should fail
	_, err = Load(cfgPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestAPISecurityConfig tests API security configuration loading
func TestAPISecurityConfig(t *testing.T) {
	yamlContent := `
api:
  security:
    allowed_directories:
      - /home/user/videos
      - /media/storage
    denied_directories:
      - /tmp
      - /var
    max_files_per_scan: 5000
    scan_timeout_seconds: 60
    allowed_origins:
      - "http://localhost:3000"
      - "https://app.example.com"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "api_security.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	// Verify API security config
	assert.Len(t, cfg.API.Security.AllowedDirectories, 2)
	assert.Contains(t, cfg.API.Security.AllowedDirectories, "/home/user/videos")
	assert.Contains(t, cfg.API.Security.AllowedDirectories, "/media/storage")

	assert.Len(t, cfg.API.Security.DeniedDirectories, 2)
	assert.Contains(t, cfg.API.Security.DeniedDirectories, "/tmp")
	assert.Contains(t, cfg.API.Security.DeniedDirectories, "/var")

	assert.Equal(t, 5000, cfg.API.Security.MaxFilesPerScan)
	assert.Equal(t, 60, cfg.API.Security.ScanTimeoutSeconds)

	assert.Len(t, cfg.API.Security.AllowedOrigins, 2)
	assert.Contains(t, cfg.API.Security.AllowedOrigins, "http://localhost:3000")
	assert.Contains(t, cfg.API.Security.AllowedOrigins, "https://app.example.com")
}

// TestAPISecurityDefaults tests default API security configuration
func TestAPISecurityDefaults(t *testing.T) {
	cfg := DefaultConfig()

	// Verify default API security settings
	assert.Empty(t, cfg.API.Security.AllowedDirectories)
	assert.Empty(t, cfg.API.Security.DeniedDirectories)
	assert.Equal(t, 10000, cfg.API.Security.MaxFilesPerScan)
	assert.Equal(t, 30, cfg.API.Security.ScanTimeoutSeconds)
	assert.Empty(t, cfg.API.Security.AllowedOrigins)
}

// TestNFOConfigExtended tests extended NFO configuration options
func TestNFOConfigExtended(t *testing.T) {
	yamlContent := `
metadata:
  nfo:
    enabled: true
    display_name: <ORIGINAL_TITLE>
    filename_template: <ID>-<YEAR>.nfo
    first_name_order: false
    actress_language_ja: true
    per_file: true
    unknown_actress_text: N/A
    actress_as_tag: true
    add_generic_role: true
    alt_name_role: true
    include_originalpath: true
    include_stream_details: true
    include_fanart: true
    include_trailer: true
    rating_source: imdb
    tag:
      - JAV
      - Asian
    tagline: Amazing content
    credits:
      - Scraped by Javinizer
      - https://github.com/javinizer/Javinizer
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nfo_extended.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	nfo := cfg.Metadata.NFO
	assert.True(t, nfo.Enabled)
	assert.Equal(t, "<ORIGINAL_TITLE>", nfo.DisplayName)
	assert.Equal(t, "<ID>-<YEAR>.nfo", nfo.FilenameTemplate)
	assert.False(t, nfo.FirstNameOrder)
	assert.True(t, nfo.ActressLanguageJA)
	assert.True(t, nfo.PerFile)
	assert.Equal(t, "N/A", nfo.UnknownActressText)
	assert.True(t, nfo.ActressAsTag)
	assert.True(t, nfo.AddGenericRole)
	assert.True(t, nfo.AltNameRole)
	assert.True(t, nfo.IncludeOriginalPath)
	assert.True(t, nfo.IncludeStreamDetails)
	assert.True(t, nfo.IncludeFanart)
	assert.True(t, nfo.IncludeTrailer)
	assert.Equal(t, "imdb", nfo.RatingSource)
	assert.Len(t, nfo.Tag, 2)
	assert.Contains(t, nfo.Tag, "JAV")
	assert.Contains(t, nfo.Tag, "Asian")
	assert.Equal(t, "Amazing content", nfo.Tagline)
	assert.Len(t, nfo.Credits, 2)
	assert.Contains(t, nfo.Credits, "Scraped by Javinizer")
}

// TestMetadataDatabaseConfigs tests all metadata database configurations
func TestMetadataDatabaseConfigs(t *testing.T) {
	yamlContent := `
metadata:
  actress_database:
    enabled: true
    auto_add: false
    convert_alias: true
  genre_replacement:
    enabled: false
    auto_add: false
  tag_database:
    enabled: true
  ignore_genres:
    - Censored
    - "High Definition"
  required_fields:
    - title
    - actress
    - release_date
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "metadata_db.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	// Actress database
	assert.True(t, cfg.Metadata.ActressDatabase.Enabled)
	assert.False(t, cfg.Metadata.ActressDatabase.AutoAdd)
	assert.True(t, cfg.Metadata.ActressDatabase.ConvertAlias)

	// Genre replacement
	assert.False(t, cfg.Metadata.GenreReplacement.Enabled)
	assert.False(t, cfg.Metadata.GenreReplacement.AutoAdd)

	// Tag database
	assert.True(t, cfg.Metadata.TagDatabase.Enabled)

	// Ignore genres
	assert.Len(t, cfg.Metadata.IgnoreGenres, 2)
	assert.Contains(t, cfg.Metadata.IgnoreGenres, "Censored")
	assert.Contains(t, cfg.Metadata.IgnoreGenres, "High Definition")

	// Required fields
	assert.Len(t, cfg.Metadata.RequiredFields, 3)
	assert.Contains(t, cfg.Metadata.RequiredFields, "title")
	assert.Contains(t, cfg.Metadata.RequiredFields, "actress")
	assert.Contains(t, cfg.Metadata.RequiredFields, "release_date")
}

// TestOutputConfigExtended tests extended output configuration
func TestOutputConfigExtended(t *testing.T) {
	yamlContent := `
output:
  folder_format: "<ID> - <TITLE>"
  file_format: "<ID> [<STUDIO>]"
  subfolder_format:
    - <YEAR>
    - <MAKER>
  delimiter: " | "
  max_title_length: 150
  max_path_length: 200
  move_subtitles: true
  subtitle_extensions:
    - .srt
    - .vtt
    - .sub
  rename_folder_in_place: true
  move_to_folder: false
  rename_file: false
  group_actress: true
  poster_format: "poster-<ID>.jpg"
  fanart_format: "fanart-<ID>.jpg"
  trailer_format: "trailer-<ID>.mp4"
  screenshot_format: "screenshot"
  screenshot_folder: "screenshots"
  screenshot_padding: 2
  actress_folder: "actresses"
  download_cover: false
  download_poster: false
  download_extrafanart: true
  download_trailer: true
  download_actress: true
  download_timeout: 120
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "output_extended.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	output := cfg.Output
	assert.Equal(t, "<ID> - <TITLE>", output.FolderFormat)
	assert.Equal(t, "<ID> [<STUDIO>]", output.FileFormat)
	assert.Len(t, output.SubfolderFormat, 2)
	assert.Contains(t, output.SubfolderFormat, "<YEAR>")
	assert.Contains(t, output.SubfolderFormat, "<MAKER>")
	assert.Equal(t, " | ", output.Delimiter)
	assert.Equal(t, 150, output.MaxTitleLength)
	assert.Equal(t, 200, output.MaxPathLength)
	assert.True(t, output.MoveSubtitles)
	assert.Len(t, output.SubtitleExtensions, 3)
	assert.True(t, output.RenameFolderInPlace)
	assert.False(t, output.MoveToFolder)
	assert.False(t, output.RenameFile)
	assert.True(t, output.GroupActress)
	assert.Equal(t, "poster-<ID>.jpg", output.PosterFormat)
	assert.Equal(t, "fanart-<ID>.jpg", output.FanartFormat)
	assert.Equal(t, "trailer-<ID>.mp4", output.TrailerFormat)
	assert.Equal(t, "screenshot", output.ScreenshotFormat)
	assert.Equal(t, "screenshots", output.ScreenshotFolder)
	assert.Equal(t, 2, output.ScreenshotPadding)
	assert.Equal(t, "actresses", output.ActressFolder)
	assert.False(t, output.DownloadCover)
	assert.False(t, output.DownloadPoster)
	assert.True(t, output.DownloadExtrafanart)
	assert.True(t, output.DownloadTrailer)
	assert.True(t, output.DownloadActress)
	assert.Equal(t, 120, output.DownloadTimeout)
}

// TestMatchingConfigExtended tests extended matching configuration
func TestMatchingConfigExtended(t *testing.T) {
	yamlContent := `
file_matching:
  extensions:
    - .mp4
    - .avi
    - .mov
  min_size_mb: 100
  exclude_patterns:
    - "*-trailer*"
    - "*-sample*"
    - "*-preview*"
  regex_enabled: true
  regex_pattern: "([A-Z]+-\\d+)"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "matching_extended.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	matching := cfg.Matching
	assert.Len(t, matching.Extensions, 3)
	assert.Contains(t, matching.Extensions, ".mp4")
	assert.Contains(t, matching.Extensions, ".avi")
	assert.Contains(t, matching.Extensions, ".mov")
	assert.Equal(t, 100, matching.MinSizeMB)
	assert.Len(t, matching.ExcludePatterns, 3)
	assert.Contains(t, matching.ExcludePatterns, "*-trailer*")
	assert.Contains(t, matching.ExcludePatterns, "*-sample*")
	assert.Contains(t, matching.ExcludePatterns, "*-preview*")
	assert.True(t, matching.RegexEnabled)
	assert.Equal(t, "([A-Z]+-\\d+)", matching.RegexPattern)
}

// TestDatabaseConfig tests database configuration
func TestDatabaseConfig(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		checkFunc   func(*testing.T, *Config)
	}{
		{
			name: "sqlite database",
			yamlContent: `
database:
  type: sqlite
  dsn: "data/custom.db"
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "sqlite", cfg.Database.Type)
				assert.Equal(t, "data/custom.db", cfg.Database.DSN)
			},
		},
		{
			name: "postgres database",
			yamlContent: `
database:
  type: postgres
  dsn: "host=localhost user=javinizer password=secret dbname=jav"
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "postgres", cfg.Database.Type)
				assert.Contains(t, cfg.Database.DSN, "host=localhost")
				assert.Contains(t, cfg.Database.DSN, "password=secret")
			},
		},
		{
			name: "mysql database",
			yamlContent: `
database:
  type: mysql
  dsn: "user:password@tcp(localhost:3306)/jav?charset=utf8mb4"
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "mysql", cfg.Database.Type)
				assert.Contains(t, cfg.Database.DSN, "tcp(localhost:3306)")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "database.yaml")

			err := os.WriteFile(cfgPath, []byte(tt.yamlContent), 0644)
			require.NoError(t, err)

			cfg, err := Load(cfgPath)
			require.NoError(t, err)

			tt.checkFunc(t, cfg)
		})
	}
}

// TestLoggingConfig tests logging configuration
func TestLoggingConfig(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		checkFunc   func(*testing.T, *Config)
	}{
		{
			name: "debug logging to file",
			yamlContent: `
logging:
  level: debug
  format: json
  output: "/var/log/javinizer.log"
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "debug", cfg.Logging.Level)
				assert.Equal(t, "json", cfg.Logging.Format)
				assert.Equal(t, "/var/log/javinizer.log", cfg.Logging.Output)
			},
		},
		{
			name: "warn logging to stdout",
			yamlContent: `
logging:
  level: warn
  format: text
  output: stdout
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "warn", cfg.Logging.Level)
				assert.Equal(t, "text", cfg.Logging.Format)
				assert.Equal(t, "stdout", cfg.Logging.Output)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "logging.yaml")

			err := os.WriteFile(cfgPath, []byte(tt.yamlContent), 0644)
			require.NoError(t, err)

			cfg, err := Load(cfgPath)
			require.NoError(t, err)

			tt.checkFunc(t, cfg)
		})
	}
}

// TestPerformanceConfig tests performance configuration
func TestPerformanceConfig(t *testing.T) {
	yamlContent := `
performance:
  max_workers: 10
  worker_timeout: 600
  buffer_size: 200
  update_interval: 50
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "performance.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, 10, cfg.Performance.MaxWorkers)
	assert.Equal(t, 600, cfg.Performance.WorkerTimeout)
	assert.Equal(t, 200, cfg.Performance.BufferSize)
	assert.Equal(t, 50, cfg.Performance.UpdateInterval)
}

// TestScraperConfigExtended tests extended scraper configuration
func TestScraperConfigExtended(t *testing.T) {
	yamlContent := `
scrapers:
  user_agent: "Custom User Agent/1.0"
  priority:
    - r18dev
    - dmm
    - jav321
  r18dev:
    enabled: false
  dmm:
    enabled: true
    scrape_actress: true
    enable_browser: false
    browser_timeout: 60
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "scraper_extended.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, "Custom User Agent/1.0", cfg.Scrapers.UserAgent)
	assert.Len(t, cfg.Scrapers.Priority, 3)
	assert.Contains(t, cfg.Scrapers.Priority, "jav321")
	assert.False(t, cfg.Scrapers.R18Dev.Enabled)
	assert.True(t, cfg.Scrapers.DMM.Enabled)
	assert.True(t, cfg.Scrapers.DMM.ScrapeActress)
	assert.False(t, cfg.Scrapers.DMM.EnableBrowser)
	assert.Equal(t, 60, cfg.Scrapers.DMM.BrowserTimeout)
}

// TestAllPriorityFields tests that all PriorityConfig fields can be loaded
func TestAllPriorityFields(t *testing.T) {
	yamlContent := `
scrapers:
  priority:
    - r18dev
    - dmm
metadata:
  priority:
    actress: [dmm, r18dev]
    original_title: [r18dev]
    cover_url: []
    description: [dmm]
    director: [r18dev, dmm]
    genre: [dmm, r18dev]
    id: [r18dev]
    content_id: [dmm]
    label: [r18dev]
    maker: [dmm]
    poster_url: []
    rating: [dmm]
    release_date: [r18dev]
    runtime: [dmm]
    series: [r18dev]
    screenshot_url: [dmm]
    title: []
    trailer_url: [r18dev]
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "all_priorities.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	priority := cfg.Metadata.Priority

	// Test explicit priorities
	assert.Equal(t, []string{"dmm", "r18dev"}, priority.Actress)
	assert.Equal(t, []string{"r18dev"}, priority.OriginalTitle)
	assert.Equal(t, []string{"dmm"}, priority.Description)
	assert.Equal(t, []string{"r18dev", "dmm"}, priority.Director)
	assert.Equal(t, []string{"dmm", "r18dev"}, priority.Genre)
	assert.Equal(t, []string{"r18dev"}, priority.ID)
	assert.Equal(t, []string{"dmm"}, priority.ContentID)
	assert.Equal(t, []string{"r18dev"}, priority.Label)
	assert.Equal(t, []string{"dmm"}, priority.Maker)
	assert.Equal(t, []string{"dmm"}, priority.Rating)
	assert.Equal(t, []string{"r18dev"}, priority.ReleaseDate)
	assert.Equal(t, []string{"dmm"}, priority.Runtime)
	assert.Equal(t, []string{"r18dev"}, priority.Series)
	assert.Equal(t, []string{"dmm"}, priority.ScreenshotURL)
	assert.Equal(t, []string{"r18dev"}, priority.TrailerURL)

	// Test empty arrays (should be preserved as empty, not filled with defaults)
	assert.Empty(t, priority.CoverURL)
	assert.Empty(t, priority.PosterURL)
	assert.Empty(t, priority.Title)
}
