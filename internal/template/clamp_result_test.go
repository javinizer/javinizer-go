package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClampResult_ProgressiveTitleTruncation(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC", Title: "Very Long Title That Exceeds Limit"}
	// maxBytes=10: "ABC - Very Long Title That Exceeds Limit" exceeds 10,
	// so title gets progressively truncated until result fits.
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 10)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 10)
	assert.Contains(t, got, "ABC", "ID should be preserved")
}

func TestClampResult_LastResortHardTruncation(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABCDEFGHIJKLMNOP", Title: ""}
	// ID alone is 16 chars, maxBytes=5. Even empty title exceeds.
	// Now returns error instead of hard-truncating.
	_, err := e.ExecuteWithMaxBytes("<ID>", ctx, 5)
	require.Error(t, err, "should error when fixed content exceeds maxBytes")
}

func TestClampResult_TitleAlreadyFits(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "CD"}
	// Result "AB - CD" = 7 bytes, maxBytes=100 — no truncation needed.
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 100)
	require.NoError(t, err)
	assert.Equal(t, "AB - CD", got)
}

func TestClampResult_ExactFit(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "CD"}
	// Result "AB - CD" = 7 bytes, maxBytes=7 — exact fit.
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "AB - CD", got)
}
