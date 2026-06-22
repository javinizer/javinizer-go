package token

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateUUID_Uncovered(t *testing.T) {
	t.Run("generates valid UUID format", func(t *testing.T) {
		id, err := generateUUID()
		assert.NoError(t, err)
		assert.Len(t, id, 36, "UUID should be 36 chars with dashes")
		// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
		assert.Equal(t, byte('-'), id[8])
		assert.Equal(t, byte('-'), id[13])
		assert.Equal(t, byte('-'), id[18])
		assert.Equal(t, byte('-'), id[23])
		assert.Equal(t, byte('4'), id[14], "version nibble should be 4")
		// Variant bits: y should be 8, 9, a, or b
		y := id[19]
		assert.Contains(t, []byte{'8', '9', 'a', 'b'}, y)
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id, err := generateUUID()
			assert.NoError(t, err)
			assert.False(t, ids[id], "UUID should be unique")
			ids[id] = true
		}
	})
}
