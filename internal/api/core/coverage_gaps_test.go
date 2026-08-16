package core

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

func TestCoreGaps_EnsureActressSyncManagerNilReceiver(t *testing.T) {
	var rt *APIRuntime
	assert.Nil(t, rt.EnsureActressSyncManager())
}

func TestCoreGaps_EnsureActressSyncManagerNilDeps(t *testing.T) {
	rt := NewAPIRuntime(nil)
	assert.Nil(t, rt.EnsureActressSyncManager())
}

func TestCoreGaps_EnsureActressSyncManagerStopped(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{CoreDeps: &commandutil.CoreDeps{}})
	rt.actressSyncStopped = true
	assert.Nil(t, rt.EnsureActressSyncManager())
}

func TestCoreGaps_ShutdownDepsNilRT(t *testing.T) {
	shutdownDeps(nil)
}

func TestCoreGaps_ShutdownDepsNilRuntime(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{CoreDeps: &commandutil.CoreDeps{}})
	shutdownDeps(rt)
}

func TestCoreGaps_ActressOnlyPriorityWarningsUnrecognized(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"title": {"unknown-scraper"}}
	warnings := actressOnlyPriorityWarnings(reg, cfg)
	assert.Empty(t, warnings)
}
