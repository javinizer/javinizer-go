package tui

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/aggregator"
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
	pool                  *worker.Pool
	progressTracker       *worker.ProgressTracker
	movieRepo             *database.MovieRepository
	registry              *models.ScraperRegistry
	aggregator            *aggregator.Aggregator
	downloader            *downloader.Downloader
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
	customScraperPriority []string // Optional custom scraper priority (nil = use default)
}

// NewProcessingCoordinator creates a new processing coordinator
func NewProcessingCoordinator(
	pool *worker.Pool,
	progressTracker *worker.ProgressTracker,
	movieRepo *database.MovieRepository,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	dl *downloader.Downloader,
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
	logging.Debugf("ProcessFiles called with %d files", len(files))

	for _, file := range files {
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
		processTask := worker.NewProcessFileTask(
			match,
			pc.registry,
			pc.aggregator,
			pc.movieRepo,
			pc.downloader,
			pc.organizer,
			pc.nfoGenerator,
			pc.destPath,
			pc.moveFiles,
			pc.forceUpdate,
			pc.forceRefresh,
			pc.progressTracker,
			pc.dryRun,
			pc.scrapeEnabled,
			pc.downloadEnabled,
			pc.organizeEnabled,
			pc.nfoEnabled,
			customScrapers,
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
