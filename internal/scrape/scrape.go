// Package scrape implements the core scraping engine: a Scraper struct that orchestrates the
// full metadata scrape pipeline for a single MovieID.
//
// A single Scrape() call runs: cache check → multi-source scraper query → result aggregation.
// Scrape is a pure query — it performs no DB writes and no poster generation by default.
// Persistence and poster generation are the caller's responsibility (typically Workflow.Scrape).
// The engine discovers available scrapers by querying a ScraperInstanceResolver at runtime.
//
// This package does NOT implement individual website scrapers — those live in the scraper
// package and its sub-packages (one per website).
//
// Key types:
//   - Scraper — the engine struct (holds registry, aggregator, DB for cache reads, HTTP client)
//   - ScraperInstanceResolver — narrow interface for the scraper registry dependency
//   - ScrapeCmd — parameters for a single scrape operation (MovieID, force refresh, etc.)
//   - ScrapeResult — the output (movie, results, sources, timing, message)
package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/spf13/afero"
)

// ProgressStep represents a step name in the progress reporting pipeline.
// The values match worker.JobEventStep string values so that the worker
// can safely convert via JobEventStep(string(step)).
type ProgressStep string

// Progress step constants. Callers should use these instead of bare string
// literals to prevent silent breakage if the step values ever change.
const (
	ProgressStepScrape   ProgressStep = "scrape"
	ProgressStepOrganize ProgressStep = "organize"
	ProgressStepDownload ProgressStep = "download"
	ProgressStepNFO      ProgressStep = "nfo"
	ProgressStepApply    ProgressStep = "apply"
)

// ProgressFunc is the callback signature for progress reporting during
// scrape and apply operations. The step parameter is a typed ProgressStep
// rather than a bare string to prevent invalid values from propagating.
type ProgressFunc func(step ProgressStep, pct float64, msg string)

// ScrapeStatus represents the status of a scrape operation.
type ScrapeStatus string

// Status constants for ScrapeResult.Status.
// Use these instead of raw string literals to prevent silent breakage
// if the status values ever change.
const (
	StatusCompleted ScrapeStatus = "completed"
	StatusFailed    ScrapeStatus = "failed"
)

