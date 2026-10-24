package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScraperResultCloneCastNameEvidence(t *testing.T) {
	t.Parallel()
	for _, evidence := range [][]CastNameEvidence{nil, {}, {{SourceID: "aa6sc", DisplayName: "Yorimoto Shiori", ManualOverride: true}}} {
		original := &ScraperResult{Source: "javlibrary", CastNameEvidence: evidence}
		clone := original.Clone()
		require.Equal(t, original, clone, "nil and non-nil empty evidence retain their shape")
		if len(evidence) > 0 {
			clone.CastNameEvidence[0] = CastNameEvidence{SourceID: "other", DisplayName: "Other Name"}
			require.Equal(t, CastNameEvidence{SourceID: "aa6sc", DisplayName: "Yorimoto Shiori", ManualOverride: true}, original.CastNameEvidence[0], "editing cloned provenance must not change the source result")
		}
		before, err := json.Marshal(original)
		require.NoError(t, err)
		original.CastNameEvidence = nil
		after, err := json.Marshal(original)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after), "transient source evidence never leaks into raw JSON")
	}
}
