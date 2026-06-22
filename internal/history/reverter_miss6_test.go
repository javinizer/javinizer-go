package history

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMiss6_IsDescendant_VariousPaths(t *testing.T) {
	assert.True(t, isDescendant("/root/a/b", "/root"))
	assert.True(t, isDescendant("/root", "/root"))
	assert.False(t, isDescendant("/other", "/root"))
	assert.False(t, isDescendant("/root2/file", "/root"))
}

// --- RevertScrape: no operations found for movie ---
