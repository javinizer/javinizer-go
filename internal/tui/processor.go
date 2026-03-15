package tui

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// ProcessingCoordinator coordinates task execution for the TUI
type ProcessingCoordinator struct {
	pool                  PoolInterface
	progressTracker       ProgressTrackerInterface
	movieRepo             *database.MovieRepository
	registry              *models.ScraperRegistry
	aggregator            *aggregator.Aggregator
	downloader            DownloaderInterface
	organizer             *organizer.Organizer
	nfoGenerator          *nfo.Generator
	destPath              string
	moveFiles             bool
	forceUpdate           bool
	forceRefresh          bool
	dryRun                bool
	scrapeEnabled         bool
	downloadEnabled       bool
	organizeEnabled       bool
	nfoEnabled            bool
	linkMode              organizer.LinkMode
	updateMode            bool
	scalarStrategy        string
	arrayStrategy         string
	cfg                   *config.Config
	customScraperPriority []string // Optional custom scraper priority (nil = use default)
}

// NewProcessingCoordinator creates a new processing coordinator
//
// IMPORTANT: This constructor requires concrete types that implement the interfaces:
//   - pool must be *worker.Pool (type assertion in ProcessFiles)
//   - progressTracker must be *worker.ProgressTracker (type assertion in ProcessFiles)
//   - dl must be *downloader.Downloader (type assertion in ProcessFiles)
//
// Passing incorrect types will cause a runtime panic when ProcessFiles is called.
func NewProcessingCoordinator(
	pool PoolInterface,
	progressTracker ProgressTrackerInterface,
	movieRepo *database.MovieRepository,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	dl DownloaderInterface,
	org *organizer.Organizer,
	nfoGen *nfo.Generator,
	destPath string,
	moveFiles bool,
) *ProcessingCoordinator {
	return &ProcessingCoordinator{
		pool:            pool,
		progressTracker: progressTracker,
		movieRepo:       movieRepo,
		registry:        registry,
		aggregator:      agg,
		downloader:      dl,
		organizer:       org,
		nfoGenerator:    nfoGen,
		destPath:        destPath,
		moveFiles:       moveFiles,
		scrapeEnabled:   true,
		downloadEnabled: true,
		organizeEnabled: true,
		nfoEnabled:      true,
		linkMode:        organizer.LinkModeNone,
		scalarStrategy:  "prefer-nfo",
		arrayStrategy:   "merge",
	}
}

// SetOptions configures which operations to perform
func (pc *ProcessingCoordinator) SetOptions(scrape, download, organize, nfo bool) {
	pc.scrapeEnabled = scrape
	pc.downloadEnabled = download
	pc.organizeEnabled = organize
	pc.nfoEnabled = nfo
}

// SetDryRun sets whether to run in dry-run mode (preview only)
func (pc *ProcessingCoordinator) SetDryRun(dryRun bool) {
	pc.dryRun = dryRun
}

// SetDestPath sets the destination path for organized files
func (pc *ProcessingCoordinator) SetDestPath(destPath string) {
	pc.destPath = destPath
}

// SetMoveFiles sets whether to move files instead of copying
func (pc *ProcessingCoordinator) SetMoveFiles(moveFiles bool) {
	pc.moveFiles = moveFiles
}

// SetScrapeEnabled sets whether metadata scraping is enabled
func (pc *ProcessingCoordinator) SetScrapeEnabled(enabled bool) {
	pc.scrapeEnabled = enabled
}

// SetDownloadEnabled sets whether media downloads are enabled
func (pc *ProcessingCoordinator) SetDownloadEnabled(enabled bool) {
	pc.downloadEnabled = enabled
}

// SetDownloadExtrafanart sets whether extrafanart downloads are enabled
func (pc *ProcessingCoordinator) SetDownloadExtrafanart(enabled bool) {
	if pc.downloader != nil {
		pc.downloader.SetDownloadExtrafanart(enabled)
	}
}

// SetOrganizeEnabled sets whether file organization is enabled
func (pc *ProcessingCoordinator) SetOrganizeEnabled(enabled bool) {
	pc.organizeEnabled = enabled
}

// SetNFOEnabled sets whether NFO generation is enabled
func (pc *ProcessingCoordinator) SetNFOEnabled(enabled bool) {
	pc.nfoEnabled = enabled
}

// SetForceUpdate sets whether to force update existing files
func (pc *ProcessingCoordinator) SetForceUpdate(forceUpdate bool) {
	pc.forceUpdate = forceUpdate
}

// SetForceRefresh sets whether to force refresh from scrapers (clear cache)
func (pc *ProcessingCoordinator) SetForceRefresh(forceRefresh bool) {
	pc.forceRefresh = forceRefresh
}

