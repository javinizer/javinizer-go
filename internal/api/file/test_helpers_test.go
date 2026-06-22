package file

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

func newTestDepsFromConfig(cfg *config.Config) *core.APIDeps {
	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: scraperutil.NewScraperRegistry(),
		},
	}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)
	return deps
}
