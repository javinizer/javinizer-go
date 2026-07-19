package system

import (
	"encoding/json"
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

func TestConfigUpdateService_ValidateAndApply_InvalidEffectiveScraperRejectedBeforeSave(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})

	jsonBody := `{"scrapers":{"priority":["r18dev"],"r18dev":{"rate_limit":-1}}}`
	newCfg := config.DefaultConfig(nil, nil)
	require.NoError(t, json.Unmarshal([]byte(jsonBody), newCfg))

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.reload = func(rt *core.APIRuntime, d *core.APIDeps, cfg *config.Config) error { return nil }

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit")

	data, readErr := afero.ReadFile(afero.NewOsFs(), tempConfigFile)
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "rate_limit: -1", "invalid config must not be persisted")
}

func TestConfigUpdateService_ValidateAndApply_ExplicitFalseInvalidScraperAcceptedAndPersisted(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})

	jsonBody := `{"scrapers":{"priority":["r18dev"],"r18dev":{"enabled":false,"rate_limit":-1}}}`
	newCfg := config.DefaultConfig(nil, nil)
	require.NoError(t, json.Unmarshal([]byte(jsonBody), newCfg))

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.reload = func(rt *core.APIRuntime, d *core.APIDeps, cfg *config.Config) error { return nil }

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	data, readErr := afero.ReadFile(afero.NewOsFs(), tempConfigFile)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "rate_limit: -1", "explicit-false invalid value is preserved")
	assert.Contains(t, string(data), "enabled: false")
}

func TestConfigUpdateService_ValidateAndApply_ScraperLanguageCanonicalAcrossDiskAndRuntime(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})

	jsonBody := `{"scrapers":{"priority":["r18dev"],"r18dev":{"language":" JA "}}}`
	newCfg := config.DefaultConfig(nil, nil)
	require.NoError(t, json.Unmarshal([]byte(jsonBody), newCfg))

	rt := testkit.GetTestRuntime(deps)
	svc := NewConfigUpdateService(rt, tempConfigFile)
	svc.reload = func(*core.APIRuntime, *core.APIDeps, *config.Config) error { return nil }

	require.NoError(t, svc.ValidateAndApply(oldCfg, newCfg, nil))

	loaded, err := config.Load(tempConfigFile)
	require.NoError(t, err)
	assert.Equal(t, "ja", loaded.Scrapers.Overrides["r18dev"].Language, "saved disk config must hold the canonical language")

	diskSnap := rt.DiskConfigSnapshot()
	require.NotNil(t, diskSnap)
	assert.Equal(t, "ja", diskSnap.Scrapers.Overrides["r18dev"].Language, "disk snapshot must hold the canonical language")

	runtimeCfg := deps.CoreDeps.GetConfig()
	require.NotNil(t, runtimeCfg)
	assert.Equal(t, "ja", runtimeCfg.Scrapers.Overrides["r18dev"].Language, "active runtime config must hold the canonical language")
}
