package resultstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/videocollision"
)

func TestMovieResultCloneVideoCollisionIsolation(t *testing.T) {
	t.Parallel()
	expected := videocollision.ConflictError{
		Code: "video_collision_review_stale",
		Collision: videocollision.Collision{
			SelectedSource:  "/incoming/selected.mp4",
			Source:          "/incoming/video.mp4",
			Destination:     "/library/video.mp4",
			SourceSize:      123,
			DestinationSize: 456,
			Kind:            "destination_changed",
			ReviewToken:     "signed-review-token",
		},
	}
	conflict := expected
	original := &MovieResult{VideoCollision: &conflict, VideoOutcome: "conflict"}
	clone := original.Clone()
	require.NotNil(t, clone)
	require.Equal(t, original, clone)
	require.NotSame(t, original.VideoCollision, clone.VideoCollision)

	clone.VideoCollision.Code = "changed-code"
	clone.VideoCollision.Collision = videocollision.Collision{
		SelectedSource:  "/changed/selected.mp4",
		Source:          "/changed/source.mp4",
		Destination:     "/changed/destination.mp4",
		SourceSize:      789,
		DestinationSize: 987,
		Kind:            "changed-kind",
		ReviewToken:     "changed-token",
	}
	require.Equal(t, expected, *original.VideoCollision)
	require.NotEqual(t, *original.VideoCollision, *clone.VideoCollision)
}
