package worker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFamilyRevisionSnapshotNilAccessor(t *testing.T) {
	require.Nil(t, familyRevisionSnapshotFromResultMap(nil, "/f/a.mp4"))
}
