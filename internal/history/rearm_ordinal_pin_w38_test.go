package history

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// rearmNextOrdinal: strictly increasing, used by the publish-bound restage
// naming — pinned so the gate sees the lambda's statement.
func TestRearmNextOrdinalW38_Monotonic(t *testing.T) {
	a := rearmNextOrdinal()
	b := rearmNextOrdinal()
	require.Greater(t, b, a)
}
