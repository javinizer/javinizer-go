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

func TestIssue277DisabledUndoCleansCommittedReplacement(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-no-undo")
			env.orch.revertLog.(*dbRevertLog).cfg.AllowRevert = false
			cmd, plan := issue277Command(t, env, move)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("prior reviewed video"), 0644))
			cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
			result, err := env.orch.Execute(t.Context(), cmd)
			require.NoError(t, err)
			require.Equal(t, "replaced", result.VideoOutcome)
			ledger := p3Ledger(t, env.repo, result.OperationID)
			require.NotNil(t, ledger.UndoRetention)
			require.False(t, *ledger.UndoRetention)
			require.NotNil(t, ledger.ApplyTerminal)
			require.Equal(t, "success", ledger.ApplyTerminal.Outcome)
			require.Empty(t, ledger.Replacements, "only committed transaction backups may be discarded")
			backups, err := filepath.Glob(plan.TargetPath + ".dlbak.*")
			require.NoError(t, err)
			require.Empty(t, backups)
			body, err := os.ReadFile(plan.TargetPath)
			require.NoError(t, err)
			require.Equal(t, "video", string(body))
			if !move {
				body, err = os.ReadFile(env.source)
				require.NoError(t, err)
				require.Equal(t, "video", string(body))
			}
		})
	}
}

func TestIssue277ResidentOutcomeRepeatAndUndo(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeOrganize, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, move := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: "/copy", true: "/move"}[move], func(t *testing.T) {
				env := setupCopyIntentE2E(t, "issue277-resident")
				if mode == operationmode.OperationModeInPlaceNoRenameFolder {
					resident := filepath.Join(filepath.Dir(env.source), env.movie.ID+".mp4")
					require.NoError(t, os.Rename(env.source, resident))
					env.source = resident
					env.match.Path, env.match.Name = resident, filepath.Base(resident)
					env.dest = filepath.Dir(resident)
					org := organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), nil)
					env.org, env.orch.organizer = org, org
				}
				cmd, _ := issue277Command(t, env, move)
				cmd.OperationMode = mode
				cmd.Organize.VideoCollisionSelectedSources = map[string]bool{env.multipart: true}
				plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, MoveFiles: move, OperationMode: mode})
				require.NoError(t, err)
				if mode == operationmode.OperationModeOrganize {
					require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
					require.NoError(t, os.Link(env.source, plan.TargetPath))
				}
				info, err := os.Stat(env.source)
				require.NoError(t, err)
				identity := captureArtifactSourceIdentity(env.fs, env.source, info)
				require.True(t, identity.hasDevIno)
				for range 2 {
					result, err := env.orch.Execute(t.Context(), cmd)
					require.NoError(t, err)
					require.Equal(t, "same-file", result.VideoOutcome)
					ledger := p3Ledger(t, env.repo, result.OperationID)
					require.NotContains(t, ledger.Delete, plan.TargetPath)
					for _, entry := range ledger.MoveBack {
						require.NotEqual(t, plan.TargetPath, entry.NewPath)
					}
					for _, entry := range ledger.Replacements {
						require.NotEqual(t, plan.TargetPath, entry.Destination)
					}
				}
				_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
				require.NoError(t, err)
				for _, path := range []string{env.source, plan.TargetPath} {
					after, err := os.Stat(path)
					require.NoError(t, err)
					require.True(t, identity.matches(env.fs, path, after))
					body, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, "video", string(body))
				}
			})
		}
	}
}
