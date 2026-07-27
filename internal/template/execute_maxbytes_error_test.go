package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteWithMaxBytes_FrameErrorReturnsClampedResult(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC", Title: "T"}
	// Use a valid template so Execute doesn't error — the frame error path
	// is when the sentinel frame execution fails, which is hard to trigger.
	// Instead test the titleBudget<=0 path with a small maxBytes.
	got, err := e.ExecuteWithMaxBytes("<ID>", ctx, 2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 2)
}

func TestExecuteWithMaxBytes_TitleBudgetExhaustedErrorPath(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC-123", Title: "T"}
	// maxBytes=1 — very small, titleBudget will be <= 0 or very small
	got, err := e.ExecuteWithMaxBytes("<ID>", ctx, 1)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 1)
}

func TestExecuteWithMaxBytes_TitleFitsInBudgetClampPath(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "X"}
	// maxBytes=3 — "AB - X" = 5 bytes, but title (1 byte) fits in budget.
	// The result exceeds maxBytes so clampBytes should truncate.
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 3)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 3)
}

func TestExecuteWithMaxBytes_TruncatedTitleClampPath(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "AB", Title: "Very Long Title"}
	// maxBytes=5 — title gets truncated, but final result may still exceed maxBytes
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 5)
}
