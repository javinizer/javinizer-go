package commandutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// AuthProvider is the minimal auth contract used by API handlers.
type AuthProvider interface {
	SessionTTL() time.Duration
	IsInitialized() bool
	AuthenticateSession(sessionID string) (string, error)
	Setup(username, password string) error
	Login(username, password string, rememberMe bool) (string, error)
	Logout(sessionID string)
	ValidateToken(ctx context.Context, tokenHash string) (string, error)
	UpdateTokenLastUsed(ctx context.Context, tokenID string) error
	GetEnv(key string) string
}

// CoreDepsReader is the read-only interface for CoreDeps. API handlers and
// other consumers that only need to read config, registry, and logger
// should accept this interface instead of *CoreDeps. The concrete *CoreDeps
// satisfies the interface implicitly.
type CoreDepsReader interface {
	GetConfig() *config.Config
	GetRegistry() *scraperutil.ScraperRegistry
	GetLogger() logging.Logger
	HasConfig() bool
}

// CoreDeps contains shared dependencies that both CLI and API commands need.
type CoreDeps struct {
	DB              *database.DB
	ScraperRegistry *scraperutil.ScraperRegistry

	// Logger is the structured logger seam. Defaults to GlobalLogger() when nil.
	// Inject a mock/spy in tests; production code leaves this nil for the global default.
	Logger logging.Logger

	// config is the single source of truth for the application config.
	// Both CLI (set once at construction) and API (hot-reloaded via
	// SetConfig/ReplaceReloadable) use this atomic pointer — there is
	// no separate Config field fallback.
	mu     sync.RWMutex
	config atomic.Pointer[config.Config]
}

// DependenciesOptions allows optional dependency injection for testing.
// Fields left nil will be initialized with real implementations.
type DependenciesOptions struct {
	DB              *database.DB                 // Optional: injected database (for tests)
	ScraperRegistry *scraperutil.ScraperRegistry // Optional: injected registry (for tests)
	Ctx             context.Context              // Optional: context for startup operations (migrations); nil uses Background
	Logger          logging.Logger               // Optional: injected logger (for tests); nil uses GlobalLogger()
}

// NewDependencies creates a new CoreDeps instance from a config.
// It initializes the database connection and scraper registry.
// This is the production constructor - for testable constructor see NewDependenciesWithOptions.
func NewDependencies(cfg *config.Config) (*CoreDeps, error) {
	return NewDependenciesWithOptions(cfg, nil)
}

// NewDependenciesWithOptions creates a new CoreDeps instance with optional dependency injection.
// If opts is nil or opts fields are nil, real implementations are created.
// If opts fields are non-nil, injected dependencies are used (for testing).
func NewDependenciesWithOptions(cfg *config.Config, opts *DependenciesOptions) (*CoreDeps, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	deps := &CoreDeps{}
	deps.config.Store(cfg)

	// Use injected DB or create real one
	ownsDB := false
	if opts != nil && opts.DB != nil {
		deps.DB = opts.DB
	} else {
		// Ensure database directory exists before opening database
		// This prevents "unable to open database file" errors on clean installs
		dbDir := filepath.Dir(cfg.Database.DSN)
		if err := os.MkdirAll(dbDir, config.DirPerm); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}

		// Initialize database
		db, err := database.New(&database.Config{
			Type:     cfg.Database.Type,
			DSN:      cfg.Database.DSN,
			LogLevel: cfg.Database.LogLevel,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize database: %w", err)
		}

		// Run startup migrations before initializing dependent services.
		ctx := context.Background()
		if opts != nil && opts.Ctx != nil {
			ctx = opts.Ctx
		}
		if err := db.RunMigrationsOnStartup(ctx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}

		// Seed default word replacements after migrations so both CLI and API
		// paths get them. Previously this was only called in the API path,
		// which was a latent bug in the CLI path.
		database.SeedDefaultWordReplacements(ctx, database.NewWordReplacementRepository(db))

		deps.DB = db
		ownsDB = true
	}

	// Use injected registry or create real one
	if opts != nil && opts.ScraperRegistry != nil {
		deps.ScraperRegistry = opts.ScraperRegistry
	} else {
		reg := scraperutil.NewScraperRegistry()
		scraper.RegisterAll(reg)

		// Set up config resolver for scraper normalization.
		// This populates cfg.Scrapers.Overrides from the registered scraper defaults.
		if err := cfg.Scrapers.Finalize(reg); err != nil {
			// Only close a DB we created here; never close an injected one.
			if ownsDB {
				_ = deps.DB.Close()
			}
			return nil, fmt.Errorf("failed to finalize scraper config: %w", err)
		}

		registry, err := scraper.NewDefaultScraperRegistryFrom(reg, scraper.ScraperRegistryConfigFromApp(cfg), database.NewContentIDMappingRepository(deps.DB))
		if err != nil {
			// Only close a DB we created here; never close an injected one
			// (avoids leaking or double-closing injected handles).
			if ownsDB {
				_ = deps.DB.Close()
			}
			return nil, fmt.Errorf("failed to initialize scraper registry: %w", err)
		}
		deps.ScraperRegistry = registry
	}

	// Use injected logger or default to GlobalLogger
	if opts != nil && opts.Logger != nil {
		deps.Logger = opts.Logger
	} else {
		deps.Logger = logging.GlobalLogger()
	}

	return deps, nil
}

