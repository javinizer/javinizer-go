package commandutil

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// BatchJobConfigFromAppConfig creates a worker.BatchJobConfig from the application config.
// This is the single source of truth for mapping *config.Config → BatchJobConfig fields.
func BatchJobConfigFromAppConfig(cfg *config.Config) worker.BatchJobConfig {
	return worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	}
}

// CLIApplyOptions holds the resolved CLI flags for the apply phase.
// Extracted from sort/update commands to centralize the mapping.
type CLIApplyOptions struct {
	DryRun              bool
	MoveFiles           bool
	LinkMode            organizer.LinkMode
	ForceUpdate         bool
	SkipOrganize        bool
	GenerateNFO         bool
	Download            bool
	DownloadExtrafanart bool
	Destination         string
	MergeOptions        workflow.MergeOptions
}

// ToApplyPhaseConfig converts CLIApplyOptions to a worker.ApplyPhaseConfig.
func (o CLIApplyOptions) ToApplyPhaseConfig() worker.ApplyPhaseConfig {
	// The --extrafanart flag is a force-enable: only emit a non-nil override
	// when it is explicitly set. A non-nil &false would override the config
	// default (download_extrafanart) and silently disable extrafanart downloads
	// for `sort`/`update` runs that simply omit the flag (issue #79). nil lets
	// the downloader fall back to the resolved config value.
	var downloadExtrafanart *bool
	if o.DownloadExtrafanart {
		t := true
		downloadExtrafanart = &t
	}
	return worker.ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{
			Skip:        o.SkipOrganize,
			MoveFiles:   o.MoveFiles,
			LinkMode:    o.LinkMode,
			ForceUpdate: o.ForceUpdate,
		},
		MergeOptions:        o.MergeOptions,
		Destination:         o.Destination,
		DryRun:              o.DryRun,
		GenerateNFO:         o.GenerateNFO,
		Download:            o.Download,
		DownloadExtrafanart: downloadExtrafanart,
	}
}
