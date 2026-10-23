package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/stretchr/testify/require"
)

func issue277Command(t *testing.T, env *copyIntentE2EEnv, move bool) (ApplyCmd, *organizer.OrganizePlan) {
	t.Helper()
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	cmd.Organize.VideoCollisionPolicy = videocollision.Fail
	cmd.Organize.VideoCollisionScope = env.jobID
	plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, MoveFiles: move, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	return cmd, plan
}

func issue277Decision(t *testing.T, env *copyIntentE2EEnv, cmd ApplyCmd, plan *organizer.OrganizePlan) videocollision.Decision {
	t.Helper()
	collision, err := inspectVideoCollision(t.Context(), env.fs, env.source, plan.TargetPath, videoReviewScope(env.jobID, env.movie.RenderGeneration, cmd.Organize.VideoCollisionGeneration, plan))
	require.NoError(t, err)
	require.NotNil(t, collision)
	return videocollision.Decision{Source: env.source, Destination: plan.TargetPath, Policy: videocollision.Replace, ReviewToken: collision.ReviewToken}
}

func TestIssue277ReviewedVideoLifecycle(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, body := range []string{"foreign video", "video"} {
			for _, policy := range []videocollision.Policy{videocollision.Fail, videocollision.Skip, videocollision.Replace} {
				t.Run(string(policy)+"/"+body+map[bool]string{true: "/move", false: "/copy"}[move], func(t *testing.T) {
					env := setupCopyIntentE2E(t, "issue277")
					cmd, plan := issue277Command(t, env, move)
					require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
					require.NoError(t, os.WriteFile(plan.TargetPath, []byte(body), 0644))
					before, err := os.Stat(plan.TargetPath)
					require.NoError(t, err)
					cmd.Organize.VideoCollisionPolicy = policy
					if policy == videocollision.Replace {
						cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
					}
					result, err := env.orch.Execute(t.Context(), cmd)
					if policy == videocollision.Fail {
						require.Error(t, err)
						require.True(t, result.PrePublication)
					} else {
						require.NoError(t, err)
					}
					if policy != videocollision.Replace {
						after, err := os.Stat(plan.TargetPath)
						require.NoError(t, err)
						require.True(t, os.SameFile(before, after))
						got, err := os.ReadFile(plan.TargetPath)
						require.NoError(t, err)
						require.Equal(t, body, string(got))
						require.FileExists(t, env.source)
						if policy == videocollision.Skip {
							require.Equal(t, "skipped", result.VideoOutcome)
							require.Empty(t, result.OperationID)
							require.Empty(t, result.DownloadPaths)
							require.Empty(t, result.NFOPath)
						}
						return
					}
					got, err := os.ReadFile(plan.TargetPath)
					require.NoError(t, err)
					require.Equal(t, "video", string(got))
					if move {
						require.NoFileExists(t, env.source)
					} else {
						require.FileExists(t, env.source)
					}
					ledger := p3Ledger(t, env.repo, result.OperationID)
					require.NotEmpty(t, ledger.Replacements)
					reverted, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, err)
					require.Equal(t, 1, reverted.Succeeded)
					got, err = os.ReadFile(plan.TargetPath)
					require.NoError(t, err)
					require.Equal(t, body, string(got))
					got, err = os.ReadFile(env.source)
					require.NoError(t, err)
					require.Equal(t, "video", string(got))
				})
			}
		}
	}
}

func TestIssue277ReviewRefusesDriftAndUnsafeTargets(t *testing.T) {
	for _, change := range []string{"forged", "target-swap", "same-inode-rewrite", "source-rewrite", "vanished", "late-swap", "no-recorder", "directory", "symlink", "multipart", "cancel"} {
		t.Run(change, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-drift")
			cmd, plan := issue277Command(t, env, true)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("foreign video"), 0644))
			decision := issue277Decision(t, env, cmd, plan)
			cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: decision}
			ctx := t.Context()
			switch change {
			case "forged":
				decision.ReviewToken += "x"
				cmd.Organize.VideoCollisionDecisions[env.source] = decision
			case "target-swap":
				require.NoError(t, os.Rename(plan.TargetPath, plan.TargetPath+".old"))
				require.NoError(t, os.WriteFile(plan.TargetPath, []byte("foreign video"), 0644))
			case "same-inode-rewrite":
				info, err := os.Stat(plan.TargetPath)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(plan.TargetPath, []byte("changed video"), 0644))
				require.NoError(t, os.Chtimes(plan.TargetPath, info.ModTime(), info.ModTime()))
			case "source-rewrite":
				require.NoError(t, os.WriteFile(env.source, []byte("other"), 0644))
			case "vanished":
				require.NoError(t, os.Remove(plan.TargetPath))
			case "late-swap":
				env.orch.artifactPrepared = func() {
					require.NoError(t, os.Rename(plan.TargetPath, plan.TargetPath+".old"))
					require.NoError(t, os.WriteFile(plan.TargetPath, []byte("late claimant"), 0644))
				}
			case "no-recorder":
				env.orch.revertLog = nil
			case "directory":
				require.NoError(t, os.Remove(plan.TargetPath))
				require.NoError(t, os.Mkdir(plan.TargetPath, 0755))
			case "symlink":
				require.NoError(t, os.Remove(plan.TargetPath))
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), plan.TargetPath); err != nil {
					t.Skipf("symlink creation unavailable: %v", err)
				}
			case "multipart":
				target := filepath.Join(filepath.Dir(plan.TargetPath), stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(env.multipart)))
				require.NoError(t, os.WriteFile(target, []byte("foreign part"), 0644))
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				env.orch.artifactPrepared = cancel
			}
			_, err := env.orch.Execute(ctx, cmd)
			require.Error(t, err)
			require.FileExists(t, env.source)
			if change == "late-swap" {
				got, err := os.ReadFile(plan.TargetPath)
				require.NoError(t, err)
				require.Equal(t, "late claimant", string(got))
			}
		})
	}
}

func TestIssue277HardlinkIsResidentNotCollision(t *testing.T) {
	env := setupCopyIntentE2E(t, "issue277-hardlink")
	cmd, plan := issue277Command(t, env, true)
	require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
	if err := os.Link(env.source, plan.TargetPath); err != nil {
		t.Skipf("hardlink unavailable: %v", err)
	}
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.True(t, result.OrganizeResult.PrimaryResident)
	require.FileExists(t, env.source)
}
