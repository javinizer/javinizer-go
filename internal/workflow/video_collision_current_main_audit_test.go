package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/stretchr/testify/require"
)

// Current-main integration probes. Never allow legacy force authority to
// expand typed collision decisions or reuse a receipt across jobs/applies.
func TestIssue277CurrentMainAuthorizationBoundaries(t *testing.T) {
	for _, mode := range []string{"cross-job", "apply-generation", "force-with-fail", "force-with-unsigned-replace", "force-with-skip"} {
		t.Run(mode, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-integration")
			cmd, plan := issue277Command(t, env, true)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("keep destination"), 0600))
			decision := issue277Decision(t, env, cmd, plan)
			switch mode {
			case "cross-job":
				cmd.Organize.VideoCollisionScope += "-other"
			case "apply-generation":
				cmd.Organize.VideoCollisionGeneration++
			case "force-with-fail":
				cmd.Organize.ForceUpdate = true
				decision.Policy = videocollision.Fail
			case "force-with-unsigned-replace":
				cmd.Organize.ForceUpdate = true
				decision.ReviewToken = ""
			case "force-with-skip":
				cmd.Organize.ForceUpdate = true
				decision.Policy = videocollision.Skip
			}
			cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: decision}
			result, err := env.orch.Execute(t.Context(), cmd)
			if mode == "force-with-skip" {
				require.NoError(t, err)
				require.Equal(t, "skipped", result.VideoOutcome)
			} else {
				require.Error(t, err)
			}
			source, err := os.ReadFile(env.source)
			require.NoError(t, err)
			require.Equal(t, "video", string(source))
			destination, err := os.ReadFile(plan.TargetPath)
			require.NoError(t, err)
			require.Equal(t, "keep destination", string(destination))
		})
	}
}
