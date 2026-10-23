package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/stretchr/testify/require"
)

func TestIssue277ReviewedRenameFileCopyMoveUndo(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(map[bool]string{true: "move", false: "copy"}[move], func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-rename")
			mode := operationmode.OperationModeInPlaceNoRenameFolder
			org := organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), nil)
			env.org, env.orch.organizer = org, org
			cmd := pr260FencedCommand(&env.movie, env.match, filepath.Dir(env.source), pr260FencedCounter(t, env.db), mode, false, move, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			cmd.Organize.VideoCollisionPolicy = videocollision.Fail
			cmd.Organize.VideoCollisionScope = env.jobID
			plan, err := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: cmd.DestPath, MoveFiles: move, OperationMode: mode})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("reviewed previous"), 0644))
			cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
			result, err := env.orch.Execute(t.Context(), cmd)
			require.NoError(t, err)
			require.Equal(t, "replaced", result.VideoOutcome)
			body, err := os.ReadFile(plan.TargetPath)
			require.NoError(t, err)
			require.Equal(t, "video", string(body))
			_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			require.NoError(t, err)
			body, err = os.ReadFile(plan.TargetPath)
			require.NoError(t, err)
			require.Equal(t, "reviewed previous", string(body))
			require.FileExists(t, env.source)
		})
	}
}

func TestIssue277BulkSkipIncludesMultipartAndSkipsAllOutput(t *testing.T) {
	env := setupCopyIntentE2E(t, "issue277-bulk-skip")
	cmd, plan := issue277Command(t, env, true)
	cmd.Organize.VideoCollisionPolicy = videocollision.Skip
	cmd.Download, cmd.GenerateNFO = true, true
	target := filepath.Join(filepath.Dir(plan.TargetPath), stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(env.multipart)))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("foreign part"), 0644))
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.Equal(t, "skipped", result.VideoOutcome)
	require.Empty(t, result.OperationID)
	require.FileExists(t, env.source)
	require.FileExists(t, env.multipart)
	require.NoFileExists(t, plan.TargetPath)
	body, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "foreign part", string(body))
}
