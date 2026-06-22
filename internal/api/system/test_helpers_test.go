package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.APIDeps {
	deps := testkit.CreateTestDeps(t, cfg, configFile)
	return deps
}

// systemDepsFromCore returns the same *core.APIDeps — all handler tests use the unified type.
func systemDepsFromCore(d *core.APIDeps) *core.APIDeps {
	return d
}

// newTestDeps creates a core.APIDeps for unit tests with the given config.
func newTestDeps(cfg *config.Config, opts ...func(*core.APIDeps)) *core.APIDeps {
	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{},
	}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)
	for _, opt := range opts {
		opt(deps)
	}
	return deps
}

// withRegistry sets the ScraperRegistry on core.APIDeps.
func withRegistry(reg *scraperutil.ScraperRegistry) func(*core.APIDeps) {
	return func(d *core.APIDeps) {
		core.NewAPIRuntime(d).ReplaceReloadable(d.CoreDeps.GetConfig(), reg)
	}
}
