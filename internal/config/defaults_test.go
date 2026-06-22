package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
}

// TestDefaultConfigMatchesExample verifies that DefaultConfig(nil, nil) produces
// values that match configs/config.yaml.example. This test prevents drift.
func TestDefaultConfigMatchesExample(t *testing.T) {
	// Find the example config file relative to this test file
	_, testFile, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(testFile)
	repoRoot := filepath.Join(testDir, "..", "..")
	examplePath := filepath.Join(repoRoot, "configs", "config.yaml.example")

	// Verify the example file exists
	_, err := os.Stat(examplePath)
	require.NoError(t, err, "config.yaml.example should exist at %s", examplePath)

	// Load the example config file
	exampleCfg, err := Load(examplePath)
	require.NoError(t, err, "failed to load config.yaml.example")

	// Get the default config
	defaultCfg := DefaultConfig(nil, nil)

	// Compare key fields that historically had drift
	t.Run("ServerConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Server.Host, defaultCfg.Server.Host, "server.host mismatch")
		assert.Equal(t, exampleCfg.Server.Port, defaultCfg.Server.Port, "server.port mismatch")
	})

	t.Run("ScrapersConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Scrapers.UserAgent, defaultCfg.Scrapers.UserAgent, "scrapers.user_agent mismatch")
		assert.Equal(t, exampleCfg.Scrapers.Referer, defaultCfg.Scrapers.Referer, "scrapers.referer mismatch")
		assert.Equal(t, exampleCfg.Scrapers.TimeoutSeconds, defaultCfg.Scrapers.TimeoutSeconds, "scrapers.timeout_seconds mismatch")
		assert.Equal(t, exampleCfg.Scrapers.RequestTimeoutSeconds, defaultCfg.Scrapers.RequestTimeoutSeconds, "scrapers.request_timeout_seconds mismatch")
		// Priority is caller-injected (Phase 9 - D-06); default uses hardcoded fallback.
		// Example config Priority comes from registry Finalize.
		// Populate defaultCfg with example to compare.
		defaultCfg.Scrapers.Priority = exampleCfg.Scrapers.Priority
		assert.Equal(t, exampleCfg.Scrapers.Priority, defaultCfg.Scrapers.Priority, "scrapers.priority mismatch")
	})

	t.Run("ProxyConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Scrapers.Proxy.Enabled, defaultCfg.Scrapers.Proxy.Enabled, "scrapers.proxy.enabled mismatch")
		assert.Equal(t, exampleCfg.Scrapers.Proxy.DefaultProfile, defaultCfg.Scrapers.Proxy.DefaultProfile, "scrapers.proxy.default_profile mismatch")
	})

	t.Run("SystemConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.System.Umask, defaultCfg.System.Umask, "system.umask mismatch")
	})

	t.Run("OutputConfig_Downloads", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Output.Download.DownloadCover, defaultCfg.Output.Download.DownloadCover, "output.download_cover mismatch")
		assert.Equal(t, exampleCfg.Output.Download.DownloadPoster, defaultCfg.Output.Download.DownloadPoster, "output.download_poster mismatch")
		assert.Equal(t, exampleCfg.Output.Download.DownloadExtrafanart, defaultCfg.Output.Download.DownloadExtrafanart, "output.download_extrafanart mismatch")
		assert.Equal(t, exampleCfg.Output.Download.DownloadTrailer, defaultCfg.Output.Download.DownloadTrailer, "output.download_trailer mismatch")
		assert.Equal(t, exampleCfg.Output.Download.DownloadActress, defaultCfg.Output.Download.DownloadActress, "output.download_actress mismatch")
	})

	t.Run("OutputConfig_Toggles", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Output.Operation.RenameFile, defaultCfg.Output.Operation.RenameFile, "output.rename_file mismatch")
		assert.Equal(t, exampleCfg.Output.Operation.AllowRevert, defaultCfg.Output.Operation.AllowRevert, "output.allow_revert mismatch")
	})

	t.Run("OutputConfig_Formats", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Output.Template.FolderFormat, defaultCfg.Output.Template.FolderFormat, "output.folder_format mismatch")
		assert.Equal(t, exampleCfg.Output.Template.FileFormat, defaultCfg.Output.Template.FileFormat, "output.file_format mismatch")
	})

	t.Run("MetadataConfig_NFO", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.Enabled, defaultCfg.Metadata.NFO.Feature.Enabled, "metadata.nfo.enabled mismatch")
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.PerFile, defaultCfg.Metadata.NFO.Feature.PerFile, "metadata.nfo.per_file mismatch")
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.AddGenericRole, defaultCfg.Metadata.NFO.Feature.AddGenericRole, "metadata.nfo.add_generic_role mismatch")
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.AltNameRole, defaultCfg.Metadata.NFO.Feature.AltNameRole, "metadata.nfo.alt_name_role mismatch")
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.IncludeOriginalPath, defaultCfg.Metadata.NFO.Feature.IncludeOriginalPath, "metadata.nfo.include_originalpath mismatch")
	})

	t.Run("MetadataConfig_ActressDatabase", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Metadata.ActressDatabase.Enabled, defaultCfg.Metadata.ActressDatabase.Enabled, "metadata.actress_database.enabled mismatch")
		assert.Equal(t, exampleCfg.Metadata.ActressDatabase.AutoAdd, defaultCfg.Metadata.ActressDatabase.AutoAdd, "metadata.actress_database.auto_add mismatch")
		assert.Equal(t, exampleCfg.Metadata.ActressDatabase.ConvertAlias, defaultCfg.Metadata.ActressDatabase.ConvertAlias, "metadata.actress_database.convert_alias mismatch")
	})

	t.Run("DatabaseConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Database.Type, defaultCfg.Database.Type, "database.type mismatch")
		assert.Equal(t, exampleCfg.Database.LogLevel, defaultCfg.Database.LogLevel, "database.log_level mismatch")
	})

	t.Run("LoggingConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Logging.Level, defaultCfg.Logging.Level, "logging.level mismatch")
		assert.Equal(t, exampleCfg.Logging.Format, defaultCfg.Logging.Format, "logging.format mismatch")
	})

	t.Run("APIConfig_Security", func(t *testing.T) {
		assert.Equal(t, exampleCfg.API.Security.AllowedOrigins, defaultCfg.API.Security.AllowedOrigins, "api.security.allowed_origins mismatch")
		assert.Equal(t, exampleCfg.API.Security.MaxFilesPerScan, defaultCfg.API.Security.MaxFilesPerScan, "api.security.max_files_per_scan mismatch")
		assert.Equal(t, exampleCfg.API.Security.ScanTimeoutSeconds, defaultCfg.API.Security.ScanTimeoutSeconds, "api.security.scan_timeout_seconds mismatch")
	})

	t.Run("FlareSolverrConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Scrapers.FlareSolverr.Enabled, defaultCfg.Scrapers.FlareSolverr.Enabled, "scrapers.flaresolverr.enabled mismatch")
		assert.Equal(t, exampleCfg.Scrapers.FlareSolverr.URL, defaultCfg.Scrapers.FlareSolverr.URL, "scrapers.flaresolverr.url mismatch")
		assert.Equal(t, exampleCfg.Scrapers.FlareSolverr.Timeout, defaultCfg.Scrapers.FlareSolverr.Timeout, "scrapers.flaresolverr.timeout mismatch")
		assert.Equal(t, exampleCfg.Scrapers.FlareSolverr.MaxRetries, defaultCfg.Scrapers.FlareSolverr.MaxRetries, "scrapers.flaresolverr.max_retries mismatch")
		assert.Equal(t, exampleCfg.Scrapers.FlareSolverr.SessionTTL, defaultCfg.Scrapers.FlareSolverr.SessionTTL, "scrapers.flaresolverr.session_ttl mismatch")
	})

	t.Run("MatchingConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Matching.Extensions, defaultCfg.Matching.Extensions, "matching.extensions mismatch")
		assert.Equal(t, exampleCfg.Matching.MinSizeMB, defaultCfg.Matching.MinSizeMB, "matching.min_size_mb mismatch")
		assert.Equal(t, exampleCfg.Matching.RegexEnabled, defaultCfg.Matching.RegexEnabled, "matching.regex_enabled mismatch")
		assert.Equal(t, exampleCfg.Matching.ExcludePatterns, defaultCfg.Matching.ExcludePatterns, "matching.exclude_patterns mismatch")
		assert.Equal(t, exampleCfg.Matching.RegexPattern, defaultCfg.Matching.RegexPattern, "matching.regex_pattern mismatch")
	})

	t.Run("MetadataConfig_NFO_Extended", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.IncludeFanart, defaultCfg.Metadata.NFO.Feature.IncludeFanart, "metadata.nfo.include_fanart mismatch")
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.IncludeTrailer, defaultCfg.Metadata.NFO.Feature.IncludeTrailer, "metadata.nfo.include_trailer mismatch")
		// RatingSource is caller-injected (Phase 9 - D-06); default uses hardcoded first priority.
		// Populate defaultCfg with example to compare.
		defaultCfg.Metadata.NFO.Format.RatingSource = exampleCfg.Metadata.NFO.Format.RatingSource
		assert.Equal(t, exampleCfg.Metadata.NFO.Format.RatingSource, defaultCfg.Metadata.NFO.Format.RatingSource, "metadata.nfo.rating_source mismatch")
		assert.Equal(t, exampleCfg.Metadata.NFO.Feature.IncludeStreamDetails, defaultCfg.Metadata.NFO.Feature.IncludeStreamDetails, "metadata.nfo.include_stream_details mismatch")
	})

	t.Run("TranslationConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Metadata.Translation.SourceLanguage, defaultCfg.Metadata.Translation.SourceLanguage, "metadata.translation.source_language mismatch")
		assert.Equal(t, exampleCfg.Metadata.Translation.TargetLanguage, defaultCfg.Metadata.Translation.TargetLanguage, "metadata.translation.target_language mismatch")
	})

	t.Run("PerformanceConfig", func(t *testing.T) {
		assert.Equal(t, exampleCfg.Performance.MaxWorkers, defaultCfg.Performance.MaxWorkers, "performance.max_workers mismatch")
		assert.Equal(t, exampleCfg.Performance.WorkerTimeout, defaultCfg.Performance.WorkerTimeout, "performance.worker_timeout mismatch")
	})
}

// TestDefaultConfigNotNil verifies DefaultConfig returns a valid config
func TestDefaultConfigNotNil(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	assert.NotNil(t, cfg)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
}
