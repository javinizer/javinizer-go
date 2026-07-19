package system

import (
	"errors"
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

func TestConfigUpdateService_SparseContextCollision_ProxyNameRegistered(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Server.Host = "0.0.0.0"

	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{Name: "proxy"})

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.saveSparse = func(*config.Config, string, config.SparseSaveContext) error {
		t.Fatal("saveSparse must not be invoked when sparse context building fails")
		return nil
	}

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)

	var valErr *validationError
	assert.ErrorAs(t, err, &valErr)
	assert.Contains(t, err.Error(), "collides with static config key")
}

func TestConfigUpdateService_SaveSparseFailure_ReturnsPersistError(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Server.Host = "0.0.0.0"

	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	called := false
	svc.saveSparse = func(cfg *config.Config, path string, ctx config.SparseSaveContext) error {
		called = true
		return errors.New("disk full")
	}

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)
	assert.True(t, called)

	var persistErr *persistError
	assert.ErrorAs(t, err, &persistErr)
}

func TestConfigUpdateService_ReloadFailure_RollbackSucceeds_ReturnsReloadError(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Server.Host = "0.0.0.0"

	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	reloadErr := errors.New("scraper registry init boom")
	svc.reload = func(rt *core.APIRuntime, deps *core.APIDeps, cfg *config.Config) error {
		return reloadErr
	}

	saveCalls := 0
	svc.saveSparse = func(cfg *config.Config, path string, ctx config.SparseSaveContext) error {
		saveCalls++
		return nil
	}

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)
	assert.Equal(t, 2, saveCalls)

	var rlErr *reloadError
	assert.ErrorAs(t, err, &rlErr)
	assert.Contains(t, err.Error(), "reverted")
}

func TestConfigUpdateService_ReloadFailure_RollbackAlsoFails_ReturnsRollbackError(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Server.Host = "0.0.0.0"

	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	svc.reload = func(rt *core.APIRuntime, deps *core.APIDeps, cfg *config.Config) error {
		return errors.New("reload failed")
	}
	rollbackErr := errors.New("rollback write failed")
	saveCalls := 0
	svc.saveSparse = func(cfg *config.Config, path string, ctx config.SparseSaveContext) error {
		saveCalls++
		if saveCalls == 1 {
			return nil
		}
		return rollbackErr
	}

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)
	assert.Equal(t, 2, saveCalls)

	var rbErr *rollbackError
	assert.ErrorAs(t, err, &rbErr)
	assert.Contains(t, err.Error(), "rollback save failed")
}

func TestConfigUpdateService_FinalizeFailure_ReturnsValidationError_NoSaveNoReload(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Server.Host = "0.0.0.0"

	tempConfigFile := t.TempDir() + "/config.yaml"
	require.NoError(t, config.Save(oldCfg, tempConfigFile))
	deps := createTestDeps(t, oldCfg, tempConfigFile)

	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)
	svc.finalize = func(*config.ScrapersConfig, models.ScraperConfigResolverInterface) error {
		return errors.New("finalize boom")
	}
	saveCalled := false
	svc.saveSparse = func(*config.Config, string, config.SparseSaveContext) error {
		saveCalled = true
		return nil
	}
	reloadCalled := false
	svc.reload = func(*core.APIRuntime, *core.APIDeps, *config.Config) error {
		reloadCalled = true
		return nil
	}

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)

	var valErr *validationError
	assert.ErrorAs(t, err, &valErr)
	assert.Contains(t, err.Error(), "finalize boom")
	assert.False(t, saveCalled)
	assert.False(t, reloadCalled)

	data, readErr := afero.ReadFile(afero.NewOsFs(), tempConfigFile)
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "0.0.0.0")
}
