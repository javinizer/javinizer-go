package config_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configFromAppConfigRoundTrip verifies that a value set in *config.Config
// is faithfully propagated through the ConfigFromAppConfig bridge to the
// downstream config struct. These tests catch missing field mappings.
//
// Per ADR-0036: round-trip tests provide test-time guarantees that bridge
// functions stay in sync with the source config.

func TestScannerConfigFromAppConfig_RoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Matching.MinSizeMB = 42
	cfg.Matching.Extensions = []string{".mkv", ".mp4"}
	cfg.Matching.ExcludePatterns = []string{"sample"}

	got := scanner.ConfigFromAppConfig(cfg)
	require.NotNil(t, got)
	assert.Equal(t, 42, got.MinSizeMB)
	assert.Equal(t, []string{".mkv", ".mp4"}, got.Extensions)
	assert.Equal(t, []string{"sample"}, got.ExcludePatterns)
}

func TestScannerConfigFromAppConfig_NilConfig(t *testing.T) {
	got := scanner.ConfigFromAppConfig(nil)
	assert.Nil(t, got)
}

func TestMatcherConfigFromAppConfig_RoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = `^([A-Z]+-\d+)`

	got := matcher.ConfigFromAppConfig(cfg)
	require.NotNil(t, got)
	assert.True(t, got.RegexEnabled)
	assert.Equal(t, `^([A-Z]+-\d+)`, got.RegexPattern)
}

func TestMatcherConfigFromAppConfig_NilConfig(t *testing.T) {
	got := matcher.ConfigFromAppConfig(nil)
	assert.Nil(t, got)
}

func TestOrganizerConfigFromAppConfig_RoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	// Set recognizable values in the source config
	cfg.Output.Template.FolderFormat = "<MAKER>/<ID>"
	cfg.Output.Template.FileFormat = "<ID>-<TITLE>"
	cfg.Output.Operation.AllowRevert = true

	got := organizer.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	require.NotNil(t, got)
	assert.Equal(t, "<MAKER>/<ID>", got.FolderFormat)
	assert.Equal(t, "<ID>-<TITLE>", got.FileFormat)
	assert.True(t, got.AllowRevert)
}

func TestOrganizerConfigFromAppConfig_NilConfig(t *testing.T) {
	got := organizer.ConfigFromAppConfig(nil, nfo.NFONameConfig{})
	assert.Nil(t, got)
}

func TestNFOConfigFromAppConfig_RoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Metadata.NFO.Feature.Enabled = true
	cfg.Metadata.NFO.Format.FirstNameOrder = true
	cfg.Metadata.NFO.Format.FilenameTemplate = "<ID>-custom.nfo"

	got := nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	require.NotNil(t, got)
	assert.Equal(t, cfg.Metadata.NFO.Feature.PerFile, got.PerFile)
	assert.True(t, got.FirstNameOrder)
	assert.Equal(t, "<ID>-custom.nfo", got.FilenameTemplate)
}

func TestNFOConfigFromAppConfig_NilConfig(t *testing.T) {
	got := nfo.ConfigFromAppConfig(nil, nfo.NFONameConfig{})
	assert.Nil(t, got)
}

func TestDownloaderConfigFromAppConfig_RoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Output.MediaFormat.PosterFormat = "<ID>-poster-custom.jpg"
	cfg.Output.Download.DownloadTimeout = 120

	got := downloader.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	require.NotNil(t, got)
	assert.Equal(t, "<ID>-poster-custom.jpg", got.PosterFormat)
	assert.Equal(t, 120, got.DownloadTimeout)
}

func TestDownloaderConfigFromAppConfig_NilConfig(t *testing.T) {
	got := downloader.ConfigFromAppConfig(nil, nfo.NFONameConfig{})
	assert.Nil(t, got)
}

func TestScrapeConfigFromAppConfig_RoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Scrapers.Priority = []string{"r18dev", "dmm"}
	cfg.Scrapers.UserAgent = "TestAgent/1.0"

	got := scrape.ConfigFromAppConfig(cfg)
	require.NotNil(t, got)
	assert.Equal(t, []string{"r18dev", "dmm"}, got.ScrapersPriority)
	assert.Equal(t, "TestAgent/1.0", got.UserAgent)
}

func TestScrapeConfigFromAppConfig_NilConfig(t *testing.T) {
	got := scrape.ConfigFromAppConfig(nil)
	assert.Nil(t, got)
}

func TestAggregatorConfigFromAppConfig_RoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	// Aggregator config is deeply nested — verify non-nil and representative fields
	got := aggregator.ConfigFromAppConfig(cfg)
	require.NotNil(t, got)
	assert.NotNil(t, got.Metadata)
}

func TestAggregatorConfigFromAppConfig_NilConfig(t *testing.T) {
	got := aggregator.ConfigFromAppConfig(nil)
	assert.Nil(t, got)
}
