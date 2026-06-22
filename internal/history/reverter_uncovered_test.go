package history

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDescendant_Uncovered_EqualPaths(t *testing.T) {
	assert.True(t, isDescendant("/out/ABC-123", "/out/ABC-123"))
}

func TestIsDescendant_Uncovered_DifferentDrives(t *testing.T) {
	assert.False(t, isDescendant("/entirely/different", "/out/ABC-123"))
}
