package commandutil

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/operationmode"
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
		RequestTimeout:  time.Duration(cfg.Scrapers.RequestTimeoutSeconds) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
		PosterDisabled:    !cfg.Output.Download.DownloadPoster,
	}
}

// CLIApplyOptions holds the resolved CLI flags for the apply phase.
// Extracted from sort/update commands to centralize the mapping.
type CLIApplyOptions struct {
	DryRun                 bool
	MoveFiles              bool
	LinkMode               organizer.LinkMode
	ForceUpdate            bool
	SkipOrganize           bool
	GenerateNFO            bool
	Download               bool
	DownloadExtrafanart    bool
	OverwriteExistingMedia bool
	Destination            string
	MergeOptions           workflow.MergeOptions
}

// persistedJobMode maps the CLI's update/organize toggle onto the persisted
// job identity fields, mirroring the API job-creation mapping in
// StartScrapeUseCase (internal/api/batch/usecases.go): the update flow's
// leave-in-place operation projects to update=true + operation_mode=
// metadata-artwork (applyplan.Project and the frontend's projectLegacyPlan
// agree on that pairing, and resolveUpdateApplyConfig runs with
// OrganizeOptions.Skip=true). Without this the CLI persisted update batches
// as update=false + empty mode, and API/history consumers read them as
// organize (#248 codex P2, F1).
//
// Organize runs (skipOrganize=false) keep an EMPTY mode override: the CLI
// resolves seam strings without an operation-mode input, so stamping the
// default ("organize") would additionally feed the organizer's per-command
// strategy override and rewrite the user's configured operation mode — while
// every consumer already infers organize from update=false + empty mode.
func persistedJobMode(skipOrganize bool) (update *bool, mode operationmode.OperationMode) {
	u := skipOrganize
	if skipOrganize {
		return &u, operationmode.OperationModeMetadataArtwork
	}
	return &u, ""
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
	// Persisted job identity (#248 codex P2, F1): committed onto the job by
	// jobController.StartApply and mirrored at job creation (newCLIBatchRuntime)
	// so a CLI update batch's jobs row classifies as update, not organize.
	updateMode, modeOverride := persistedJobMode(o.SkipOrganize)
	return worker.ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{
			Skip:        o.SkipOrganize,
			MoveFiles:   o.MoveFiles,
			LinkMode:    o.LinkMode,
			ForceUpdate: o.ForceUpdate,
		},
		MergeOptions:           o.MergeOptions,
		Destination:            o.Destination,
		DryRun:                 o.DryRun,
		GenerateNFO:            o.GenerateNFO,
		Download:               o.Download,
		DownloadExtrafanart:    downloadExtrafanart,
		OverwriteExistingMedia: o.OverwriteExistingMedia,
		Update:                 updateMode,
		OperationModeOverride:  modeOverride,
	}
}
