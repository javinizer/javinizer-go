package r18dev

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefixlessRawRemasterDumpParity(t *testing.T) {
	for _, id := range []string{"rct00156hd", "rct00156h", "dv00818ai", "t2800123hd", "a00123hd"} {
		t.Run(id, func(t *testing.T) {
			candidates := r18devdump.ContentIDCandidatesWithMarker(id)
			require.NotEmpty(t, candidates)
			assert.Equal(t, id, candidates[0])
			count := 0
			for _, cid := range candidates {
				if cid == id {
					count++
				}
			}
			assert.Equal(t, 1, count)
			s := &scraper{dumpLookup: &stubDumpLookup{matches: []models.DumpMatch{{ContentID: id}}}}
			result, matches := s.searchFromDump(context.Background(), id)
			assert.Nil(t, result)
			require.Len(t, matches, 1)
			assert.Equal(t, id, matches[0].ContentID)
		})
	}
	assert.Equal(t, "1rct00156h", r18devdump.ContentIDCandidatesWithMarker("RCT-00156-HD")[0])
}