// GetLogger returns the Logger seam, falling back to GlobalLogger if none was injected.
func (d *CoreDeps) GetLogger() logging.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return logging.GlobalLogger()
}

// GetConfig returns the current config (thread-safe).
// Reads from the atomic pointer — the single source of truth for both
// CLI (set once at construction) and API (hot-reloaded) paths.
// Panics if no config has been set — this indicates a construction bug
// (NewDependenciesWithOptions already validates cfg != nil).
func (d *CoreDeps) GetConfig() *config.Config {
	cfg := d.config.Load()
	if cfg == nil {
		panic("commandutil: GetConfig() called with nil config — this is a construction bug; use NewDependenciesWithOptions to construct CoreDeps with a valid config")
	}
	return cfg
}

// HasConfig reports whether a config has been set.
// Use this for early initialization checks where the absence of config
// is expected (e.g., APIRuntime.InitAPIConfig) rather than a bug.
func (d *CoreDeps) HasConfig() bool {
	return d.config.Load() != nil
}

// SetConfig atomically sets the configuration (thread-safe).
// Panics if cfg is nil — this indicates a programming error.
func (d *CoreDeps) SetConfig(cfg *config.Config) {
	if cfg == nil {
		panic("commandutil: SetConfig() called with nil config — this is a programming error")
	}
	d.config.Store(cfg)
}

// GetRegistry returns the current scraper registry (thread-safe).
// Panics if ScraperRegistry is nil — this indicates a construction bug
// (NewDependenciesWithOptions always initializes the registry).
func (d *CoreDeps) GetRegistry() *scraperutil.ScraperRegistry {
	d.mu.RLock()
	reg := d.ScraperRegistry
	d.mu.RUnlock()

	if reg == nil {
		panic("commandutil: GetRegistry() called with nil ScraperRegistry — this is a construction bug; use NewDependenciesWithOptions to construct CoreDeps properly")
	}
	return reg
}

// ReplaceReloadable swaps config and registry atomically.
// Config is stored via atomic.Pointer INSIDE the write lock so that
// GetConfig (which reads the same atomic pointer) cannot see new config
// while GetRegistry (mutex-protected) still returns old values.
// This prevents a split-brain window during hot-reload.
// Panics if cfg is nil — this indicates a programming error.
func (d *CoreDeps) ReplaceReloadable(cfg *config.Config, registry *scraperutil.ScraperRegistry) {
	if cfg == nil {
		panic("commandutil: ReplaceReloadable() called with nil config — this is a programming error")
	}
	if registry == nil {
		panic("commandutil: ReplaceReloadable() called with nil registry — this is a programming error")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ScraperRegistry = registry
	d.config.Store(cfg)
}

// Close closes all resources held by the CoreDeps.
// Should be called when done using the CoreDeps.
func (d *CoreDeps) Close() error {
	if d.DB != nil {
		return d.DB.Close()
	}
	return nil
}

// bootstrapResult holds the fully initialized dependency stack:
// CoreDeps (DB, registry) plus workflow components (Workflow, Matcher, Scanner).
type bootstrapResult struct {
	*CoreDeps
	*workflow.WorkflowComponents
}

// bootstrapMode selects which workflow construction path the factory uses.
type bootstrapMode int

const (
	bootstrapModeFull       bootstrapMode = iota // calls factory.NewWorkflow("")
	bootstrapModeScrapeOnly                      // calls factory.NewScrapeOnlyWorkflow()
)

// bootstrapWorkflow runs the shared dependency bootstrap + factory construction
// preamble used by both Bootstrap (full) and BootstrapScrapeOnly (scrape-only).
// Returns a bootstrapResult with all WorkflowComponents fields populated from
// the factory accessors — the cached sub-graph is identical regardless of mode.
func bootstrapWorkflow(cfg *config.Config, mode bootstrapMode) (*bootstrapResult, error) {
	deps, err := NewDependencies(cfg)
	if err != nil {
		return nil, err
	}
	fc, fcErr := workflow.NewFactoryConfigFromRepos(cfg, deps.ScraperRegistry, deps.DB.Repositories())
	if fcErr != nil {
		_ = deps.Close()
		return nil, fcErr
	}
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		_ = deps.Close()
		return nil, err
	}

	var wf workflow.WorkflowInterface
	if mode == bootstrapModeScrapeOnly {
		wf, err = factory.NewScrapeOnlyWorkflow()
	} else {
		wf, err = factory.NewWorkflow("")
	}
	if err != nil {
		_ = deps.Close()
		return nil, err
	}

	return &bootstrapResult{CoreDeps: deps, WorkflowComponents: &workflow.WorkflowComponents{
		Workflow:  wf,
		Matcher:   factory.Matcher(),
		Scanner:   factory.Scanner(),
		PosterGen: factory.PosterGen(),
	}}, nil
}

// Bootstrap initializes the full dependency stack from a config:
// CoreDeps (database, scraper registry) → WorkflowComponents (workflow, matcher, scanner).
// Callers should defer result.Close() to release resources.
func Bootstrap(cfg *config.Config) (*bootstrapResult, error) {
	return bootstrapWorkflow(cfg, bootstrapModeFull)
}

// BootstrapScrapeOnly initializes the dependency stack with a scrape-only workflow.
// Use this for commands that only need metadata scraping (no organization/downloads).
func BootstrapScrapeOnly(cfg *config.Config) (*bootstrapResult, error) {
	return bootstrapWorkflow(cfg, bootstrapModeScrapeOnly)
}
