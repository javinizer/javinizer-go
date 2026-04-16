package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

// BatchScrapeOptions encapsulates all parameters for creating a BatchScrapeTask.
type BatchScrapeOptions struct {
	TaskID            string
	FilePath          string
	FileIndex         int
	Job               *BatchJob
	Registry          *models.ScraperRegistry
	Aggregator        *aggregator.Aggregator
	MovieRepo         *database.MovieRepository
	Matcher           *matcher.Matcher
	ProgressTracker   *ProgressTracker
	Force             bool
	UpdateMode        bool
	SelectedScrapers  []string
	HTTPClient        httpclientiface.HTTPClient
	UserAgent         string
	Referer           string
	ProcessedMovieIDs map[string]bool
	Cfg               *config.Config
	ScalarStrategy    string
	ArrayStrategy     string
}

// BatchScrapeTask represents a task for scraping metadata for a single file in a batch operation
type BatchScrapeTask struct {
	BaseTask
	filePath          string
	fileIndex         int
	job               *BatchJob
	registry          *models.ScraperRegistry
	aggregator        *aggregator.Aggregator
	movieRepo         *database.MovieRepository
	matcher           *matcher.Matcher
	progressTracker   *ProgressTracker
	force             bool
	updateMode        bool
	selectedScrapers  []string
	httpClient        httpclientiface.HTTPClient
	userAgent         string
	referer           string
	processedMovieIDs map[string]bool
	cfg               *config.Config
	scalarStrategy    string
	arrayStrategy     string
}

// NewBatchScrapeTask creates a new batch scrape task
func NewBatchScrapeTask(opts *BatchScrapeOptions) *BatchScrapeTask {
	desc := fmt.Sprintf("Scraping metadata for %s", filepath.Base(opts.FilePath))

	return &BatchScrapeTask{
		BaseTask: BaseTask{
			id:          opts.TaskID,
			taskType:    TaskTypeBatchScrape,
			description: desc,
		},
		filePath:          opts.FilePath,
		fileIndex:         opts.FileIndex,
		job:               opts.Job,
		registry:          opts.Registry,
		aggregator:        opts.Aggregator,
		movieRepo:         opts.MovieRepo,
		matcher:           opts.Matcher,
		progressTracker:   opts.ProgressTracker,
		force:             opts.Force,
		updateMode:        opts.UpdateMode,
		selectedScrapers:  opts.SelectedScrapers,
		httpClient:        opts.HTTPClient,
		userAgent:         opts.UserAgent,
		referer:           opts.Referer,
		processedMovieIDs: opts.ProcessedMovieIDs,
		cfg:               opts.Cfg,
		scalarStrategy:    opts.ScalarStrategy,
		arrayStrategy:     opts.ArrayStrategy,
	}
}

// Execute implements the Task interface
func (t *BatchScrapeTask) Execute(ctx context.Context) error {
	fileInfo := scanner.FileInfo{
		Path:      t.filePath,
		Name:      filepath.Base(t.filePath),
		Extension: filepath.Ext(t.filePath),
		Dir:       filepath.Dir(t.filePath),
	}
	matchResults := t.matcher.Match([]scanner.FileInfo{fileInfo})

	var movieID string
	if len(matchResults) > 0 {
		movieID = matchResults[0].ID
	} else {
		movieID = filepath.Base(t.filePath)
	}

	// Step 1: Initial progress update
	t.progressTracker.Update(t.id, 0.1, fmt.Sprintf("Scraping %s", movieID), 0)

	// Record running state immediately so UI can show in-progress status
	startTime := time.Now()
	t.job.UpdateFileResult(t.filePath, &FileResult{
		FilePath:  t.filePath,
		Status:    JobStatusRunning,
		StartedAt: startTime,
	})

	// Step 2: Querying scrapers
	t.progressTracker.Update(t.id, 0.2, "Querying scrapers...", 0)

	// Use the shared scraping logic
	movie, fileResult, err := RunBatchScrapeOnce(
		ctx,
		t.job,
		t.filePath,
		t.fileIndex,
		"",
		t.registry,
		t.aggregator,
		t.movieRepo,
		t.matcher,
		t.httpClient,
		t.userAgent,
		t.referer,
		t.force,
		t.updateMode,
		t.selectedScrapers,
		nil, // No priority override for batch scraping
		t.processedMovieIDs,
		t.cfg,
		t.scalarStrategy,
		t.arrayStrategy,
	)

	// Step 3: Aggregating results (if we got this far without error)
	if err == nil && fileResult != nil && fileResult.Status == JobStatusCompleted {
		t.progressTracker.Update(t.id, 0.8, "Aggregating metadata...", 0)
	}

	// Update job with result
	if fileResult != nil {
		// Preserve multipart metadata from discovery phase (for letter patterns like -A, -B)
		// This is needed because individual file matching loses multipart context for letter patterns
		t.job.mu.RLock()
		if info, ok := t.job.FileMatchInfo[t.filePath]; ok {
			fileResult.IsMultiPart = info.IsMultiPart
			fileResult.PartNumber = info.PartNumber
			fileResult.PartSuffix = info.PartSuffix
			logging.Debugf("[Batch %s] File %d: Applied discovery multipart metadata: IsMultiPart=%v, PartNumber=%d, PartSuffix=%s",
				t.job.ID, t.fileIndex, info.IsMultiPart, info.PartNumber, info.PartSuffix)
		}
		t.job.mu.RUnlock()
		t.job.UpdateFileResult(t.filePath, fileResult)
	}

	// Update progress tracker based on result
	if err != nil {
		if err == ctx.Err() {
			// Context cancelled
			t.progressTracker.Cancel(t.id)
		} else {
			// Scraping failed
			t.progressTracker.Fail(t.id, err)
		}
		return err
	}

	// Success
	if fileResult != nil && fileResult.MovieID != "" {
		movieID = fileResult.MovieID
	}
	t.progressTracker.Complete(t.id, fmt.Sprintf("Scraped %s successfully", movieID))
	if movie != nil {
		logging.Debugf("[Batch %s] File %d: Task completed successfully for %s", t.job.ID, t.fileIndex, movie.ID)
	}

	return nil
}
