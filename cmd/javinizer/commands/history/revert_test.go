package history

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRevertCommand(t *testing.T) {
	cmd := NewRevertCommand()
	assert.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "revert")
	assert.True(t, cmd.HasFlags())
}
