package bridge_test

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

// TestConfigBridgeRoundTrips verifies that each ConfigFromAppConfig bridge
// correctly extracts the relevant fields from *config.Config. Per ADR-0045:
// these tests catch drift when a new config field is added but the bridge
// isn't updated. The test sets specific values on the monolith config,
// runs the bridge, and checks that the narrow config has those values.

func TestMatcherConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = `^[A-Z]+-\d+`

	result := matcher.ConfigFromAppConfig(cfg)
	require.NotNil(t, result)
	assert.Equal(t, true, result.RegexEnabled)
	assert.Equal(t, `^[A-Z]+-\d+`, result.RegexPattern)
}

func TestScannerConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Matching.Extensions = []string{".mp4", ".mkv"}
	cfg.Matching.MinSizeMB = 100
	cfg.Matching.ExcludePatterns = []string{"*.tmp"}

	result := scanner.ConfigFromAppConfig(cfg)
	require.NotNil(t, result)
	assert.Equal(t, []string{".mp4", ".mkv"}, result.Extensions)
	assert.Equal(t, 100, result.MinSizeMB)
	assert.Equal(t, []string{"*.tmp"}, result.ExcludePatterns)
}

func TestOrganizerConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Output.Template.FolderFormat = "{id}"
	cfg.Output.Template.FileFormat = "{id}-{title}"
	cfg.Output.Operation.AllowRevert = true

	result := organizer.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	require.NotNil(t, result)
	assert.Equal(t, "{id}", result.FolderFormat)
	assert.Equal(t, "{id}-{title}", result.FileFormat)
	assert.Equal(t, true, result.AllowRevert)
}

func TestNFOConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Metadata.NFO.Feature.Enabled = true
	cfg.Metadata.NFO.Feature.PerFile = true

	result := nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	require.NotNil(t, result)
	assert.Equal(t, true, result.PerFile)
}

func TestDownloaderConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Output.Download.DownloadPoster = true
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadTimeout = 120

	result := downloader.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	require.NotNil(t, result)
	assert.Equal(t, true, result.DownloadPoster)
	assert.Equal(t, false, result.DownloadCover)
	assert.Equal(t, 120, result.DownloadTimeout)
}

func TestScrapeConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Scrapers.UserAgent = "test-agent"
	cfg.Scrapers.Priority = []string{"r18dev", "dmm"}

	result := scrape.ConfigFromAppConfig(cfg)
	require.NotNil(t, result)
	assert.Equal(t, "test-agent", result.UserAgent)
	assert.Equal(t, []string{"r18dev", "dmm"}, result.ScrapersPriority)
}

func TestAggregatorConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	cfg.Scrapers.Priority = []string{"r18dev", "dmm"}

	result := aggregator.ConfigFromAppConfig(cfg)
	require.NotNil(t, result)
	assert.Equal(t, []string{"r18dev", "dmm"}, result.ScrapersPriority)
}

func TestConfigBridgeNilConfig(t *testing.T) {
	// All bridges should handle nil config gracefully
	assert.Nil(t, matcher.ConfigFromAppConfig(nil))
	assert.Nil(t, scanner.ConfigFromAppConfig(nil))
	assert.Nil(t, organizer.ConfigFromAppConfig(nil, nfo.NFONameConfig{}))
	assert.Nil(t, nfo.ConfigFromAppConfig(nil, nfo.NFONameConfig{}))
	assert.Nil(t, downloader.ConfigFromAppConfig(nil, nfo.NFONameConfig{}))
	assert.Nil(t, scrape.ConfigFromAppConfig(nil))
	assert.Nil(t, aggregator.ConfigFromAppConfig(nil))
}
