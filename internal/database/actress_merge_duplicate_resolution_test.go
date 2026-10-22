package database

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditDuplicateCanonicalResolution(t *testing.T) {
	for i := 0; i < 40; i++ {
		_, err := normalizeMergeResolutions(map[string]string{"FIRST_NAME": "source", "first_name": "target"})
		require.Error(t, err, "contradictory normalized decisions must be rejected")
	}
}

func TestNormalizeEquivalentMergeDecisions(t *testing.T) {
	decisions, err := NormalizeActressMergeResolutions(map[string]string{" FIRST_NAME ": " SOURCE ", "first_name": "source"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"first_name": "source"}, decisions)
}
