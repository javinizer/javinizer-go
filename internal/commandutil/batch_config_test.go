package commandutil

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchJobConfigFromAppConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Performance.MaxWorkers = 4
	cfg.Performance.WorkerTimeout = 30
	cfg.Scrapers.Priority = []string{"r18dev", "dmm"}
	cfg.Metadata.NFO.Feature.Enabled = true

	result := BatchJobConfigFromAppConfig(cfg)
	assert.Equal(t, 4, result.MaxWorkers)
	assert.Equal(t, 30*time.Second, result.WorkerTimeout)
	assert.Equal(t, []string{"r18dev", "dmm"}, result.ScraperPriority)
	assert.True(t, result.NFOEnabled)
}

func TestCLIApplyOptions_ToApplyPhaseConfig(t *testing.T) {
	opts := CLIApplyOptions{
		DryRun:              true,
		MoveFiles:           false,
		LinkMode:            organizer.LinkModeHard,
		ForceUpdate:         true,
		SkipOrganize:        false,
		GenerateNFO:         true,
		Download:            true,
		DownloadExtrafanart: true,
		Destination:         "/test/dest",
		MergeOptions:        workflow.MergeOptions{ForceOverwrite: true},
	}

	cfg := opts.ToApplyPhaseConfig()
	assert.True(t, cfg.DryRun)
	assert.False(t, cfg.OrganizeOptions.MoveFiles)
	assert.Equal(t, organizer.LinkModeHard, cfg.OrganizeOptions.LinkMode)
	assert.True(t, cfg.OrganizeOptions.ForceUpdate)
	assert.True(t, cfg.GenerateNFO)
	assert.True(t, cfg.Download)
	assert.NotNil(t, cfg.DownloadExtrafanart)
	assert.True(t, *cfg.DownloadExtrafanart)
	assert.Equal(t, "/test/dest", cfg.Destination)
	assert.True(t, cfg.MergeOptions.ForceOverwrite)
}

func TestCLIApplyOptions_ToApplyPhaseConfig_Defaults(t *testing.T) {
	opts := CLIApplyOptions{}
	cfg := opts.ToApplyPhaseConfig()
	assert.False(t, cfg.DryRun)
	assert.False(t, cfg.GenerateNFO)
	assert.False(t, cfg.Download)
	assert.Nil(t, cfg.DownloadExtrafanart, "unset flag must yield nil so config default is respected")
}

// TestCLIApplyOptions_ToApplyPhaseConfig_ExtrafanartNotSetIsNil reproduces issue #79:
// when the --extrafanart flag is NOT set, the resulting ApplyPhaseConfig must
// carry a nil *bool so the downloader falls back to the config default
// (download_extrafanart: true). A non-nil &false unconditionally overrides the
// config default and silently disables extrafanart downloads — exactly what
// `javinizer sort` (no --extrafanart) was doing.
func TestCLIApplyOptions_ToApplyPhaseConfig_ExtrafanartNotSetIsNil(t *testing.T) {
	opts := CLIApplyOptions{} // no flag set
	cfg := opts.ToApplyPhaseConfig()
	assert.Nil(t, cfg.DownloadExtrafanart, "unset --extrafanart must be nil so the config default is respected")
}

// TestCLIApplyOptions_ToApplyPhaseConfig_ExtrafanartExplicitlyEnabled is the
// positive control: an explicitly-enabled flag yields a non-nil &true override.
func TestCLIApplyOptions_ToApplyPhaseConfig_ExtrafanartExplicitlyEnabled(t *testing.T) {
	opts := CLIApplyOptions{DownloadExtrafanart: true}
	cfg := opts.ToApplyPhaseConfig()
	require.NotNil(t, cfg.DownloadExtrafanart)
	assert.True(t, *cfg.DownloadExtrafanart)
}
