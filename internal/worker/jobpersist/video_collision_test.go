package jobpersist

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestIssue277CollisionAndSkipReceiptsRoundTrip(t *testing.T) {
	plan := applyplan.Default(applyplan.VideoOperationOrganize, "/library")
	plan.VideoCollisionPolicy = videocollision.Skip
	conflict := &videocollision.ConflictError{Code: "VIDEO_COLLISION_STALE", Collision: videocollision.Collision{Source: "/incoming/a.mp4", Destination: "/library/a.mp4", Kind: "file", SourceSize: 7, DestinationSize: 9, ReviewToken: "server-receipt"}}
	snapshot := Snapshot{ID: "issue277", ApplyPlan: plan, ApplyGeneration: 3, Results: map[string]*resultstore.MovieResult{"a": {Status: models.JobStatusFailed, VideoCollision: conflict}, "b": {Status: models.JobStatusCompleted, VideoOutcome: "skipped"}}}
	row, err := Encode(snapshot)
	require.NoError(t, err)
	loaded, errs := Decode(row)
	require.Empty(t, errs)
	require.Equal(t, videocollision.Skip, loaded.ApplyPlan.VideoCollisionPolicy)
	require.Equal(t, uint64(3), loaded.ApplyGeneration)
	require.Equal(t, conflict, loaded.Results["a"].VideoCollision)
	require.Equal(t, "skipped", loaded.Results["b"].VideoOutcome)
	clone := loaded.Results["a"].Clone()
	clone.VideoCollision.Collision.Destination = "changed"
	require.Equal(t, "/library/a.mp4", loaded.Results["a"].VideoCollision.Collision.Destination)
}
