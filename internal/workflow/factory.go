package workflow

import (
	"context"
	"fmt"
	"time"

	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// ScraperResolver is the narrow interface the workflow package requires from the
// scraper registry. It combines instance resolution (for the scrape engine)
// with download proxy collection (for the downloader HTTP client config).
// Defined here per Go convention: consume interfaces, produce structs.
//
// ScraperResolver satisfies scrape.ScraperInstanceResolver, so it can be
// passed directly to scrape.New().
type ScraperResolver interface {
	scrape.ScraperInstanceResolver
	CollectDownloadProxyResolvers(priority []string) []models.DownloadProxyResolver
}

// workflowFactoryConfig holds constructed domain objects for WorkflowFactory.
// replaces the former factoryConfig grab-bag of raw configs with
// a struct that accepts already-constructed domain objects. Callers that have
// a *config.Config and want the convenience of auto-wiring should use
// NewFactoryConfigFromRepos, which calls ConfigFromAppConfig bridges and
// domain constructors internally.
//
// All fields are required unless explicitly documented as optional.
// Construction of these objects is the caller's responsibility — WorkflowFactory
// does not call ConfigFromAppConfig or any domain constructors.
type workflowFactoryConfig struct {
	// Workflow-level sub-configs (promoted from former workflowConfig grab-bag).
	MaxFilesPerScan int
	DownloadHTTPCfg downloader.HTTPClientConfig

	// Orchestrator sub-configs — extracted per W3-A from flat fields.
	// Each sub-config groups the parameters consumed by one orchestrator,
	// reducing parameter counts at construction sites.
	PreviewCfg PreviewConfig
	ApplyCfg   ApplyConfig

	// Shared filesystem and template engine.
	Fs             afero.Fs
	TemplateEngine *template.Engine

	// Domain objects — constructed by the caller.
	Matcher      matcher.MatcherInterface
	Scanner      scanner.ScannerInterface
	ScannerCfg   scanner.Config
	Organizer    organizer.OrganizerInterface
	NFOGenerator nfo.GeneratorInterface
	NFOIface     nfo.NFOInterface // Composite passed to sub-orchestrators that narrow it: compare→NFOFieldMerger, preview→NFOFieldMerger, apply→NFOFileMerger, revertLog→NFOFieldMerger
	Downloader   downloader.DownloaderInterface
	HTTPClient   httpclientiface.HTTPClient
	PosterGen    poster.PosterGenerator
	Scraper      scrape.ScraperInterface
	Aggregator   aggregator.AggregatorInterface

	// Repositories — required for orchestrator construction.
	database.Repositories

	// RevertLog configuration — needed by NewWorkflow to construct per-call RevertLog.
	AllowRevert bool
	NFOCfg      *nfo.Config

	// OperationMode — optional; if set, was already resolved at the factory boundary.
	OperationMode *operationmode.OperationMode

	// Logger — optional; structured logger seam. Defaults to GlobalLogger() when nil.
	Logger logging.Logger
}

// resolveLogger returns the provided logger, or logging.GlobalLogger() if nil.
// Used by orchestrators that may not have their logger field initialized
// when constructed directly by tests (bypassing the factory).
func resolveLogger(l logging.Logger) logging.Logger {
	if l == nil {
		return logging.GlobalLogger()
	}
	return l
}

// WorkflowComponents holds a Workflow and its supporting components returned
// by NewWorkflowFactory + .NewWorkflow().
type WorkflowComponents struct {
	Workflow  WorkflowInterface
	Matcher   matcher.MatcherInterface
	Scanner   scanner.ScannerInterface
	PosterGen poster.PosterGenerator
}

// domainConfigs holds the narrow configs extracted from *config.Config by each
// domain package's ConfigFromAppConfig bridge. Created by extractDomainConfigs.
type domainConfigs struct {
	nfoNameCfg  nfo.NFONameConfig // Pre-constructed, shared across nfo/organizer/downloader bridges
	snCfg       *scanner.Config
	matchCfg    *matcher.Config
	orgCfg      *organizer.Config
	nfoCfg      *nfo.Config
	downloadCfg *downloader.Config
	scrapeCfg   *scrape.Config
	aggCfg      *aggregator.Config
	translation scrape.Translator
}

// extractDomainConfigs calls each domain package's ConfigFromAppConfig bridge
// and returns the narrow configs in a single struct. This is the first step
// of NewFactoryConfigFromRepos — separating config extraction from object
// construction gives each step a clear contract and makes it easier to test
// that the bridges stay in sync with the monolith config.
//
// Per W3-B: NFONameConfig is constructed first from the raw config fields, then
// passed to nfo, organizer, and downloader bridges so that overlapping fields
// (FilenameTemplate, FirstNameOrder, PerFile, GroupActress, GroupActressName)
// are read from the monolith config exactly once instead of independently in
// each bridge.
func extractDomainConfigs(cfg *config.Config) domainConfigs {
	// Construct NFONameConfig first — shared across nfo, organizer, downloader bridges.
	// Use the canonical constructor so the full actress-rendering subset
	// (GroupActress, GroupActressName, GroupUnknownActressName, ActressLanguageJA,
	// ActressDelimiter) stays in sync with NFO/path generation. Previously this
	// hand-built only the GroupActress subset, leaving display-title rendering
	// (ApplyDisplayTitleFromSource) reading zero values for the remaining fields.
	nfoNameCfg := nfo.NFONameConfigFromAppConfig(cfg)
	return domainConfigs{
		nfoNameCfg:  nfoNameCfg,
		snCfg:       scanner.ConfigFromAppConfig(cfg),
		matchCfg:    matcher.ConfigFromAppConfig(cfg),
		orgCfg:      organizer.ConfigFromAppConfig(cfg, nfoNameCfg),
		nfoCfg:      nfo.ConfigFromAppConfig(cfg, nfoNameCfg),
		downloadCfg: downloader.ConfigFromAppConfig(cfg, nfoNameCfg),
		scrapeCfg:   scrape.ConfigFromAppConfig(cfg),
		aggCfg:      aggregator.ConfigFromAppConfig(cfg),
		translation: scrape.NewTranslatorFromApp(&cfg.Metadata.Translation),
	}
}

// buildDownloadHTTPCfg constructs the HTTP client configuration for the downloader,
// including proxy resolution and timeout defaults.
func buildDownloadHTTPCfg(cfg *config.Config, downloadCfg *downloader.Config, scrapeCfg *scrape.Config, registry ScraperResolver) downloader.HTTPClientConfig {
	var downloadProxyProfile *models.ProxyProfile
	scrapersProxyCfg := &cfg.Scrapers.Proxy
	if cfg.Output.Download.DownloadProxy.Enabled {
		resolved := models.ResolveScraperProxy(*scrapersProxyCfg, &cfg.Output.Download.DownloadProxy)
		if resolved != nil && resolved.URL != "" {
			downloadProxyProfile = resolved
		}
	}
	globalProxyProfile := models.ResolveGlobalProxy(*scrapersProxyCfg)

	proxyResolvers := registry.CollectDownloadProxyResolvers(scrapeCfg.ScrapersPriority)

	downloadTimeout := downloadCfg.DownloadTimeout
	if downloadTimeout <= 0 {
		downloadTimeout = 60
	}
	return downloader.HTTPClientConfig{
		Timeout:           time.Duration(downloadTimeout) * time.Second,
		DownloadProxy:     downloadProxyProfile,
		ProxyResolvers:    proxyResolvers,
		GlobalProxy:       globalProxyProfile,
		GlobalProxyConfig: scrapersProxyCfg,
	}
}

// buildScanner constructs a scanner from its narrow config.
func buildScanner(fs afero.Fs, snCfg *scanner.Config) scanner.ScannerInterface {
	return scanner.NewScanner(fs, snCfg)
}

// buildScraper constructs the scraper and aggregator from their configs.
// Called by NewFactoryConfigFromRepos during wiring. Not used by WorkflowFactory
// directly — the factory receives constructed objects.
// Per D-8: accepts ContentRepos + ReplacementRepos instead of the full Repositories
// bag, making the dependency surface explicit — callers see exactly which domains
// the scraper needs.
func buildScraper(scrapeCfg *scrape.Config, aggCfg *aggregator.Config, translator scrape.Translator, registry ScraperResolver, httpClient httpclientiface.HTTPClient, fs afero.Fs, content database.ContentRepos, replacement database.ReplacementRepos) (scrape.ScraperInterface, aggregator.AggregatorInterface, error) {
	// MovieRepo is strictly required — it is accessed unconditionally during
	// scraping and persistence. The other repos (ActressRepo, ActressAliasRepo,
	// GenreReplacementRepo, WordReplacementRepo) are passed to the aggregator
	// and scraper constructors, which tolerate nil values:
	//   - scrape.New tolerates nil actressRepo (scrape.go:227, nil-checked
	//     before enrichActressesFromDB) and nil movieRepo (cache.go:18,
	//     nil-checked in tryCache before FindByID).
	//   - The aggregator's GenreProcessor / WordProcessor / AliasResolver handle
	//     nil replacement repos by no-oping the respective processing steps.
	// This allows partial wiring in setups that only exercise input validation
	// without a full DB (e.g. security tests that construct a workflow with nil
	// repos to test request validation paths). Adding hard nil checks here would
	// reject those valid partial-wiring scenarios.
	if content.MovieRepo == nil {
		return nil, nil, fmt.Errorf("buildScraper: movieRepo must not be nil")
	}
	agg := aggregator.New(aggCfg,
		aggregator.NewGenreProcessor(aggCfg.Metadata, replacement.GenreReplacementRepo),
		aggregator.NewWordProcessor(aggCfg.Metadata, replacement.WordReplacementRepo),
		aggregator.NewAliasResolver(aggCfg.Metadata, content.ActressAliasRepo),
	)
	return scrape.New(registry, agg, content.ActressRepo, content.MovieRepo, httpClient, scrapeCfg, translator, fs), agg, nil
}

// buildMatcher constructs a matcher from its narrow config.
func buildMatcher(matchCfg *matcher.Config) (matcher.MatcherInterface, error) {
	m, err := matcher.NewMatcher(matchCfg)
	if err != nil {
		return nil, fmt.Errorf("buildMatcher: %w", err)
	}
	return m, nil
}

// buildOrganizer constructs an organizer from its narrow config and dependencies.
func buildOrganizer(fs afero.Fs, orgCfg *organizer.Config, engine *template.Engine, m matcher.MatcherInterface) organizer.OrganizerInterface {
	return organizer.NewOrganizer(fs, orgCfg, engine, m)
}

// newStrategyResolver creates a StrategyResolverFunc that captures the full
// *organizer.Config for strategy creation. Per DEEP-5: the preview orchestrator
// delegates strategy creation to this closure instead of carrying the full
// config directly. The closure clones the config and overrides the operation
// mode per call, matching the previous inline behavior.
func newStrategyResolver(fs afero.Fs, orgCfg *organizer.Config, m matcher.MatcherInterface, engine *template.Engine) StrategyResolverFunc {
	return func(operationMode operationmode.OperationMode) organizer.OperationStrategy {
		if orgCfg != nil {
			strategyCfg := *orgCfg
			strategyCfg.OperationMode = operationMode
			return organizer.ResolveStrategy(fs, &strategyCfg, m, engine)
		}
		strategyCfg := &organizer.Config{OperationMode: operationMode}
		return organizer.ResolveStrategy(fs, strategyCfg, m, engine)
	}
}

// buildNFO constructs the NFO generator and implementor from their narrow config.
func buildNFO(fs afero.Fs, nfoCfg *nfo.Config, engine *template.Engine) (nfo.GeneratorInterface, nfo.NFOInterface) {
	nfoConfig := *nfoCfg
	nfoConfig.TemplateEngine = engine
	return nfo.NewGenerator(fs, &nfoConfig), nfo.NewNFOImplementor(fs, &nfoConfig, engine)
}

// buildDownloader constructs the media downloader and its HTTP client.
func buildDownloader(httpCfg downloader.HTTPClientConfig, fs afero.Fs, downloadCfg *downloader.Config, engine *template.Engine) (downloader.DownloaderInterface, httpclientiface.HTTPClient, error) {
	httpClient, httpErr := downloader.NewHTTPClient(httpCfg)
	if httpErr != nil {
		return nil, nil, fmt.Errorf("buildDownloader: failed to create HTTP client: %w", httpErr)
	}
	return downloader.NewDownloader(httpClient, fs, downloadCfg, engine), httpClient, nil
}

// buildPosterGenerator constructs the poster generator from its dependencies.
func buildPosterGenerator(fs afero.Fs, scrapeCfg *scrape.Config, httpClient httpclientiface.HTTPClient) poster.PosterGenerator {
	posterManager := poster.NewPosterManager(fs, scrapeCfg.TempDir, httpClient)
	return poster.NewScrapePosterGenerator(posterManager, scrapeCfg.UserAgent, scrapeCfg.Referer)
}

// ScraperConstructionError is returned by NewFactoryConfigFromRepos when scraper
// construction fails. Per S-10: callers that want partial construction (e.g.,
// scan-only workflows) can catch this error explicitly and proceed with a nil
// Scraper in the config.
type ScraperConstructionError struct {
	err error
}

func (e *ScraperConstructionError) Error() string { return e.err.Error() }
func (e *ScraperConstructionError) Unwrap() error { return e.err }

// NewScraperConstructionError wraps a scraper construction error.
func NewScraperConstructionError(err error) *ScraperConstructionError {
	return &ScraperConstructionError{err: err}
}

// buildOrchestratorConfigs constructs the PreviewConfig and ApplyConfig from the
// shared domain configs, file system, matcher, and template engine. Both the error
// and success paths in NewFactoryConfigFromRepos call this, eliminating the
// duplicated construction.
func buildOrchestratorConfigs(cfg *config.Config, dcs domainConfigs, fs afero.Fs, fileMatcher matcher.MatcherInterface, sharedEngine *template.Engine) (PreviewConfig, ApplyConfig) {
	previewCfg := PreviewConfig{
		PathCfg: PreviewPathConfig{
			MediaFormatConfig: dcs.orgCfg.MediaFormatConfig,
		},
		ResolveStrategy: newStrategyResolver(fs, dcs.orgCfg, fileMatcher, sharedEngine),
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
		NFOPerFile:      dcs.nfoCfg.PerFile,
		DisplayTitle:    cfg.Metadata.NFO.Format.DisplayTitle,
		OpMode:          cfg.Output.GetOperationMode(),
		MaxPathLength:   cfg.Output.Template.MaxPathLength,
		Downloads: downloadToggles{
			Poster:      cfg.Output.Download.DownloadPoster,
			Cover:       cfg.Output.Download.DownloadCover,
			Extrafanart: cfg.Output.Download.DownloadExtrafanart,
			Trailer:     cfg.Output.Download.DownloadTrailer,
		},
	}
	applyCfg := ApplyConfig{
		NFONameCfg:   dcs.nfoNameCfg,
		DisplayTitle: cfg.Metadata.NFO.Format.DisplayTitle,
	}
	return previewCfg, applyCfg
}

// NewFactoryConfigFromRepos builds all domain objects from a *config.Config + registry + repos. It delegates
// to named construction steps (extractDomainConfigs, buildMatcher, etc.)
// instead of inlining all construction logic. Each step has a clear contract
// and can be tested independently.
//
// This is the single path through which CLI and API callers construct domain
// objects from a raw config. Callers that want to inject custom domain objects
// for testing should construct workflowFactoryConfig directly.
func NewFactoryConfigFromRepos(cfg *config.Config, registry ScraperResolver, repos database.Repositories) (workflowFactoryConfig, error) {
	if cfg == nil || registry == nil {
		return workflowFactoryConfig{Repositories: repos}, fmt.Errorf("NewFactoryConfigFromRepos: cfg and registry must not be nil")
	}

	// Step 1: Extract narrow configs from monolith.
	dcs := extractDomainConfigs(cfg)

	// Step 2: Build download HTTP client config.
	downloadHTTPCfg := buildDownloadHTTPCfg(cfg, dcs.downloadCfg, dcs.scrapeCfg, registry)

	// Step 3: Construct shared infrastructure.
	fs := afero.NewOsFs()
	sharedEngine := template.NewEngine()

	// Step 4: Construct domain objects via named steps.
	fileMatcher, err := buildMatcher(dcs.matchCfg)
	if err != nil {
		return workflowFactoryConfig{}, err
	}
	fileScanner := buildScanner(fs, dcs.snCfg)
	fileOrganizer := buildOrganizer(fs, dcs.orgCfg, sharedEngine, fileMatcher)
	nfoGenerator, nfoIface := buildNFO(fs, dcs.nfoCfg, sharedEngine)
	mediaDownloader, httpClient, httpErr := buildDownloader(downloadHTTPCfg, fs, dcs.downloadCfg, sharedEngine)
	if httpErr != nil {
		return workflowFactoryConfig{}, httpErr
	}
	posterGen := buildPosterGenerator(fs, dcs.scrapeCfg, httpClient)

	scraperObj, agg, scraperErr := buildScraper(dcs.scrapeCfg, dcs.aggCfg, dcs.translation, registry, httpClient, fs, repos.ContentRepos, repos.ReplacementRepos)
	previewCfg, applyCfg := buildOrchestratorConfigs(cfg, dcs, fs, fileMatcher, sharedEngine)

	baseFC := workflowFactoryConfig{
		MaxFilesPerScan: cfg.API.Security.MaxFilesPerScan,
		DownloadHTTPCfg: downloadHTTPCfg,
		PreviewCfg:      previewCfg,
		ApplyCfg:        applyCfg,
		Fs:              fs,
		TemplateEngine:  sharedEngine,

		Matcher:      fileMatcher,
		Scanner:      fileScanner,
		ScannerCfg:   *dcs.snCfg,
		Organizer:    fileOrganizer,
		NFOGenerator: nfoGenerator,
		NFOIface:     nfoIface,
		Downloader:   mediaDownloader,
		HTTPClient:   httpClient,
		PosterGen:    posterGen,
		Repositories: repos,
		AllowRevert:  dcs.orgCfg.AllowRevert,
		NFOCfg:       dcs.nfoCfg,
	}

	if scraperErr != nil {
		// Per S-10: return ScraperConstructionError so callers that want partial
		// construction can catch it explicitly. The rest of the config is still
		// assembled so that scan-only workflows (which don't need a scraper) work.
		baseFC.Scraper = nil
		baseFC.Aggregator = nil
		return baseFC, NewScraperConstructionError(fmt.Errorf("scraper construction failed: %w", scraperErr))
	}

	baseFC.Scraper = scraperObj
	baseFC.Aggregator = agg
	return baseFC, nil
}

// WorkflowFactory caches the shared dependency sub-graph and produces per-call Workflow
// instances. the shared sub-graph (scraper, matcher, organizer, downloader,
// NFO generator, template engine, scanner, poster generator) is read-only after construction.
// Only the RevertLog varies per call (it depends on JobID), so caching is safe.
//
// The aggregator's replacement caches (genre, word, alias) can be reloaded in-place
// via ReloadReplacementCaches without destroying the factory — this avoids the cold-start
// penalty of rebuilding the entire dependency graph on every genre/word mutation.
//
// Usage:
//   - API layer: construct once, cache on APIDeps, invalidate on config reload.
//   - CLI layer: uses NewWorkflowFactory + .NewWorkflow/.NewScrapeOnlyWorkflow accessors.
type WorkflowFactory struct {
	fc workflowFactoryConfig

	// Cached sub-orchestrators — assembled once, reused per NewWorkflow call.
	// only the apply orchestrator's RevertLog varies per call;
	// all others are identical across invocations.
	cachedScrape    scrapeOrchestrator
	cachedCompare   compareOrchestrator
	cachedPreview   previewOrchestrator
	cachedScanMatch scanAndMatchOrchestrator
}

// NewWorkflowFactory constructs a WorkflowFactory from a workflowFactoryConfig
// with pre-built domain objects. Per DEEP-8: the factory supports all workflow
// modes (NewWorkflow, NewScrapeOnlyWorkflow, NewScanOnlyWorkflow) from a single
// cached instance. Sub-orchestrators are built conditionally based on which
// dependencies are available — nil deps result in noOp placeholders.
//
// Required: Matcher (minimum for scan-only mode). Other deps are required only
// by specific workflow modes — call NewWorkflow, NewScrapeOnlyWorkflow, or
// NewScanOnlyWorkflow to get mode-specific validation errors.
func NewWorkflowFactory(fc workflowFactoryConfig) (*WorkflowFactory, error) {
	// Matcher is the bare minimum — even scan-only needs it.
	if fc.Matcher == nil {
		return nil, fmt.Errorf("dependencies: matcher must not be nil")
	}

	// Default Logger to GlobalLogger if not injected — must run BEFORE cached
	// sub-orchestrator construction so all sub-orchs receive a non-nil logger.
	if fc.Logger == nil {
		fc.Logger = logging.GlobalLogger()
	}

	// Conditionally build sub-orchestrators based on available deps.
	// Per DEEP-8: a single factory serves all modes. Sub-orchestrators that
	// require nil deps get noOp placeholders, and the per-mode New* methods
	// validate that the orchestrators they need are actually configured.
	var cachedScrape scrapeOrchestrator
	if fc.Scraper != nil {
		cachedScrape = newScrapeOrchestrator(fc.Scraper, fc.MovieRepo, fc.PreviewCfg.DisplayTitle, fc.TemplateEngine, fc.ApplyCfg.NFONameCfg, fc.Logger)
	} else {
		cachedScrape = noOpScrapeOrchestrator{}
	}

	var cachedCompare compareOrchestrator
	if fc.Scraper != nil && fc.NFOIface != nil {
		cachedCompare = newCompareOrchestrator(fc.Fs, fc.NFOIface, fc.Scraper, fc.Logger)
	} else {
		cachedCompare = noOpCompareOrchestrator{}
	}

	var cachedPreview previewOrchestrator
	if fc.NFOIface != nil {
		cachedPreview = newPreviewOrchestrator(fc.Fs, fc.Matcher, fc.PreviewCfg, fc.ApplyCfg.NFONameCfg, fc.TemplateEngine, fc.NFOIface, fc.Logger)
	} else {
		cachedPreview = noOpPreviewOrchestrator{}
	}

	cachedScanMatch := newScanAndMatchOrchestrator(fc.Scanner, fc.ScannerCfg, fc.Fs, fc.Matcher, fc.MaxFilesPerScan, fc.Logger)

	return &WorkflowFactory{
		fc:              fc,
		cachedScrape:    cachedScrape,
		cachedCompare:   cachedCompare,
		cachedPreview:   cachedPreview,
		cachedScanMatch: cachedScanMatch,
	}, nil
}

// NewWorkflow produces a full Workflow with a fresh RevertLog for the given jobID.
// The shared sub-graph (scraper, organizer, downloader, etc.) is reused from the cache.
//
// Per DEEP-8: validates that all sub-orchestrators required for a full workflow
// are configured. Returns an error if the factory was constructed without
// the necessary dependencies (e.g., nil Scraper, Organizer, Downloader, or repos).
func (f *WorkflowFactory) NewWorkflow(jobID string) (WorkflowInterface, error) {
	// Per DEEP-8: validate that all sub-orchestrators are real (not noOp).
	if _, ok := f.cachedScrape.(noOpScrapeOrchestrator); ok {
		return nil, fmt.Errorf("workflow: scraper must not be nil for full workflow")
	}
	if f.fc.Organizer == nil {
		return nil, fmt.Errorf("workflow: organizer must not be nil for full workflow")
	}
	if f.fc.Downloader == nil {
		return nil, fmt.Errorf("workflow: downloader must not be nil for full workflow")
	}
	if f.fc.MovieRepo == nil {
		return nil, fmt.Errorf("workflow: movieRepo must not be nil for full workflow")
	}
	if f.fc.BatchFileOpRepo == nil {
		return nil, fmt.Errorf("workflow: batchFileOpRepo must not be nil for full workflow")
	}

	revertLogCfg := RevertLogConfig{
		AllowRevert: f.fc.AllowRevert,
		NFOCfg:      f.fc.NFOCfg,
	}
	revertLog := NewRevertLogFromConfig(f.fc.BatchFileOpRepo, &revertLogCfg, jobID, f.fc.Fs, f.fc.TemplateEngine, f.fc.NFOIface, f.fc.Logger)

	// Construct a fresh apply orchestrator per workflow with the revert log
	// already wired in. the apply orchestrator is not cached
	// because RevertLog depends on JobID — each workflow gets its own instance.
	apply := newApplyOrchestrator(f.fc.Fs, f.fc.Organizer, f.fc.Downloader, f.fc.NFOGenerator, f.fc.NFOIface, f.fc.ApplyCfg, f.fc.TemplateEngine, revertLog, f.fc.MovieTagRepo, f.fc.Logger)

	wf := &Workflow{
		scrape:    f.cachedScrape,
		apply:     apply,
		compare:   f.cachedCompare,
		preview:   f.cachedPreview,
		scanMatch: f.cachedScanMatch,
	}

	return wf, nil
}

// NewScrapeOnlyWorkflow produces a scrape-only Workflow reusing the cached sub-graph.
// No RevertLog is needed for scrape-only operations.
//
// Per DEEP-8: validates that the scrape and compare sub-orchestrators are
// configured. Returns an error if the factory was constructed without Scraper.
func (f *WorkflowFactory) NewScrapeOnlyWorkflow() (WorkflowInterface, error) {
	if _, ok := f.cachedScrape.(noOpScrapeOrchestrator); ok {
		return nil, fmt.Errorf("scrape-only workflow: scraper must not be nil")
	}

	return &Workflow{
		scrape:    f.cachedScrape,
		apply:     noOpApplyOrchestrator{},
		compare:   f.cachedCompare,
		preview:   noOpPreviewOrchestrator{},
		scanMatch: f.cachedScanMatch,
	}, nil
}

// NewScanOnlyWorkflow produces a scan-only Workflow reusing the cached scanner and matcher.
//
// Per DEEP-8: scan-only mode only requires the scanMatch sub-orchestrator, which
// is always built by NewWorkflowFactory (Matcher is the minimum required dep).
func (f *WorkflowFactory) NewScanOnlyWorkflow() WorkflowInterface {

	return &Workflow{
		scrape:    noOpScrapeOrchestrator{},
		apply:     noOpApplyOrchestrator{},
		compare:   noOpCompareOrchestrator{},
		preview:   noOpPreviewOrchestrator{},
		scanMatch: f.cachedScanMatch,
	}
}

// PosterGen returns the cached PosterGenerator from the shared sub-graph.
// Per W-3: the API layer retrieves the cached instance from the factory instead of
// constructing its own, collapsing 3 construction sites to 1.
func (f *WorkflowFactory) PosterGen() poster.PosterGenerator {
	return f.fc.PosterGen
}

// Matcher returns the cached MatcherInterface from the shared sub-graph.
// Used by callers that need the matcher alongside the workflow (e.g., CLI Bootstrap).
func (f *WorkflowFactory) Matcher() matcher.MatcherInterface {
	return f.fc.Matcher
}

// Scanner returns the cached ScannerInterface from the shared sub-graph.
// Used by callers that need the scanner alongside the workflow (e.g., CLI Bootstrap).
func (f *WorkflowFactory) Scanner() scanner.ScannerInterface {
	return f.fc.Scanner
}

// MaxFilesPerScan returns the per-scan file limit the factory was built from.
// Exposed for hot-reload consistency diagnostics so callers can verify a cached
// factory's config-derived value matches the current APIConfig snapshot.
func (f *WorkflowFactory) MaxFilesPerScan() int {
	return f.fc.MaxFilesPerScan
}

// ReloadReplacementCaches refreshes the aggregator's genre, word, and alias
// replacement caches in-place, without destroying the WorkflowFactory or
// rebuilding the scraper, matcher, organizer, downloader, or NFO generator.
// Use this on the hot path when a genre/word/alias mutation must be visible
// to the next aggregation call — avoids the cold-start penalty of a full
// factory rebuild via InvalidateWorkflowCaches.
//
// Safe to call concurrently with ongoing NewWorkflow/NewScrapeOnlyWorkflow
// calls: each sub-processor owns its own mutex and the cache swap is atomic.
func (f *WorkflowFactory) ReloadReplacementCaches(ctx context.Context) {
	if f == nil || f.fc.Aggregator == nil {
		return
	}
	f.fc.Aggregator.ReloadReplacementCaches(ctx)
}
