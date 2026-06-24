package core

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
)

// BootstrapAPIWithOpts is a variant of BootstrapAPI that accepts a
// commandutil.DependenciesOptions so callers can inject alternate
// dependencies — most importantly an alternate ScraperRegistry seeded with
// the test-only "e2emock" scraper for full-stack E2E tests.
//
// All other behavior is identical to BootstrapAPI. Production callers should
// keep using BootstrapAPI; this variant exists so E2E binaries can register
// a deterministic mock scraper at the scraper seam while exercising the real
// API server, real worker pipeline, real result tracker, real DB, and real
// HTTP frontend/proxy stack end-to-end.
func BootstrapAPIWithOpts(cfg *config.Config, configFile string, auth commandutil.AuthProvider, depsOpts *commandutil.DependenciesOptions) (*APIDeps, *APIRuntime, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("config cannot be nil")
	}
	if depsOpts == nil {
		return BootstrapAPI(cfg, configFile, auth)
	}
	coreDeps, err := commandutil.NewDependenciesWithOptions(cfg, depsOpts)
	if err != nil {
		return nil, nil, err
	}
	return bootstrapAPIDeps(cfg, configFile, auth, coreDeps)
}
