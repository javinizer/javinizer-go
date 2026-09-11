package r18devdump

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWideNumericDumpLookup(t *testing.T) {
	path := seedDump(t, "abc1h\tABC-1-HD\nabc123456h\tABC-123456-HD")
	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()
	for _, tc := range []struct{ query, cid string }{
		{"ABC-1-HD", "abc1h"},
		{"ABC-1H", "abc1h"},
		{"ABC-123456-HD", "abc123456h"},
		{"ABC-123456H", "abc123456h"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			movie, err := store.LookupMovie(context.Background(), tc.query)
			require.NoError(t, err)
			require.NotNil(t, movie)
			assert.Equal(t, tc.cid, movie.ContentID)
		})
	}
	assert.Contains(t, dumpNormKeys("ABC-1H"), "ABC1HD")
	assert.Contains(t, dumpNormKeys("ABC-123456H"), "ABC123456HD")
}
