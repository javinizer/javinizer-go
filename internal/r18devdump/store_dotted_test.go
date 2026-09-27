package r18devdump

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestT28MarkerCandidateIdentity(t *testing.T) {
	assert.Contains(t, ContentIDCandidatesWithMarker("T28-123-HD"), "9t28123h")
	assert.Contains(t, ContentIDCandidatesWithMarker("T28-12-HD"), "9t28012h")
	assert.Contains(t, ContentIDCandidatesWithMarker("t2800123hd"), "t2800123hd")
	assert.NotContains(t, ContentIDCandidatesWithMarker("t2800123hd"), "t2800123h")
}

func TestStoreDottedMarkerCandidateFallback(t *testing.T) {
	path := seedDump(t, "1rct00156h\t\\N\ndv00818ai\t\\N\n9t28123h\t\\N")
	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()
	for _, tc := range []struct{ query, cid string }{
		{"RCT.00156.HD", "1rct00156h"},
		{"RCT-156.HD", "1rct00156h"},
		{"DV.818.AI", "dv00818ai"},
		{"T28-123-HD", "9t28123h"},
		{"T28.00123.HD", "9t28123h"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			matches, err := store.MatchByDisplayID(context.Background(), tc.query)
			require.NoError(t, err)
			require.Len(t, matches, 1)
			assert.Equal(t, tc.cid, matches[0].ContentID)
		})
	}
}
