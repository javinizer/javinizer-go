package applyplan

import (
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVideoCollisionPolicyCompatibilityAndSeparation(t *testing.T) {
	var legacy Plan
	require.NoError(t, json.Unmarshal([]byte(`{"version":1,"video_operation":"organize","destination":"/output","nfo_output":"write","media_policy":"missing"}`), &legacy))
	projection, err := Project(&legacy)
	require.NoError(t, err)
	require.Equal(t, videocollision.Fail, projection.VideoCollisionPolicy)
	for _, policy := range []videocollision.Policy{videocollision.Fail, videocollision.Skip, videocollision.Replace} {
		plan := Default(VideoOperationOrganize, "/output")
		plan.VideoCollisionPolicy = policy
		projected, err := Project(plan)
		require.NoError(t, err)
		require.Equal(t, policy, projected.VideoCollisionPolicy)
		require.False(t, projected.OverwriteExistingMedia)
	}
	legacy.VideoCollisionPolicy = "keep-both"
	_, err = Project(&legacy)
	require.Error(t, err)
}
