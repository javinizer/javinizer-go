package actresscache_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/require"
)

func TestLookupDMMIDMatchesExactClaimsAndOwnsResult(t *testing.T) {
	entries := evidence()
	unique := entries[0]
	unique.SourceID = "actor-at-limit"
	unique.ExternalIDs = []actresscache.ExternalID{{Namespace: "dmm", Value: strconv.Itoa(math.MaxInt)}}
	encoded := entries[0]
	encoded.SourceID = "actor-with-other-spelling"
	encoded.ExternalIDs = []actresscache.ExternalID{
		{Namespace: "dmm", Value: "08"},
		{Namespace: "dmm", Value: "0"},
		{Namespace: "dmm", Value: "-7"},
	}
	snapshot := build(t, append(entries, unique, encoded))
	for _, id := range []int{7, 8, math.MaxInt} {
		want, err := snapshot.LookupUnique("dmm", strconv.Itoa(id))
		require.NoError(t, err)
		require.Equal(t, want, snapshot.LookupDMMID(id))
	}
	require.True(t, snapshot.LookupDMMID(7).Ambiguous)
	require.Nil(t, snapshot.LookupDMMID(8).Candidate, "leading-zero claim isn't the decimal ID")
	for _, id := range []int{0, -7} {
		raw, err := snapshot.LookupUnique("dmm", strconv.Itoa(id))
		require.NoError(t, err)
		require.NotNil(t, raw.Candidate, "the generic raw-claim contract is unchanged")
		require.Equal(t, actresscache.UniqueLookupResult{}, snapshot.LookupDMMID(id))
	}

	got := snapshot.LookupDMMID(math.MaxInt)
	require.NotNil(t, got.Candidate)
	got.Candidate.Metadata.Aliases[0] = "changed"
	got.Candidate.ExternalIDs[0].Value = "changed"
	again, err := snapshot.LookupUnique("dmm", strconv.Itoa(math.MaxInt))
	require.NoError(t, err)
	require.Equal(t, again, snapshot.LookupDMMID(math.MaxInt))
	require.NotEqual(t, got.Candidate, again.Candidate)
}
