package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/stretchr/testify/require"
)

func TestIssue277ReplacementRollback(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(map[bool]string{true: "move", false: "copy"}[move], func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-rollback")
			cmd, plan := issue277Command(t, env, move)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("displaced video"), 0644))
			before, err := os.Stat(plan.TargetPath)
			require.NoError(t, err)
			cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
			cmd.PublicationFence = postPublishFence{movie: &env.movie, err: os.ErrPermission}
			_, err = env.orch.Execute(t.Context(), cmd)
			require.ErrorIs(t, err, os.ErrPermission)
			body, err := os.ReadFile(plan.TargetPath)
			require.NoError(t, err)
			require.Equal(t, "displaced video", string(body))
			after, err := os.Stat(plan.TargetPath)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			body, err = os.ReadFile(env.source)
			require.NoError(t, err)
			require.Equal(t, "video", string(body))
		})
	}
}

func TestIssue277LateArrivalAndAfterArmClaimant(t *testing.T) {
	for _, reviewed := range []bool{false, true} {
		t.Run(map[bool]string{true: "after-arm", false: "late-arrival"}[reviewed], func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-late")
			cmd, plan := issue277Command(t, env, true)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
			plant := func() { require.NoError(t, os.WriteFile(plan.TargetPath, []byte("late claimant"), 0644)) }
			if reviewed {
				require.NoError(t, os.WriteFile(plan.TargetPath, []byte("reviewed video"), 0644))
				cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
				env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, preExecute: func(*organizer.OrganizePlan) { plant() }}
			} else {
				env.orch.artifactPrepared = plant
			}
			result, err := env.orch.Execute(t.Context(), cmd)
			require.Error(t, err)
			var conflict *videocollision.ConflictError
			require.True(t, errors.As(err, &conflict), "%v", err)
			require.Equal(t, map[bool]string{true: "VIDEO_COLLISION_STALE", false: "VIDEO_COLLISION_REFRESHED"}[reviewed], conflict.Code)
			body, err := os.ReadFile(plan.TargetPath)
			require.NoError(t, err)
			require.Equal(t, "late claimant", string(body))
			require.FileExists(t, env.source)
			if reviewed {
				require.NotEmpty(t, p3Ledger(t, env.repo, result.OperationID).Replacements)
			}
		})
	}
}

func TestIssue277SelectedSiblingNeverInheritsApply(t *testing.T) {
	env := setupCopyIntentE2E(t, "issue277-selected")
	cmd, plan := issue277Command(t, env, true)
	target := filepath.Join(filepath.Dir(plan.TargetPath), stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(env.multipart)))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("unowned part"), 0644))
	cmd.Organize.VideoCollisionSelectedSources = map[string]bool{env.source: true, env.multipart: true}
	cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.multipart: {Source: env.multipart, Destination: target, Policy: videocollision.Skip}}
	_, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.FileExists(t, env.multipart)
	body, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "unowned part", string(body))
}

func TestIssue277NamingAndGenerationInvalidateReview(t *testing.T) {
	for _, change := range []string{"naming", "generation", "movie-identity", "destination", "skip-drift", "no-op-recorder"} {
		t.Run(change, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-context")
			cmd, plan := issue277Command(t, env, false)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("foreign"), 0644))
			decision := issue277Decision(t, env, cmd, plan)
			switch change {
			case "naming":
				env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, MaxTitleLength: 99}, nil, nil)
			case "generation":
				env.movie.RenderGeneration++
			case "movie-identity":
				env.movie.ContentID = "different-content-with-same-generation"
			case "destination":
				cmd.DestPath = filepath.Join(env.dest, "different")
			case "skip-drift":
				decision.Policy = videocollision.Skip
				require.NoError(t, os.WriteFile(plan.TargetPath, []byte("changed"), 0644))
			case "no-op-recorder":
				env.orch.revertLog = noOpRevertLog{}
			}
			cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: decision}
			_, err := env.orch.Execute(t.Context(), cmd)
			require.Error(t, err)
			require.FileExists(t, env.source)
		})
	}
}