// SetLinkMode sets how copy operations materialize files (copy/hardlink/softlink).
// Link mode is ignored when move mode is enabled.
func (pc *ProcessingCoordinator) SetLinkMode(mode organizer.LinkMode) {
	if !mode.IsValid() {
		mode = organizer.LinkModeNone
	}
	pc.linkMode = mode
}

// SetUpdateMode sets whether update-mode merge behavior is enabled.
func (pc *ProcessingCoordinator) SetUpdateMode(enabled bool) {
	pc.updateMode = enabled
}

// SetMergeStrategies sets scalar/array merge behavior for update mode.
func (pc *ProcessingCoordinator) SetMergeStrategies(scalarStrategy, arrayStrategy string) {
	pc.scalarStrategy = scalarStrategy
	pc.arrayStrategy = arrayStrategy
}

// SetConfig provides runtime config for template-aware NFO merge path resolution.
func (pc *ProcessingCoordinator) SetConfig(cfg *config.Config) {
	pc.cfg = cfg
}

// SetCustomScrapers sets custom scraper priority for manual search
// Makes a defensive copy to prevent data races with worker goroutines
func (pc *ProcessingCoordinator) SetCustomScrapers(scrapers []string) {
	if scrapers == nil {
		pc.customScraperPriority = nil
		return
	}
	pc.customScraperPriority = append([]string(nil), scrapers...)
}

// GetCustomScrapers returns the current custom scraper priority
// Returns a copy to prevent external mutation
func (pc *ProcessingCoordinator) GetCustomScrapers() []string {
	if pc.customScraperPriority == nil {
		return nil
	}
	return append([]string(nil), pc.customScraperPriority...)
}

// ProcessFiles processes the selected files with matched JAV IDs
func (pc *ProcessingCoordinator) ProcessFiles(
	ctx context.Context,
	files []FileItem,
	matches map[string]matcher.MatchResult,
) error {
	// Validate critical dependencies to prevent deep panics in worker package
	if pc.pool == nil {
		return fmt.Errorf("worker pool is nil")
	}
	if pc.registry == nil {
		return fmt.Errorf("scraper registry is nil")
	}
	if pc.progressTracker == nil {
		return fmt.Errorf("progress tracker is nil")
	}
	if pc.downloader == nil {
		return fmt.Errorf("downloader is nil")
	}

	logging.Debugf("ProcessFiles called with %d files", len(files))

	for _, file := range files {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		logging.Debugf("Processing file: %s, IsDir: %v, Matched: %v", file.Path, file.IsDir, file.Matched)

		// Skip directories
		if file.IsDir {
			logging.Debugf("Skipping directory: %s", file.Path)
			continue
		}

		// Skip files without matched JAV IDs
		if !file.Matched {
			logging.Debugf("Skipping unmatched file: %s", file.Path)
			continue
		}

		match, found := matches[file.Path]
		if !found {
			logging.Debugf("No match found for %s in matches map", file.Path)
			continue
		}

		logging.Debugf("Submitting task for file: %s (ID: %s)", file.Path, match.ID)

		// Make a defensive copy of custom scraper priority for this task
		// to prevent data races if the UI modifies it while tasks are running
		var customScrapers []string
		if pc.customScraperPriority != nil {
			customScrapers = append([]string(nil), pc.customScraperPriority...)
		}

		// Submit a composite task that handles all operations sequentially
		// Type assertions needed for pool.Submit and progressTracker to pass to worker.NewProcessFileTask
		// (worker package expects concrete types)
		taskOpts := []worker.ProcessFileOption{
			worker.WithLinkMode(pc.linkMode),
			worker.WithUpdateMerge(pc.updateMode, pc.scalarStrategy, pc.arrayStrategy, pc.cfg),
		}

		processTask := worker.NewProcessFileTask(
			match,
			pc.registry,
			pc.aggregator,
			pc.movieRepo,
			pc.downloader.(*downloader.Downloader),
			pc.organizer,
			pc.nfoGenerator,
			pc.destPath,
			pc.moveFiles,
			pc.forceUpdate,
			pc.forceRefresh,
			pc.progressTracker.(*worker.ProgressTracker),
			pc.dryRun,
			pc.scrapeEnabled,
			pc.downloadEnabled,
			pc.organizeEnabled,
			pc.nfoEnabled,
			customScrapers,
			taskOpts...,
		)

		if err := pc.pool.Submit(processTask); err != nil {
			return fmt.Errorf("failed to submit process task for %s: %w", match.ID, err)
		}
	}

	logging.Debugf("ProcessFiles completed, submitted tasks")
	return nil
}

// Wait waits for all tasks to complete
func (pc *ProcessingCoordinator) Wait() error {
	return pc.pool.Wait()
}

// Stop stops the worker pool
func (pc *ProcessingCoordinator) Stop() {
	pc.pool.Stop()
}
