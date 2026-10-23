package applyplan

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/videocollision"
)

func TestValidateVideoCollisionPolicyWithoutNormalization(t *testing.T) {
	for _, policy := range []videocollision.Policy{"", videocollision.Fail, videocollision.Skip, videocollision.Replace, "keep-both", "REPLACE", " replace "} {
		t.Run(string(policy), func(t *testing.T) {
			plan := Default(VideoOperationOrganize, "/output")
			plan.VideoCollisionPolicy = policy
			before := *plan
			err := Validate(plan)
			switch policy {
			case "", videocollision.Fail, videocollision.Skip, videocollision.Replace:
				require.NoError(t, err)
			default:
				require.ErrorContains(t, err, "invalid video_collision_policy")
				require.ErrorContains(t, err, string(policy))
			}
			require.Equal(t, before, *plan)
		})
	}
}
