package models

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCastNameProvenanceAdditiveCloneContract(t *testing.T) {
	for _, provenance := range [][]CastNameProvenance{nil, {}, {{Source: "javlibrary", SourceID: "aa6sc", DisplayName: "Yorimoto Shiori", State: CastNameUnknownOrder}}} {
		original := &ScraperResult{Actresses: []ActressInfo{{FirstName: "Yorimoto", LastName: "Shiori"}}, CastNameProvenance: provenance}
		clone := original.Clone()
		require.Equal(t, original, clone)
		before, err := json.Marshal(original)
		require.NoError(t, err)
		var roundtrip ScraperResult
		require.NoError(t, json.Unmarshal(before, &roundtrip))
		require.Equal(t, original.Actresses, roundtrip.Actresses)
		if len(provenance) > 0 {
			require.Equal(t, provenance, roundtrip.CastNameProvenance)
			clone.CastNameProvenance[0].DisplayName = "changed"
			require.Equal(t, "Yorimoto Shiori", original.CastNameProvenance[0].DisplayName)
		} else {
			require.NotContains(t, string(before), "cast_name_provenance")
		}
	}
}
