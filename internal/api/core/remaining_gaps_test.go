package core

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

func TestRemainingGaps_ActressOnlyPriorityWarningsAllActressOnly(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"title": {"unknown"}}
	warnings := actressOnlyPriorityWarnings(reg, cfg)
	assert.Empty(t, warnings)
}

func TestRemainingGaps_EnsureActressSyncManagerAlreadyRunning(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{CoreDeps: &commandutil.CoreDeps{}})
	assert.Nil(t, rt.EnsureActressSyncManager())
	assert.Nil(t, rt.EnsureActressSyncManager())
}
