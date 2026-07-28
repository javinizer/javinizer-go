package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteWithMaxBytes_FrameErrorReturnsClampedResult(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC", Title: "T"}
	// ID alone is 3 bytes, maxBytes=2. Now returns error instead of hard-truncating.
	_, err := e.ExecuteWithMaxBytes("<ID>", ctx, 2)
	require.Error(t, err, "should error when fixed content exceeds maxBytes")
}

func TestExecuteWithMaxBytes_TitleBudgetExhaustedErrorPath(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC-123", Title: "T"}
	// maxBytes=1 — ID alone (7 bytes) exceeds budget. Now returns error.
	_, err := e.ExecuteWithMaxBytes("<ID>", ctx, 1)
	require.Error(t, err, "should error when fixed content exceeds maxBytes")
}

func TestExecuteWithMaxBytes_TitleFitsInBudgetClampPath(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "X"}
	// maxBytes=3 — "AB - X" = 6 bytes exceeds budget. Now returns error.
	_, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 3)
	require.Error(t, err, "should error when fixed content exceeds maxBytes")
}

func TestExecuteWithMaxBytes_TruncatedTitleClampPath(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "Very Long Title"}
	// maxBytes=5 — title gets truncated, but final result may still exceed maxBytes
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 5)
}
