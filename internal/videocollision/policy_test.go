package videocollision

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNormalize(t *testing.T) {
	for _, policy := range []Policy{"", Fail, Skip, Replace} {
		got, err := Normalize(policy)
		require.NoError(t, err)
		require.NotEmpty(t, got)
	}
	for _, policy := range []Policy{"REPLACE", "true", "keep-both", " fail "} {
		_, err := Normalize(policy)
		require.Error(t, err)
	}
}
