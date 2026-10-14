package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestResidentPrimaryFailedSidecarRollbackRemainsRevertable(t *testing.T) {
	for _, kind := range []string{"subtitle", "multipart"} {
		phases := []string{"fence", "stale"}
		if kind == "subtitle" {
			phases = append(phases, "inner")
		}
		for _, phase := range phases {
			for _, blocked := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/blocked=%v", kind, phase, blocked), func(t *testing.T) {
					fs := afero.NewOsFs()
					root := t.TempDir()
					incoming := filepath.Join(root, "incoming")
					library := filepath.Join(root, "library")
					dir := filepath.Join(library, "ABC-123")
					require.NoError(t, fs.MkdirAll(incoming, 0755))
					require.NoError(t, fs.MkdirAll(dir, 0755))
					source, target := filepath.Join(incoming, "incoming.mp4"), filepath.Join(dir, "ABC-123.mp4")
					require.NoError(t, afero.WriteFile(fs, source, []byte("resident video"), 0644))
					require.NoError(t, os.Link(source, target))
					original, err := fs.Stat(target)
					require.NoError(t, err)
					sideSource, sideTarget := filepath.Join(incoming, "incoming.en.srt"), filepath.Join(dir, "ABC-123.eng.srt")
					if kind == "multipart" {
						sideSource, sideTarget = filepath.Join(incoming, "incoming-cd2.mp4"), filepath.Join(dir, "ABC-123-cd2.mp4")
					}
					require.NoError(t, afero.WriteFile(fs, sideSource, []byte("admitted sidecar"), 0644))
					db, _ := pr260ArtifactDB(t)
					repo := database.NewBatchFileOperationRepository(db)
					movie := models.Movie{ID: "ABC-123", ContentID: "resident-rollback"}
					engine := template.NewEngine()
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, engine, nil)
					orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "resident-rollback", fs, nil, nil, nil), nil, nil)
					fence := postPublishFence{movie: &movie, err: os.ErrPermission, after: func() {
						_, err := fs.Stat(sideSource)
						require.True(t, os.IsNotExist(err), "sidecar was consumed before fence failure")
						require.FileExists(t, sideTarget)
						if blocked {
							require.NoError(t, afero.WriteFile(fs, sideSource, []byte("foreign claimant"), 0644))
						}
					}}
					failure := error(os.ErrPermission)
					if phase == "stale" {
						failure = database.ErrApplyPublicationStale
						fence.err = failure
					}
					if phase == "inner" {
						orch.revertLog = residentRollbackReconcileFailure{RevertLog: orch.revertLog, before: fence.after}
						fence.after = nil
						fence.err = nil
					}
					cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: fence}
					result, err := orch.Execute(context.Background(), cmd)
					require.ErrorIs(t, err, failure)
					require.NotNil(t, result)
					rows, err := repo.FindByBatchJobID(t.Context(), "resident-rollback")
					require.NoError(t, err)
					require.Len(t, rows, 1)
					require.Empty(t, rows[0].NewPath, "resident primary never becomes an Undo anchor")
					if blocked {
						require.False(t, result.PrePublication, "unresolved sidecar mutation must stay recoverable")
						require.Equal(t, models.RevertStatusFailed, rows[0].RevertStatus)
						ledger, err := models.ParseGeneratedFiles(rows[0].GeneratedFiles)
						require.NoError(t, err)
						require.True(t, ledger.PrimaryResident)
						require.NotEmpty(t, ledger.MoveBack)
						body, err := afero.ReadFile(fs, sideSource)
						require.NoError(t, err)
						require.Equal(t, "foreign claimant", string(body))
						require.NoError(t, fs.Remove(sideSource))
					} else {
						require.True(t, result.PrePublication, "successful compensation leaves no unresolved source mutation")
						require.Equal(t, models.RevertStatusNoOp, rows[0].RevertStatus)
					}
					require.NoError(t, fs.Remove(source))
					undone, err := history.NewReverter(fs, repo).RevertBatch(t.Context(), "resident-rollback")
					require.NoError(t, err)
					require.Zero(t, undone.Failed)
					if blocked {
						require.Equal(t, 1, undone.Succeeded)
					}
					body, err := afero.ReadFile(fs, sideSource)
					require.NoError(t, err)
					require.Equal(t, "admitted sidecar", string(body))
					info, err := fs.Stat(target)
					require.NoError(t, err)
					require.True(t, os.SameFile(original, info))
					_, err = fs.Stat(source)
					require.True(t, os.IsNotExist(err), "Undo must never relocate resident primary")
				})
			}
		}
	}
}

type residentRollbackReconcileFailure struct {
	RevertLog
	before func()
}

func (l residentRollbackReconcileFailure) ReconcileMoveIntents(context.Context, OperationID, []models.FileMove) error {
	l.before()
	return os.ErrPermission
}

func TestResidentPrimaryUnarmedSidecarRemainsUnresolved(t *testing.T) {
	env := setupCopyIntentE2E(t, "resident-unarmed")
	info, err := env.fs.Stat(env.subtitle)
	require.NoError(t, err)
	stage := &artifactStage{fs: env.fs, residentSidecarMutation: true, siblings: []artifactSibling{{sourcePath: env.subtitle, subtitle: true, identity: captureArtifactSourceIdentity(env.fs, env.subtitle, info)}}}
	require.NoError(t, env.fs.Remove(env.subtitle))
	stage.clearRestoredResidentSidecars()
	require.True(t, stage.residentSidecarMutation, "successful batch rollback cannot settle a move whose inverse never armed")
}
