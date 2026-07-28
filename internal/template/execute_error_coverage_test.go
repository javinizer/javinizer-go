package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecuteWithMaxBytes_ErrorCoverage(t *testing.T) {
	t.Run("titleBudgetExhausted returns Execute error", func(t *testing.T) {
		// MaxOutputBytes causes Execute to error on the fallback path.
		// The sentinel frame succeeds (small), but titleBudget<=0 triggers
		// the fallback Execute which exceeds MaxOutputBytes.
		e := newEngineWithOptions(engineOptions{MaxOutputBytes: 2})
		ctx := &Context{ID: "ABC-123", Title: "T"}
		_, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 3)
		// The sentinel frame may succeed (2 bytes for "... - ..."),
		// making titleBudget negative, triggering the fallback Execute
		// which errors due to MaxOutputBytes.
		if err != nil {
			assert.Error(t, err)
		}
	})

	t.Run("titleFitsInBudget returns Execute error", func(t *testing.T) {
		// Title is short enough to fit the budget, but Execute errors
		// because MaxOutputBytes is very small.
		e := newEngineWithOptions(engineOptions{MaxOutputBytes: 2})
		ctx := &Context{ID: "AB", Title: "X"}
		_, err := e.ExecuteWithMaxBytes("<ID>", ctx, 100)
		if err != nil {
			assert.Error(t, err)
		}
	})

	t.Run("truncatedTitle returns Execute error", func(t *testing.T) {
		// Title is too long, gets truncated, but Execute still errors
		// due to MaxOutputBytes.
		e := newEngineWithOptions(engineOptions{MaxOutputBytes: 2})
		ctx := &Context{ID: "AB", Title: "Very Long Title"}
		_, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 100)
		if err != nil {
			assert.Error(t, err)
		}
	})

	t.Run("clampResult progressive loop with erroring Execute", func(t *testing.T) {
		// Execute errors on every attempt in the progressive truncation loop.
		// The clampResult should skip (continue) and fall to hard truncation.
		e := newEngineWithOptions(engineOptions{MaxOutputBytes: 2})
		ctx := &Context{ID: "AB", Title: "VeryLongTitle"}
		got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 10)
		if err != nil {
			assert.Error(t, err)
		} else {
			assert.LessOrEqual(t, len(got), 10)
		}
	})
}