// MarshalJSON serializes ScrapeStatus to a JSON string.
func (s ScrapeStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON deserializes ScrapeStatus from a JSON string.
func (s *ScrapeStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*s = ScrapeStatus(str)
	return nil
}

// ScrapeCmd holds the parameters for a single scrape operation.
type ScrapeCmd struct {
	MovieID          string
	ForceRefresh     bool
	SelectedScrapers []string
	PriorityOverride []string
	RawInput         string // Raw URL/manual string — seam resolves via matcher.ParseInput internally
	ParseWarning     string // Set when RawInput could not be parsed; used as-is for MovieID

	// SkipPersist opts out of the scrape orchestrator's synchronous DB persist
	// (step 4 of scrapeOrchestrator.Execute). Callers that set this MUST persist
	// the returned movie themselves — typically the batch scrape phase, which
	// offloads persistence to a dedicated goroutine pool so the errgroup-gated
	// scrape workers never block on SQLite's single-writer lock (the root cause
	// of the 5→1 worker degradation).
	// Single-scrape paths (CLI scrape, API scrape/rescrape, workflow tests) leave
	// this false so Workflow.Scrape continues to persist inline before returning.
	SkipPersist bool
}

// ScrapeResult holds the output of a scrape operation: the aggregated movie, per-scraper results, field sources, and timing.
type ScrapeResult struct {
	// Movie is the aggregated result from scraping.
	// API handlers should use this field instead of calling conversion functions.
	// Nil when Status != StatusSuccess.
	Movie          *models.Movie `json:"movie,omitempty"`
	ScraperResults []*models.ScraperResult
	FieldSources   map[string]string
	ActressSources map[string]string
	Status         ScrapeStatus
	Message        string

	// Cached indicates this result was served from the movie DB cache
	// (tryCache) rather than a live scrape. Set so downstream consumers can
	// distinguish a cache hit from a live scrape without inferring it from
	// ScraperResults length (which is now populated on cache hits too, via
	// ScraperResultFromCachedMovie, for the review source viewer).
	Cached bool `json:"cached,omitempty"`

	// Internal enrichment signals — read by the workflow orchestrator and propagated
	// to OrchestrationMeta. Downstream consumers (MovieResult, API) should read from
	// OrchestrationMeta, not from these fields directly.
	TranslationWarning string                         `json:"translation_warning,omitempty"` // set by applyTranslation when partial translation occurs
	TranslationOutput  *translation.TranslationOutput `json:"-"`                             // genre/actress translation data for persistence
	NeedsPersistence   bool                           `json:"needs_persistence,omitempty"`   // set by tryCache when cached result needs re-persistence

	StartedAt time.Time
	EndedAt   time.Time
}

// ScraperInstanceResolver is the narrow interface the scrape engine requires
// from the scraper registry. It defines only the instance-query methods needed
// to resolve and order scrapers for a given input. Defined here per Go
// convention: consume interfaces, produce structs.
type ScraperInstanceResolver interface {
	GetInstance(name string) (models.Scraper, bool)
	GetInstancesByPriorityForInput(priority []string, input string) []models.Scraper
	GetAllInstances() []models.Scraper

	// Names returns the sorted list of registered scraper names.
	// Included because consumers (TUI, API) need to enumerate available
	// scrapers without constructing full instances.
	Names() []string
}

// Scraper is the scrape engine that orchestrates cache lookup, multi-source scraping, and aggregation for a single MovieID.
type Scraper struct {
	registry    ScraperInstanceResolver
	aggregator  aggregator.AggregatorInterface
	actressRepo database.ActressRepositoryInterface
	movieRepo   database.MovieRepositoryInterface
	httpClient  httpclientiface.HTTPClient
	cfg         *Config
	translator  Translator
	fs          afero.Fs
}

// ScraperInterface is the contract for executing a scrape operation.
type ScraperInterface interface {
	Scrape(ctx context.Context, cmd ScrapeCmd, progress ProgressFunc) (*ScrapeResult, error)
}

var _ ScraperInterface = (*Scraper)(nil)

// New constructs a Scraper engine from its registry, aggregator, repositories, HTTP client, config, translator, and filesystem dependencies.
func New(
	registry ScraperInstanceResolver,
	aggregator aggregator.AggregatorInterface,
	actressRepo database.ActressRepositoryInterface,
	movieRepo database.MovieRepositoryInterface,
	httpClient httpclientiface.HTTPClient,
	cfg *Config,
	translator Translator,
	fs afero.Fs,
) *Scraper {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	if translator == nil {
		translator = noOpTranslator{}
	}
	// Default a nil config to a zero-value Config. Several downstream paths
	// (resolveScrapeInput, postProcessScraped) dereference cfg unconditionally,
	// so a nil here would panic on RawInput resolution / translation gating.
	// A zero-value Config is safe (all fields are zeroable) and matches the
	// "nothing configured" behavior.
	if cfg == nil {
		cfg = &Config{}
	}
	return &Scraper{
		registry:    registry,
		aggregator:  aggregator,
		actressRepo: actressRepo,
		movieRepo:   movieRepo,
		httpClient:  httpClient,
		cfg:         cfg,
		translator:  translator,
		fs:          fs,
	}
}

func prog(p ProgressFunc, step ProgressStep, pct float64, msg string) {
	if p != nil {
		p(step, pct, msg)
	}
}

// resolveScrapeInput resolves RawInput if provided, parsing URL/manual input
// to extract MovieID and determine optimal scrapers. Returns the resolved
// ScrapeCmd or an error if MovieID is empty after resolution.
func resolveScrapeInput(ctx context.Context, cmd ScrapeCmd, registry ScraperInstanceResolver, cfg *Config) (ScrapeCmd, error) {
	if cmd.RawInput != "" {
		parsed, parseErr := matcher.ParseInput(cmd.RawInput, registry)
		if parseErr != nil {
			logging.Warnf("[scrape] RawInput parse failed for %q: %v (using as-is for MovieID)", RedactURLQuery(cmd.RawInput), parseErr)
			cmd.MovieID = RedactURLQuery(cmd.RawInput)
			cmd.ParseWarning = fmt.Sprintf("input could not be parsed: %v", parseErr)
		} else {
			cmd.MovieID = parsed.ID
			if len(cmd.SelectedScrapers) == 0 && parsed.IsURL && len(parsed.CompatibleScrapers) > 0 {
				cmd.PriorityOverride = matcher.CalculateOptimalScrapers(nil, cfg.ScrapersPriority, parsed)
			} else if len(cmd.SelectedScrapers) > 0 {
				cmd.SelectedScrapers = matcher.CalculateOptimalScrapers(cmd.SelectedScrapers, cfg.ScrapersPriority, parsed)
			}
		}
	}

	if cmd.MovieID == "" {
		return cmd, fmt.Errorf("scrape called with empty MovieID")
	}

	return cmd, nil
}

// postProcessScraped enriches the aggregated movie with actress DB data,
// translation, and assembles the final ScrapeResult.
func postProcessScraped(ctx context.Context, scraped *models.Movie, results []*models.ScraperResult, aggResult *aggregator.AggregateResult, cfg *Config, translator Translator, actressRepo database.ActressRepositoryInterface, cmd ScrapeCmd, startTime time.Time) (*ScrapeResult, error) {
	var fieldSources map[string]string
	var resolvedPriorities map[string][]string
	if aggResult != nil {
		fieldSources = aggResult.FieldSources
		resolvedPriorities = aggResult.ResolvedPriorities
	}

	actressSources := buildActressSourcesFromScrapeResults(results, resolvedPriorities, cmd.SelectedScrapers, scraped.Actresses)

	if actressRepo != nil {
		if enriched := enrichActressesFromDB(ctx, scraped, actressRepo, cfg); enriched > 0 {
			logging.Debugf("[scrape] Enriched %d actresses from database", enriched)
		}
	}

	var translationWarning string
	var translationOutput *translation.TranslationOutput
	if cfg.TranslationEnabled {
		translationWarning, translationOutput = applyTranslation(ctx, scraped, translator)
	}

	now := time.Now()
	result := &ScrapeResult{
		Movie:              scraped,
		ScraperResults:     results,
		FieldSources:       fieldSources,
		ActressSources:     actressSources,
		TranslationWarning: translationWarning,
		TranslationOutput:  translationOutput,
		Message:            cmd.ParseWarning,
		Status:             StatusCompleted,
		StartedAt:          startTime,
		EndedAt:            now,
	}

	return result, nil
}

// Scrape runs the cache-aware scrape pipeline for the given command and returns the aggregated result.
func (s *Scraper) Scrape(ctx context.Context, cmd ScrapeCmd, progress ProgressFunc) (*ScrapeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Scrape is a pure query — no DB writes, no poster generation.
	// Persistence and poster generation are handled by the caller (typically Workflow.Scrape).
	startTime := time.Now()
	prog(progress, ProgressStepScrape, 0, "Starting...")

	// Phase 1: Resolve input
	cmd, err := resolveScrapeInput(ctx, cmd, s.registry, s.cfg)
	if err != nil {
		return nil, err
	}

	var actressRepo database.ActressRepositoryInterface
	if s.actressRepo != nil {
		actressRepo = s.actressRepo
	}

	skipCache := cmd.ForceRefresh || len(cmd.SelectedScrapers) > 0

	if !skipCache {
		result := s.tryCache(ctx, cmd, actressRepo, startTime)
		if result != nil {
			prog(progress, ProgressStepScrape, 1, "Found in cache")
			return result, nil
		}
	}

	prog(progress, ProgressStepScrape, 0.2, "Querying scrapers...")

	// Phase 2: Query + aggregate
	scraperNames := resolveScraperNames(cmd.SelectedScrapers, cmd.PriorityOverride, s.cfg)
	resolvedID := s.resolveContentID(ctx, cmd.MovieID, scraperNames)
	scrapers := s.registry.GetInstancesByPriorityForInput(scraperNames, resolvedID)

	results, failures := s.queryAll(ctx, cmd.MovieID, resolvedID, scrapers, startTime)
	if len(results) == 0 {
		return failedResult(cmd.MovieID, buildNoResultsError(failures), startTime), nil
	}

	prog(progress, ProgressStepScrape, 0.7, "Aggregating metadata...")

	var scraped *models.Movie
	var aggResult *aggregator.AggregateResult
	if len(cmd.SelectedScrapers) > 0 {
		scraped, aggResult, err = s.aggregator.AggregateWithPriority(results, cmd.SelectedScrapers)
	} else {
		scraped, aggResult, err = s.aggregator.Aggregate(results)
	}
	if err != nil {
		return nil, err
	}

	if scraped.ContentID == "" && resolvedID != "" && resolvedID != cmd.MovieID {
		scraped.ContentID = resolvedID
		logging.Debugf("[scrape] Using resolved ContentID %q as fallback (aggregator produced empty ContentID)", resolvedID)
	}

	// Phase 3: Post-process
	result, err := postProcessScraped(ctx, scraped, results, aggResult, s.cfg, s.translator, actressRepo, cmd, startTime)
	if err != nil {
		return nil, err
	}

	prog(progress, ProgressStepScrape, 1.0, "Completed")
	return result, nil
}

func failedResult(movieID string, message string, startTime time.Time) *ScrapeResult {
	now := time.Now()
	return &ScrapeResult{
		Status:    StatusFailed,
		Message:   message,
		StartedAt: startTime,
		EndedAt:   now,
	}
}
