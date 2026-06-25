package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/spf13/afero"
)

// compareOrchestrator is the internal interface for the Compare phase.
// Unexported — only the composition root (Workflow) uses it.
type compareOrchestrator interface {
	Execute(ctx context.Context, cmd CompareCmd) (*CompareResult, error)
}

// compareOrchImpl owns the compare pipeline: parse NFO, scrape fresh, merge.
type compareOrchImpl struct {
	fs      afero.Fs
	merger  nfo.NFOFieldMerger
	scraper scrape.ScraperInterface
	logger  logging.Logger
}

var _ compareOrchestrator = (*compareOrchImpl)(nil)

func newCompareOrchestrator(fs afero.Fs, merger nfo.NFOFieldMerger, scraper scrape.ScraperInterface, logger logging.Logger) compareOrchestrator {
	return &compareOrchImpl{
		fs:      fs,
		merger:  merger,
		scraper: scraper,
		logger:  logger,
	}
}

// Execute runs the compare pipeline: parse NFO → scrape fresh → merge.
func (o *compareOrchImpl) Execute(ctx context.Context, cmd CompareCmd) (*CompareResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Validate required inputs.
	if cmd.MovieID == "" {
		return nil, fmt.Errorf("movie_id is required for comparison")
	}
	if cmd.NFOPath == "" {
		return nil, fmt.Errorf("nfo_path is required for comparison")
	}

	result := &CompareResult{}

	// Step 1: Parse NFO file.
	if o.fs == nil {
		return nil, fmt.Errorf("filesystem not configured")
	}
	parseResult, parseErr := nfo.ParseNFO(o.fs, cmd.NFOPath)
	if parseErr != nil {
		// Distinguish between "file not found" (expected — continue without NFO)
		// and "parse error" (malformed XML — return as error so the handler
		// can return an appropriate HTTP status code).
		if errors.Is(parseErr, os.ErrNotExist) {
			resolveLogger(o.logger).Debugf("[workflow] Compare: NFO not found at %s", cmd.NFOPath)
			result.NFOExists = false
		} else {
			return nil, fmt.Errorf("%w: %s", ErrNFOParseFailed, parseErr.Error())
		}
	} else {
		result.NFOExists = true
		result.NFOData = parseResult.Movie
		// Only return the filename (not absolute path) to avoid disclosing server directory structure
		result.NFOPath = filepath.Base(cmd.NFOPath)
	}

	// Step 2: Scrape fresh data.
	// Compare's purpose is to diff the existing NFO against freshly scraped
	// source data, so the DB cache must always be bypassed (ForceRefresh=true).
	// main's compareNFO handler scraped live sources directly with no cache;
	// without this, the default compare path returns the cached movie and the
	// diff shows no differences even when sources have changed.
	if o.scraper == nil {
		return nil, fmt.Errorf("workflow scraper not configured (scraper was nil at construction)")
	}
	scrapeResult, scrapeErr := o.scraper.Scrape(ctx, scrape.ScrapeCmd{
		MovieID:          cmd.MovieID,
		SelectedScrapers: cmd.SelectedScrapers,
		ForceRefresh:     true,
	}, nil)
	if scrapeErr != nil {
		return nil, fmt.Errorf("%w for %s: %s", ErrScrapeFailed, cmd.MovieID, scrapeErr.Error())
	}
	if scrapeResult == nil || scrapeResult.Movie == nil {
		return nil, fmt.Errorf("%w for %s", ErrScrapeNoResult, cmd.MovieID)
	}
	result.ScrapedData = scrapeResult.Movie

	// Step 3: If NFO doesn't exist, return scraped data without merge.
	if !result.NFOExists || result.NFOData == nil {
		result.Movie = result.ScrapedData
		return result, nil
	}

	// Step 4: Merge using pre-resolved strategies.
	// Per ADR-0030: preset resolution happens at the factory boundary.
	// By the time we reach the orchestrator, ScalarStrategy and ArrayStrategy
	// are fully resolved — no Parse* or ApplyPreset calls here.
	scalarStrategy := cmd.ScalarStrategy
	mergeArrays := cmd.ArrayStrategy

	// Step 5: Merge.
	mergeResult, mergeErr := nfo.MergeMovieMetadataWithOptions(result.ScrapedData, result.NFOData, scalarStrategy, mergeArrays)
	if mergeErr != nil {
		return nil, fmt.Errorf("%w: %s", ErrMergeFailed, mergeErr.Error())
	}

	result.Movie = mergeResult.Merged
	result.MergeStats = &mergeResult.Stats

	// Step 6: Identify per-field differences.
	// Domain logic lives behind the seam — the API layer maps to JSON.
	result.Differences = identifyDifferences(result.NFOData, result.ScrapedData, result.Movie)

	return result, nil
}

// noOpCompareOrchestrator returns an error when Compare is called on a Workflow
// that was not configured for comparison (e.g., scan-only mode via WorkflowFactory).
// Per T-098-03: returns error (not silent success) — callers detect misconfiguration.
type noOpCompareOrchestrator struct{}

var _ compareOrchestrator = (*noOpCompareOrchestrator)(nil)

func (noOpCompareOrchestrator) Execute(_ context.Context, _ CompareCmd) (*CompareResult, error) {
	return nil, fmt.Errorf("compare not configured")
}
