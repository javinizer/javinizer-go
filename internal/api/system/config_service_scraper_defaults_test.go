package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigUpdateService_ScraperDefaults_ReturnsRegisteredDefaults(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})
	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "javdb",
		Defaults: models.ScraperSettings{Enabled: false, RateLimit: 1000},
	})

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	defaults := svc.scraperDefaults()

	assert.Contains(t, defaults, "r18dev")
	assert.Equal(t, "en", defaults["r18dev"].Language)
	assert.Contains(t, defaults, "javdb")
	assert.False(t, defaults["javdb"].Enabled)
}

func TestConfigUpdateService_SaveSparse_PrunesDefaultOnlyScraperBlocks(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})
	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "javdb",
		Defaults: models.ScraperSettings{Enabled: false, RateLimit: 1000},
	})

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true},
		"javdb":  {Enabled: true},
	}

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.reload = func(rt *core.APIRuntime, d *core.APIDeps, cfg *config.Config) error { return nil }

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	data, err := afero.ReadFile(afero.NewOsFs(), tempConfigFile)
	require.NoError(t, err)
	persisted := string(data)

	assert.NotContains(t, persisted, "r18dev:")
	assert.Contains(t, persisted, "javdb:")
	assert.Contains(t, persisted, "enabled: true")
}

func TestConfigUpdateService_SaveSparse_DefaultTrueExplicitFalsePreserved(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: false},
	}

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.reload = func(rt *core.APIRuntime, d *core.APIDeps, cfg *config.Config) error { return nil }

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	data, err := afero.ReadFile(afero.NewOsFs(), tempConfigFile)
	require.NoError(t, err)
	persisted := string(data)

	assert.Contains(t, persisted, "r18dev:")
	assert.Contains(t, persisted, "enabled: false")
}

func TestConfigUpdateService_SaveSparse_DefaultFalseExplicitTruePreserved(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "javdb",
		Defaults: models.ScraperSettings{Enabled: false, RateLimit: 1000},
	})

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"javdb": {Enabled: true},
	}

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.reload = func(rt *core.APIRuntime, d *core.APIDeps, cfg *config.Config) error { return nil }

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	data, err := afero.ReadFile(afero.NewOsFs(), tempConfigFile)
	require.NoError(t, err)
	persisted := string(data)

	assert.Contains(t, persisted, "javdb:")
	assert.Contains(t, persisted, "enabled: true")
	assert.NotContains(t, persisted, "rate_limit: 1000")
}
