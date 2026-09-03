package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Regression: the JobRunner fallback apply config used by batch jobs must not
// authorize destination overwrite by default (multipart data-loss fix, issue #223).
func TestDefaultApplyPhaseConfig_NoOverwriteAuthorization(t *testing.T) {
	cfg := defaultApplyPhaseConfig("/lib")
	assert.False(t, cfg.OrganizeOptions.ForceUpdate, "fallback must not authorize overwrite")
	assert.True(t, cfg.OrganizeOptions.MoveFiles)
	assert.Equal(t, "/lib", cfg.Destination)
}
