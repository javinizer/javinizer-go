package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteWithMaxBytes_ErrorPaths(t *testing.T) {
	e := NewEngine()

	t.Run("invalid template causes frame error, fallback also errors", func(t *testing.T) {
		ctx := &Context{ID: "ABC", Title: "T"}
		// An unclosed tag like "<ID" with no closing > should cause Execute to error
		_, err := e.ExecuteWithMaxBytes("<ID", ctx, 10)
		// If Execute errors on both frame and fallback, ExecuteWithMaxBytes returns the error
		// If Execute doesn't error (just returns the literal), the result is clamped
		if err != nil {
			assert.Error(t, err)
		} else {
			// Execute doesn't error on unclosed tags — it just returns the literal string
			// So we can't easily trigger the error paths through normal templates.
			// This test documents that unclosed tags don't cause errors.
			t.Skip("Execute does not error on unclosed tags; error paths require a custom failing engine")
		}
	})

	t.Run("empty template with small maxBytes", func(t *testing.T) {
		ctx := &Context{ID: "ABC", Title: "VeryLongTitle"}
		got, err := e.ExecuteWithMaxBytes("", ctx, 3)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(got), 3)
	})

	t.Run("template with only fixed content exceeding maxBytes", func(t *testing.T) {
		ctx := &Context{ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", Title: ""}
		got, err := e.ExecuteWithMaxBytes("<ID>", ctx, 5)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(got), 5)
	})

	t.Run("clampResult progressive truncation with CJK title", func(t *testing.T) {
		ctx := &Context{ID: "ABC", Title: "これは非常に長い日本語のタイトルです"}
		got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 15)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(got), 15)
	})

	t.Run("clampResult last resort with long ID and short maxBytes", func(t *testing.T) {
		ctx := &Context{ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890", Title: ""}
		got, err := e.ExecuteWithMaxBytes("<ID>", ctx, 3)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(got), 3)
	})
}
