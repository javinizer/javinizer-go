package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeamStringsFromBody verifies that the seam-field extraction (used by
// prepareBatchRequest) distinguishes missing fields (fall back to defaults)
// from present-but-wrong-typed fields (reject with 400). See CodeRabbit
// review 4583245642: stringField previously collapsed both cases to "".
func TestSeamStringsFromBody(t *testing.T) {
	t.Run("nil body yields empty input", func(t *testing.T) {
		input, err := seamStringsFromBody(nil)
		require.NoError(t, err)
		assert.Equal(t, workflow.SeamStringsInput{}, input)
	})

	t.Run("missing fields yield empty input (fall back to defaults)", func(t *testing.T) {
		input, err := seamStringsFromBody(map[string]any{})
		require.NoError(t, err)
		assert.Equal(t, workflow.SeamStringsInput{}, input)
	})

	t.Run("string fields are extracted", func(t *testing.T) {
		body := map[string]any{
			"operation_mode":  "organize",
			"link_mode":       "hard",
			"preset":          "conservative",
			"scalar_strategy": "prefer-nfo",
			"array_strategy":  "merge",
		}
		input, err := seamStringsFromBody(body)
		require.NoError(t, err)
		assert.Equal(t, "organize", input.OperationMode)
		assert.Equal(t, "hard", input.LinkMode)
		assert.Equal(t, "conservative", input.Preset)
		assert.Equal(t, "prefer-nfo", input.ScalarStrategy)
		assert.Equal(t, "merge", input.ArrayStrategy)
	})

	t.Run("present non-string operation_mode is rejected", func(t *testing.T) {
		_, err := seamStringsFromBody(map[string]any{"operation_mode": 42})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})

	t.Run("present non-string preset is rejected", func(t *testing.T) {
		_, err := seamStringsFromBody(map[string]any{"preset": true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})
}
