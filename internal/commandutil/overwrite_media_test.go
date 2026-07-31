package commandutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCLIApplyOptions_MapsOverwriteExistingMedia(t *testing.T) {
	assert.True(t, CLIApplyOptions{OverwriteExistingMedia: true}.ToApplyPhaseConfig().OverwriteExistingMedia)
	assert.False(t, (CLIApplyOptions{}).ToApplyPhaseConfig().OverwriteExistingMedia)
}
