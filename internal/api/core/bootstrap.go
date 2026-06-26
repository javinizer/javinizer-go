package core

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

// BootstrapAPI initializes the full API dependency stack from a config:
// CoreDeps (database, scraper registry, seeded word replacements) → API-specific services.
// The AuthProvider must be constructed by the caller (it depends on api/auth which
// cannot be imported here due to the import cycle: api/auth → api/core).
// Callers should defer deps.CoreDeps.DB.Close() to release resources.
// Returns the APIRuntime so callers can call rt.Shutdown() for graceful
// termination of background goroutines (e.g. temp cleanup).
func BootstrapAPI(cfg *config.Config, configFile string, auth commandutil.AuthProvider) (*APIDeps, *APIRuntime, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("config cannot be nil")
	}

	coreDeps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return nil, nil, err
	}

	return bootstrapAPIDeps(cfg, configFile, auth, coreDeps)
}

// bootstrapAPIDeps performs the post-CoreDeps wiring (repos, jobStore,
// auth, runtime, temp cleanup, startup event). Shared between BootstrapAPI
// (constructs CoreDeps from config) and BootstrapAPIWithOpts (uses the
// caller-supplied CoreDeps via DependenciesOptions). E2E binaries use the
// latter to inject a deterministic mock scraper at the scraper seam while
// exercising the same wiring path as production.
func bootstrapAPIDeps(cfg *config.Config, configFile string, auth commandutil.AuthProvider, coreDeps *commandutil.CoreDeps) (*APIDeps, *APIRuntime, error) {
	logging.Infof("Registered %d scrapers", len(coreDeps.ScraperRegistry.GetAllInstances()))

	repos := coreDeps.DB.Repositories()

	sharedEngine := template.NewEngine()
	// Initialize ONE filesystem and thread it through the DI seam (JobStore,
	// Reverter, APIDeps.Fs) so there is a single afero.Fs default instead of
	// three split ones (previously JobStore got nil, Reverter built its own
	// OsFs, and APIDeps.Fs was left unset). GetFs() falls back to OsFs when
	// nil, so this is behavior-preserving for existing callers.
	fs := afero.NewOsFs()
	jobStore := worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, cfg.System.TempDir, sharedEngine, fs)
	eventEmitter := eventlog.NewEmitter(repos.EventRepo)
	reverter := history.NewReverter(fs, repos.BatchFileOpRepo)

	apiDeps := &APIDeps{
		CoreDeps:     coreDeps,
		ConfigFile:   configFile,
		Repos:        repos,
		EventEmitter: eventEmitter,
		Reverter:     reverter,
		JobStore:     jobStore,
		Auth:         auth,
		TokenStore:   NewTokenStore(),
		Fs:           fs,
	}

	// Create APIRuntime which owns the mutable state and sets the back-reference
	// on apiDeps. Must be done before SetConfig/InitAPIConfig since those now
	// delegate to APIRuntime.
	rt := NewAPIRuntime(apiDeps)

	rt.SetConfig(cfg)
	rt.EnsureRuntime()

	// Temp poster cleanup is intentionally NOT started automatically.
	// Running CleanupStaleTempDirs on startup (or on a periodic ticker) wipes
	// temp poster artifacts for terminal/orphaned jobs, but the DB still holds
	// their cropped_poster_url — leaving a disentangled state where the list
	// view renders broken thumbnails after every restart. This was a regression
	// vs v0.3.15-alpha, which only cleaned temp dirs on explicit job deletion.
	// Cleanup therefore happens only via DeleteJob -> CleanJobTempDir, which
	// removes the job record and its temp dir together.

	if err := eventEmitter.EmitSystemEvent(context.Background(), "server", "Javinizer API server initialized", models.SeverityInfo, map[string]any{
		"host": cfg.Server.Host,
		"port": cfg.Server.Port,
	}); err != nil {
		logging.Warnf("Failed to emit server startup event: %v", err)
	}

	return apiDeps, rt, nil
}
