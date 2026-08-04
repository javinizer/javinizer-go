package actresscache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validatedStub(key string, aliases []string) rankedCandidate {
	return rankedCandidate{
		rank: 1,
		candidate: ValidatedCandidate{Candidate: Candidate{
			Key:     key,
			Source:  "test",
			Aliases: aliases,
		}},
	}
}

// Transitive collapse: A{花子}, C{はなこ} start in separate groups; B{花子,
// はなこ} bridges both, exercising the matches[1:] merge loop.
func TestMergeCandidatesTransitiveCollapse(t *testing.T) {
	records := mergeCandidates([]rankedCandidate{
		validatedStub("a", []string{"花子"}),
		validatedStub("c", []string{"はなこ"}),
		validatedStub("b", []string{"花子", "はなこ"}),
	})
	require.Len(t, records, 1, "bridging candidate must collapse both groups")
	assert.ElementsMatch(t, []string{"花子", "はなこ"}, records[0].Aliases)
}
