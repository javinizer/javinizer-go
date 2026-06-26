package commandutil

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

func TestCoreDeps_GetConfig_AtomicPointer_Uncovered(t *testing.T) {
	deps := &CoreDeps{}
	newCfg := &config.Config{}
	deps.config.Store(newCfg)

	got := deps.GetConfig()
	assert.Equal(t, newCfg, got)
}

func TestCoreDeps_SetConfig_Nil_Uncovered(t *testing.T) {
	deps := &CoreDeps{}
	deps.config.Store(&config.Config{})
	// SetConfig(nil) should panic — nil config is a programming error
	assert.Panics(t, func() {
		deps.SetConfig(nil)
	}, "SetConfig should panic when called with nil config")
}

func TestCoreDeps_ReplaceReloadable_NilConfig_Uncovered(t *testing.T) {
	deps := &CoreDeps{}
	deps.config.Store(&config.Config{})
	// ReplaceReloadable(nil, ...) should panic — nil config is a programming error
	assert.Panics(t, func() {
		deps.ReplaceReloadable(nil, nil)
	}, "ReplaceReloadable should panic when called with nil config")
}

func TestCoreDeps_GetConfig_BothNil_Uncovered(t *testing.T) {
	deps := &CoreDeps{}
	// GetConfig should panic when no config has been set
	assert.Panics(t, func() {
		deps.GetConfig()
	}, "GetConfig should panic when no config has been set")
}

func TestCoreDeps_Close_NilDB_Uncovered(t *testing.T) {
	deps := &CoreDeps{}
	err := deps.Close()
	assert.NoError(t, err)
}

func TestCoreDeps_GetRegistry_Nil_Panics_Uncovered(t *testing.T) {
	deps := &CoreDeps{}
	deps.config.Store(&config.Config{})
	// GetRegistry should panic when ScraperRegistry is nil
	assert.Panics(t, func() {
		deps.GetRegistry()
	}, "GetRegistry should panic when ScraperRegistry is nil")
}

func TestCoreDeps_ReplaceReloadable_SetsBoth_Uncovered(t *testing.T) {
	deps := &CoreDeps{}
	deps.config.Store(&config.Config{})

	newCfg := &config.Config{}
	newReg := scraperutil.NewScraperRegistry()
	deps.ScraperRegistry = nil
	deps.ReplaceReloadable(newCfg, newReg)

	// GetConfig should now return newCfg from atomic pointer
	got := deps.GetConfig()
	assert.Equal(t, newCfg, got)
	assert.Equal(t, newReg, deps.ScraperRegistry)
}
