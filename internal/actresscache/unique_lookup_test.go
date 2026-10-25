package actresscache_test

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLookupUniquePreservesAmbiguityAndCopies(t *testing.T) {
	s := build(t, evidence())
	got, err := s.LookupUnique("dmm", "7")
	require.NoError(t, err)
	require.True(t, got.Ambiguous)
	require.Nil(t, got.Candidate)
	got, err = s.LookupUnique("dmm", "07")
	require.NoError(t, err)
	require.False(t, got.Ambiguous)
	require.Nil(t, got.Candidate)
	_, err = s.LookupUnique("DMM", "7")
	require.Error(t, err)
	_, err = s.LookupUnique("dmm", " 7")
	require.Error(t, err)
	got, err = s.LookupUnique("other", "01")
	require.NoError(t, err)
	require.NotNil(t, got.Candidate)
	got.Candidate.Metadata.Aliases[0] = "changed"
	got.Candidate.ExternalIDs[0].Value = "changed"
	again, err := s.LookupUnique("other", "01")
	require.NoError(t, err)
	require.NotEqual(t, got.Candidate, again.Candidate)
}
