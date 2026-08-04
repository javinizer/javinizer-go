package actresscache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validatedStub(key string, aliases []string) rankedCandidate {
	return validatedStubRank(key, 1, aliases)
}

func validatedStubRank(key string, rank int, aliases []string) rankedCandidate {
	return validatedStubFull(key, rank, 0, aliases)
}

func validatedStubFull(key string, rank int, dmmID int, aliases []string) rankedCandidate {
	return rankedCandidate{
		rank: rank,
		candidate: ValidatedCandidate{Candidate: Candidate{
			Key:     key,
			Source:  "test",
			DMMID:   dmmID,
			Aliases: aliases,
		}},
	}
}

// Transitive collapse: A{花子}, C{はなこ} start in separate groups; B{花子,
// はなこ} bridges both, exercising the matches[1:] merge loop.
func TestMergeCandidatesTransitiveCollapse(t *testing.T) {
	records := mergeCandidates([]rankedCandidate{
		validatedStubRank("a", 1, []string{"花子"}),
		validatedStubRank("c", 0, []string{"はなこ"}),          // sorts first, own group
		validatedStubFull("b", 1, 7, []string{"花子", "はなこ"}), // DMM ID lets it bridge two groups
	})
	require.Len(t, records, 1, "bridging candidate must collapse both groups")
	assert.ElementsMatch(t, []string{"花子", "はなこ"}, records[0].Aliases)
}
