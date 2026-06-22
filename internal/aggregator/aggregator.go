package aggregator

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// AggregateResult carries the byproducts of an aggregation call.
// Returned explicitly so callers never need to query hidden mutable state.
type AggregateResult struct {
	FieldSources       map[string]string
	ResolvedPriorities map[string][]string
}

// AggregatorInterface abstracts aggregator operations for dependency injection.
// Allows CLI commands and API endpoints to accept either real Aggregator or test mocks.
type AggregatorInterface interface {
	Aggregate(results []*models.ScraperResult) (*models.Movie, *AggregateResult, error)

	AggregateWithPriority(results []*models.ScraperResult, customPriority []string) (*models.Movie, *AggregateResult, error)

	// ReloadReplacementCaches refreshes the genre, word, and alias replacement
	// caches from their backing repositories without reconstructing the
	// aggregator or any of its peers (scraper, matcher, organizer, etc.).
	// Use this on the hot path when a genre/word/alias mutation must be visible
	// to the next aggregation call — avoids the cold-start penalty of a full
	// factory rebuild.
	ReloadReplacementCaches(ctx context.Context)
}

// Compile-time verification that Aggregator implements AggregatorInterface
var _ AggregatorInterface = (*Aggregator)(nil)

// Aggregator combines metadata from multiple scrapers based on priority.
// It delegates genre replacement, word replacement, actress alias
// resolution, and actress merging to focused sub-modules, keeping the
// Aggregator itself as a thin composition root.
type Aggregator struct {
	cfg                *Config
	templateEngine     template.EngineInterface
	genreProcessor     genreProcessorInterface
	wordProcessor      wordProcessorInterface
	aliasResolver      aliasResolverInterface
	actressMerger      actressMergerInterface
	resolvedPriorities map[string][]string // Cached resolved priorities — set once in New(), read-only thereafter. Do NOT mutate after construction.
}

// Config returns the aggregator's configuration.
func (a *Aggregator) Config() (*Config, error) {
	if a == nil {
		return nil, fmt.Errorf("Config called on nil Aggregator")
	}
	return a.cfg, nil
}

// TemplateEngine returns the aggregator's template engine.
func (a *Aggregator) TemplateEngine() (template.EngineInterface, error) {
	if a == nil {
		return nil, fmt.Errorf("TemplateEngine called on nil Aggregator")
	}
	return a.templateEngine, nil
}

// ReloadReplacementCaches refreshes the genre, word, and alias replacement
// caches from their backing repositories. This is the targeted alternative
// to destroying and rebuilding the entire WorkflowFactory: only the
// in-memory replacement maps are swapped, so the scraper, matcher,
// organizer, downloader, and NFO generator remain cached.
//
// The reload is individually safe per sub-processor (each owns its own mutex)
// and is safe to call concurrently with ongoing aggregation.
func (a *Aggregator) ReloadReplacementCaches(ctx context.Context) {
	if a == nil {
		return
	}
	if a.genreProcessor != nil {
		a.genreProcessor.Reload(ctx)
	}
	if a.wordProcessor != nil {
		a.wordProcessor.Reload(ctx)
	}
	if a.aliasResolver != nil {
		a.aliasResolver.Reload(ctx)
	}
}

// New creates a new aggregator with injected sub-modules.
// This is the single constructor — callers construct sub-modules
// externally and pass them in:
//
//	agg := aggregator.New(cfg,
//	    aggregator.NewGenreProcessor(cfg.Metadata, genreRepo),
//	    aggregator.NewWordProcessor(cfg.Metadata, wordRepo),
//	    aggregator.NewAliasResolver(cfg.Metadata, aliasRepo),
//	)
//
// Pass nil sub-modules when their features are not needed or no database
// is available.
func New(cfg *Config, genreProcessor genreProcessorInterface, wordProcessor wordProcessorInterface, aliasResolver aliasResolverInterface) *Aggregator {
	if cfg == nil {
		return nil
	}
	te := cfg.TemplateEngine
	if te == nil {
		te = template.NewEngine()
	}
	agg := &Aggregator{
		cfg:            cfg,
		templateEngine: te,
		genreProcessor: genreProcessor,
		wordProcessor:  wordProcessor,
		aliasResolver:  aliasResolver,
		actressMerger:  newActressMerger(),
	}
	agg.resolvePriorities()
	return agg
}
