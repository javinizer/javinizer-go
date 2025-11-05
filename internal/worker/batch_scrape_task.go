package worker

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

// BatchScrapeTask represents a task for scraping metadata for a single file in a batch operation
type BatchScrapeTask struct {
	BaseTask
	filePath         string
	fileIndex        int
	job              *BatchJob
	registry         *models.ScraperRegistry
	aggregator       *aggregator.Aggregator
	movieRepo        *database.MovieRepository
	matcher          *matcher.Matcher
	progressTracker  *ProgressTracker
	force            bool
	selectedScrapers []string // empty = use default
	httpClient       *http.Client
	userAgent        string
	referer          string
}

// NewBatchScrapeTask creates a new batch scrape task
func NewBatchScrapeTask(
	taskID string,
	filePath string,
	fileIndex int,
	job *BatchJob,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	movieRepo *database.MovieRepository,
	mat *matcher.Matcher,
	progressTracker *ProgressTracker,
	force bool,
	selectedScrapers []string,
	httpClient *http.Client,
	userAgent string,
	referer string,
) *BatchScrapeTask {
	desc := fmt.Sprintf("Scraping metadata for %s", filepath.Base(filePath))

	return &BatchScrapeTask{
		BaseTask: BaseTask{
			id:          taskID,
			taskType:    TaskTypeBatchScrape,
			description: desc,
		},
		filePath:         filePath,
		fileIndex:        fileIndex,
		job:              job,
		registry:         registry,
		aggregator:       agg,
		movieRepo:        movieRepo,
		matcher:          mat,
		progressTracker:  progressTracker,
		force:            force,
		selectedScrapers: selectedScrapers,
		httpClient:       httpClient,
		userAgent:        userAgent,
		referer:          referer,
	}
}

// Execute implements the Task interface
func (t *BatchScrapeTask) Execute(ctx context.Context) error {
	// Update progress tracker
	t.progressTracker.Update(t.id, 0.1, fmt.Sprintf("Starting scrape..."), 0)

	// Record running state immediately so UI can show in-progress status
	startTime := time.Now()
	t.job.UpdateFileResult(t.filePath, &FileResult{
		FilePath:  t.filePath,
		Status:    JobStatusRunning,
		StartedAt: startTime,
	})

	// Use the shared scraping logic
	movie, fileResult, err := RunBatchScrapeOnce(
		ctx,
		t.job,
		t.filePath,
		t.fileIndex,
		"", // No query override for normal batch scraping
		t.registry,
		t.aggregator,
		t.movieRepo,
		t.matcher,
		t.httpClient,
		t.userAgent,
		t.referer,
		t.force,
		t.selectedScrapers,
	)

	// Update job with result
	if fileResult != nil {
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
	movieID := fileResult.MovieID
	t.progressTracker.Complete(t.id, fmt.Sprintf("Scraped %s successfully", movieID))
	logging.Debugf("[Batch %s] File %d: Task completed successfully for %s", t.job.ID, t.fileIndex, movie.ID)

	return nil
}
