package system

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

type actressOnlyScraper struct{ name string }

func (s *actressOnlyScraper) Name() string { return s.name }
func (s *actressOnlyScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *actressOnlyScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *actressOnlyScraper) IsEnabled() bool                                { return true }
func (s *actressOnlyScraper) Config() *models.ScraperSettings                { return &models.ScraperSettings{} }
func (s *actressOnlyScraper) Close() error                                   { return nil }
func (s *actressOnlyScraper) SupportsMovieSearch() bool                      { return false }
func (s *actressOnlyScraper) ResolveActressMetadata(context.Context, models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{}, nil
}

type movieScraper struct{ actressOnlyScraper }

func (s *movieScraper) SupportsMovieSearch() bool { return true }

// An actress-field override listing only actress-only resolvers can never
// produce any cast during movie aggregation — reject before saving.
func TestValidateActressPriorityCapability(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&actressOnlyScraper{name: "minnanoav"})
	registry.RegisterInstance(&movieScraper{actressOnlyScraper{name: "dmm"}})
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry}}

	priority := func(list []string) *config.Config {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Metadata.Priority.Fields = map[string][]string{"actress": list}
		return cfg
	}

	require.NoError(t, validatePriorityFieldCapabilities(deps, priority([]string{"minnanoav"})))
	require.NoError(t, validatePriorityFieldCapabilities(deps, priority([]string{"minnanoav", "dmm"})))
	require.NoError(t, validatePriorityFieldCapabilities(deps, priority([]string{"__skip__"})))
	require.NoError(t, validatePriorityFieldCapabilities(deps, priority([]string{"unknown-only"})))
} // Actress-only resolvers are valid on the actress field: the sync manager
// consumes ActressFieldPriority and invokes ActressMetadataResolver directly.
func TestConfigUpdateService_ValidateAndApply_ActressOnlyPriorityAccepted(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	deps.CoreDeps.ScraperRegistry.RegisterInstance(&actressOnlyScraper{name: "minnanoav"})

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Metadata.Priority.Fields = map[string][]string{"actress": {"minnanoav"}}

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.reload = func(rt *core.APIRuntime, d *core.APIDeps, cfg *config.Config) error { return nil }
	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)
}

// The same listing on a non-actress field must reject as well.
func TestConfigUpdateService_ValidateAndApply_ActressOnlyOnTitleRejected(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	deps.CoreDeps.ScraperRegistry.RegisterInstance(&actressOnlyScraper{name: "minnanoav"})

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Metadata.Priority.Fields = map[string][]string{"title": {"minnanoav"}}

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.reload = func(rt *core.APIRuntime, d *core.APIDeps, cfg *config.Config) error { return nil }
	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err, "title: [minnanoav] can never gain data")
}
