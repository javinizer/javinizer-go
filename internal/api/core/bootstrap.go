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
	jobStore := worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, cfg.System.TempDir, sharedEngine, nil)
	eventEmitter := eventlog.NewEmitter(repos.EventRepo)
	reverter := history.NewReverter(afero.NewOsFs(), repos.BatchFileOpRepo)

	apiDeps := &APIDeps{
		CoreDeps:     coreDeps,
		ConfigFile:   configFile,
		Repos:        repos,
		EventEmitter: eventEmitter,
		Reverter:     reverter,
		JobStore:     jobStore,
		Auth:         auth,
		TokenStore:   NewTokenStore(),
	}

	// Create APIRuntime which owns the mutable state and sets the back-reference
	// on apiDeps. Must be done before SetConfig/InitAPIConfig since those now
	// delegate to APIRuntime.
	rt := NewAPIRuntime(apiDeps)

	rt.SetConfig(cfg)
	rt.EnsureRuntime()

	// Start periodic cleanup of stale temp poster directories.
	// The stop channel is stored on the APIRuntime for graceful shutdown.
	rt.SetTempCleanupStop(jobStore.StartStaleTempCleanup())

	if err := eventEmitter.EmitSystemEvent(context.Background(), "server", "Javinizer API server initialized", models.SeverityInfo, map[string]any{
		"host": cfg.Server.Host,
		"port": cfg.Server.Port,
	}); err != nil {
		logging.Warnf("Failed to emit server startup event: %v", err)
	}

	return apiDeps, rt, nil
}
