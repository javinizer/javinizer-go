package r18devdump

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaddedRemasterFullDumpLookup(t *testing.T) {
	path := seedDump(t, "1rct00156h\tRCT-156-HD\ndv00818ai\tDV-818-AI\nabc00001h\tABC-001-HD")
	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()
	for _, tc := range []struct{ query, cid string }{
		{"RCT-00156-HD", "1rct00156h"},
		{"RCT.156.HD", "1rct00156h"},
		{"RCT.00156.HD", "1rct00156h"},
		{"RCT_00156_HD", "1rct00156h"},
		{"DV.00818.AI", "dv00818ai"},
		{"RCT-00156H", "1rct00156h"},
		{"DV-00818AI", "dv00818ai"},
		{"ABC-00001H", "abc00001h"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			movie, err := store.LookupMovie(context.Background(), tc.query)
			require.NoError(t, err)
			require.NotNil(t, movie)
			assert.Equal(t, tc.cid, movie.ContentID)
		})
	}
	assert.Contains(t, dumpNormKeys("ABC-00000-HD"), "ABC000HD")
	assert.Equal(t, []string{"RCT00156"}, dumpNormKeys("RCT-00156"))
	assert.Equal(t, []string{"RCT.00156"}, dumpNormKeys("RCT.00156"))
}
