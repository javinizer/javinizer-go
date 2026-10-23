package videocollision

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConflictErrorActionableTextPreservesFields(t *testing.T) {
	t.Parallel()
	conflict := &ConflictError{
		Code: "video_collision_review_stale",
		Collision: Collision{
			SelectedSource:  "/incoming/selected.mp4",
			Source:          "/incoming/source video.mp4",
			Destination:     "/library/destination video.mp4",
			SourceSize:      123,
			DestinationSize: 456,
			Kind:            "destination_changed",
			ReviewToken:     "signed-review-token",
		},
	}
	expected := *conflict
	var err error = conflict
	text := err.Error()
	for _, detail := range []string{
		conflict.Code,
		conflict.Collision.Source,
		conflict.Collision.Destination,
		conflict.Collision.Kind,
		"video destination conflict",
		"refresh video collision review",
	} {
		require.Contains(t, text, detail)
	}
	require.Equal(t, expected, *conflict)
}
