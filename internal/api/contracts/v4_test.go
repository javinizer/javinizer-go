package contracts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTimeV4(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	result := FormatTime(now)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "2024")
}

func TestFormatTimePtrV4(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		result := FormatTimePtr(nil)
		assert.Nil(t, result)
	})

	t.Run("non-nil returns formatted string", func(t *testing.T) {
		now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		result := FormatTimePtr(&now)
		require.NotNil(t, result)
		assert.Contains(t, *result, "2024")
	})
}
