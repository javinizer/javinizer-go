package workflow

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
)

// OrchestrationMeta carries metadata about what the workflow's Scrape orchestrator
// did beyond the core scrape query. These fields are NEVER set by the scraper itself —
// only by the workflow's scrapeOrchestrator steps (DisplayTitle, Persist, Poster, Translation).
// Moving them here keeps scrape.ScrapeResult pure (only scraper output) while still
// propagating orchestration state to downstream consumers (MovieResult, API handlers).
//
// Embeds models.OrchestrationState for the five shared fields that propagate to
// MovieResult and the API layer. NeedsPersistence is a workflow-internal transient
// signal (set then cleared during persistence) that does not propagate to MovieResult.
type OrchestrationMeta struct {
	models.OrchestrationState `json:",inline"`
	NeedsPersistence          bool `json:"needs_persistence,omitempty"` // true if the scraper returned a cache hit that needs re-persistence (e.g. re-translated)
}

// scrapeOrchestrator is the internal interface for the Scrape phase.
// Unexported — only the composition root (Workflow) uses it.
// Returns (*scrape.ScrapeResult, *OrchestrationMeta, error) so that orchestration
// metadata is separated from the pure scrape output.
type scrapeOrchestrator interface {
	Execute(ctx context.Context, cmd scrape.ScrapeCmd, progress scrape.ProgressFunc) (*scrape.ScrapeResult, *OrchestrationMeta, error)
}

type scrapeOrchImpl struct {
	scraper        scrape.ScraperInterface
	movieRepo      database.MovieRepositoryInterface
	displayTitle   string
	templateEngine template.EngineInterface
	nameCfg        nfo.NFONameConfig
	logger         logging.Logger
}

var _ scrapeOrchestrator = (*scrapeOrchImpl)(nil)

func newScrapeOrchestrator(scraper scrape.ScraperInterface, movieRepo database.MovieRepositoryInterface, displayTitle string, templateEngine template.EngineInterface, nameCfg nfo.NFONameConfig, logger logging.Logger) scrapeOrchestrator {
	return &scrapeOrchImpl{
		scraper:        scraper,
		movieRepo:      movieRepo,
		displayTitle:   displayTitle,
		templateEngine: templateEngine,
		nameCfg:        nameCfg,
		logger:         logger,
	}
}

// Execute runs the 4-step Scrape sequence:
//  1. ForceRefresh cache clear (if cmd.ForceRefresh && movieRepo != nil)
//  2. Scrape via scraper.Scrape
//  3. Apply DisplayTitle via ApplyDisplayTitleFromSource
//  4. Persist to DB via movieRepo.UpsertWithTranslations
//
// Poster generation has been moved to the worker's scrape phase
// (see scrape_phase.go) — the orchestrator is now a pure query + persist pipeline.
func (o *scrapeOrchImpl) Execute(ctx context.Context, cmd scrape.ScrapeCmd, progress scrape.ProgressFunc) (*scrape.ScrapeResult, *OrchestrationMeta, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var meta OrchestrationMeta

	// Step 1: ForceRefresh cache clear.
	if cmd.ForceRefresh && o.movieRepo != nil {
		if err := o.movieRepo.Delete(ctx, cmd.MovieID); err != nil {
			resolveLogger(o.logger).Debugf("[workflow] Cache delete failed (may not exist): %v", err)
		}
	}

	if o.scraper == nil {
		return nil, nil, fmt.Errorf("workflow scraper not configured (scraper was nil at construction)")
	}

	// Step 2: Scrape (core query).
	result, err := o.scraper.Scrape(ctx, cmd, progress)
	if err != nil && (result == nil || result.Movie == nil) {
		return result, nil, err
	}

	// Propagate scrape-internal enrichment signals to meta.
	if result != nil {
		if result.TranslationWarning != "" {
			s := result.TranslationWarning
			meta.TranslationWarning = &s
		}
		if result.NeedsPersistence {
			meta.NeedsPersistence = true
		}
	}

	// Step 3: Apply DisplayTitle — sole application point for the scrape path.
	if result != nil && result.Movie != nil {
		ApplyDisplayTitleFromSource(ctx, result.Movie, result.Movie, o.displayTitle, o.templateEngine, o.nameCfg)
		meta.DisplayTitleApplied = true
	}

	// Step 4: Persist to database (Upsert).
	// Pass genre/actress translations from TranslationOutput for ID resolution.
	// Skipped when cmd.SkipPersist is set — the caller owns persistence in that
	// case (the batch scrape phase offloads this to a dedicated goroutine pool so
	// the errgroup-gated scrape workers don't block on SQLite's single-writer lock).
	if result != nil && result.Movie != nil && o.movieRepo != nil && !cmd.SkipPersist {
		var genreTrans []models.GenreTranslationData
		var actressTrans []models.ActressTranslationData
		if result.TranslationOutput != nil {
			genreTrans = result.TranslationOutput.GenreTranslations
			actressTrans = result.TranslationOutput.ActressTranslations
		}
		savedMovie, upsertErr := o.movieRepo.UpsertWithTranslations(ctx, result.Movie, genreTrans, actressTrans)
		if upsertErr != nil {
			resolveLogger(o.logger).Warnf("[workflow] Failed to persist %s: %v", result.Movie.ID, upsertErr)
			return result, &meta, upsertErr
		}
		result.Movie = savedMovie
		meta.Persisted = true
		// Clear the NeedsPersistence signal now that we've persisted.
		meta.NeedsPersistence = false
	}

	// Poster generation has been moved to the worker's scrape phase.
	// The scrape orchestrator is now a pure query + persist pipeline.
	// The worker calls posterGen.GeneratePoster(ctx, jobID, movie) after
	// Scrape() returns, keeping the side-effect (filesystem write) out of
	// the query path.

	return result, &meta, err
}

// noOpScrapeOrchestrator returns an error when Scrape is called on a Workflow
// that was not configured for scraping (e.g., scan-only mode via WorkflowFactory).
// Per T-098-01: returns error (not silent success) — callers detect misconfiguration.
type noOpScrapeOrchestrator struct{}

var _ scrapeOrchestrator = (*noOpScrapeOrchestrator)(nil)

func (noOpScrapeOrchestrator) Execute(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *OrchestrationMeta, error) {
	return nil, nil, fmt.Errorf("scrape not configured")
}
