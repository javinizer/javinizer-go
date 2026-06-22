package history

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- revertFile: update operation type with cleanup ---

func TestMiss5_IsDescendant(t *testing.T) {
	assert.True(t, isDescendant("/out/sub/file", "/out"))
	assert.True(t, isDescendant("/out", "/out"))
	assert.False(t, isDescendant("/other/file", "/out"))
	assert.False(t, isDescendant("/outside", "/out"))
}

// --- RevertBatch: failed DB persist after revert ---
