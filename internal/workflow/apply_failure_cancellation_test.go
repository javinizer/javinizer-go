package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// All payloads are small synthetic files under t.TempDir; no downloader or
// external service participates. The real SQLite repository rejects canceled
// contexts, so the assertion covers durable reconciliation rather than a mock.
func TestCanceledResidentApplyReconcilesRecovery(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, scenario := range []string{"compensated", "owned-refusal", "foreign-successor"} {
			t.Run(fmt.Sprintf("move=%v/%s", move, scenario), func(t *testing.T) {
				env := setupCopyIntentE2E(t, "cancel-recovery")
				require.NoError(t, env.fs.Remove(env.multipart))
				targetDir := filepath.Join(env.dest, env.movie.ID)
				require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
				primary := filepath.Join(targetDir, env.movie.ID+".mp4")
				require.NoError(t, os.Link(env.source, primary))
				subtitleBefore, err := afero.ReadFile(env.fs, env.subtitle)
				require.NoError(t, err)
				primaryBefore, err := env.fs.Stat(primary)
				require.NoError(t, err)
				fs := &recoveryDenyRollbackFS{Fs: env.fs, dir: targetDir}
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
				orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, template.NewEngine(), NewDBRevertLog(env.repo, NewRevertLogConfig(true, nil), env.jobID, fs, nil, nil, nil), nil, nil)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				output := ""
				var foreignBody []byte
				fence := postPublishFence{movie: &env.movie, err: context.Canceled, after: func() {
					rows, e := env.repo.FindByBatchJobID(t.Context(), env.jobID)
					require.NoError(t, e)
					require.Len(t, rows, 1)
					ledger, e := models.ParseGeneratedFiles(rows[0].GeneratedFiles)
					require.NoError(t, e)
					if move {
						require.NotEmpty(t, ledger.MoveBack)
						output = ledger.MoveBack[0].NewPath
					} else {
						// Copy installs remain pinned until the fence commits, whereas
						// move installs already promoted their move-back inverse.
						require.NotEmpty(t, ledger.PlannedDeletes)
						for _, entry := range ledger.PlannedDeletes {
							if filepath.Ext(entry.Path) == ".srt" {
								output = entry.Path
							}
						}
					}
					require.Equal(t, ".srt", filepath.Ext(output))
					require.FileExists(t, output)
					if scenario == "foreign-successor" {
						foreignBody, e = afero.ReadFile(env.fs, output)
						require.NoError(t, e)
						require.NoError(t, env.fs.Rename(output, output+".saved"))
						require.NoError(t, afero.WriteFile(env.fs, output, foreignBody, 0644))
					}
					if scenario == "owned-refusal" {
						fs.armed = true
					}
					cancel()
				}}
				cmd := ApplyCmd{Movie: &env.movie, Match: env.match, DestPath: env.dest, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: fence}
				result, err := orch.Execute(ctx, cmd)
				require.ErrorIs(t, err, context.Canceled, "cleanup must not replace the caller's cancellation")
				require.Equal(t, "artifact_publication", result.FailedStep)
				if scenario == "owned-refusal" {
					require.ErrorIs(t, err, os.ErrPermission, "rollback errors must survive the cancellation result")
				}
				id, e := strconv.ParseUint(result.OperationID, 10, 64)
				require.NoError(t, e)
				_, e = env.repo.FindByID(ctx, uint(id))
				require.ErrorIs(t, e, context.Canceled, "real repository must reject the original request context")
				ledger := p3Ledger(t, env.repo, result.OperationID)
				require.NotContains(t, ledger.Delete, output, "rollback must retract promoted unqualified deletes even after cancellation")
				for _, entry := range ledger.PlannedDeletes {
					if entry.Path == output {
						require.True(t, entry.RecoveryPublication, "retained output must carry recovery ownership proof")
					}
				}
				for _, entry := range ledger.MoveBack {
					if entry.NewPath == output {
						require.NotNil(t, entry.Proof, "move-back must carry recovery ownership proof")
						require.True(t, entry.Proof.RecoveryPublication)
					}
				}
				rows, e := env.repo.FindByBatchJobID(t.Context(), env.jobID)
				require.NoError(t, e)
				require.Len(t, rows, 1)
				if scenario == "compensated" {
					require.Equal(t, models.RevertStatusNoOp, rows[0].RevertStatus)
					require.Empty(t, ledger.MoveBack)
					require.Empty(t, ledger.PlannedDeletes)
					// A subsequent unrelated occupant must not be consumed by stale claims.
					require.NoError(t, afero.WriteFile(env.fs, output, []byte("later unrelated file"), 0644))
					foreignBody = []byte("later unrelated file")
				} else if scenario == "foreign-successor" && !move {
					require.Equal(t, models.RevertStatusNoOp, rows[0].RevertStatus)
				} else {
					require.Equal(t, models.RevertStatusFailed, rows[0].RevertStatus)
				}
				if scenario == "owned-refusal" {
					first, e := history.NewReverter(fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, e)
					require.Equal(t, 1, first.Failed)
					require.FileExists(t, output)
				}
				fs.armed = false
				retried, e := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
				require.NoError(t, e)
				if scenario == "owned-refusal" {
					require.Zero(t, retried.Failed)
					require.Equal(t, 1, retried.Succeeded)
					body, e := afero.ReadFile(env.fs, env.subtitle)
					require.NoError(t, e)
					require.Equal(t, subtitleBefore, body)
					_, e = env.fs.Stat(output)
					require.True(t, os.IsNotExist(e), "retry must compensate the owned output")
				} else {
					body, e := afero.ReadFile(env.fs, output)
					require.NoError(t, e)
					require.Equal(t, foreignBody, body, "foreign output must survive Undo")
				}
				now, e := env.fs.Stat(primary)
				require.NoError(t, e)
				require.True(t, os.SameFile(primaryBefore, now))
				sourceNow, e := env.fs.Stat(env.source)
				require.NoError(t, e)
				require.True(t, os.SameFile(primaryBefore, sourceNow), "resident source identity is unchanged")
			})
		}
	}
}

type failureContextKey struct{}
type failureContextLog struct {
	RevertLog
	seen     context.Context
	fail     error
	deadline time.Time
	seenErr  error
}

func (l *failureContextLog) CompleteFailed(ctx context.Context, _ OperationID, _ *ApplyResult) error {
	l.seen = ctx
	l.seenErr = ctx.Err()
	l.deadline, _ = ctx.Deadline()
	return l.fail
}
func TestFailedApplyCleanupContextBounded(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			parent := context.WithValue(t.Context(), failureContextKey{}, "request-value")
			var ctx context.Context
			var cancel context.CancelFunc
			if expired {
				ctx, cancel = context.WithDeadline(parent, time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(parent)
			}
			cancel()
			sentinel := errors.New("ledger failure")
			log := &failureContextLog{fail: sentinel}
			orch := &applyOrchImpl{revertLog: log}
			before := time.Now()
			err := orch.completeRevertLogWithStateErr(ctx, "1", &applyPipelineState{}, true)
			after := time.Now()
			require.ErrorIs(t, err, sentinel)
			require.Equal(t, "request-value", log.seen.Value(failureContextKey{}))
			require.NoError(t, log.seenErr, "cleanup receives a live context despite canceled request")
			require.False(t, log.deadline.Before(before.Add(29*time.Second)))
			require.True(t, log.deadline.Before(after.Add(31*time.Second)))
			require.ErrorIs(t, log.seen.Err(), context.Canceled, "cleanup timer is released when finalization returns")
			require.Error(t, ctx.Err(), "request remains canceled")
		})
	}
}

// Compile-time check keeps the test on the production, context-aware repository.
var _ database.BatchFileOperationRepositoryInterface = (*database.BatchFileOperationRepository)(nil)

type canceledPreparationLog struct {
	*failureContextLog
	cancel context.CancelFunc
}

func (l canceledPreparationLog) Begin(context.Context, ApplyCmd) (OperationID, error) {
	l.cancel()
	return "1", nil
}
func (l canceledPreparationLog) CaptureSnapshot(context.Context, OperationID, ApplyCmd) {}
func TestCanceledPreparationFinalizesWithCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	log := &failureContextLog{}
	movie := &models.Movie{ID: "TEST", ContentID: "cancel-preparation"}
	orch := &applyOrchImpl{fs: afero.NewMemMapFs(), revertLog: canceledPreparationLog{log, cancel}}
	result, err := orch.Execute(ctx, ApplyCmd{Movie: movie, PublicationFence: postPublishFence{movie: movie}})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "artifact_staging", result.FailedStep)
	require.True(t, result.PrePublication)
	require.NotNil(t, log.seen)
	require.NoError(t, log.seenErr)
	require.False(t, log.deadline.IsZero())
	require.ErrorIs(t, log.seen.Err(), context.Canceled)
}

// Exercise the controller's reconciliation of a previously promoted Delete
// separately from copy-mode staging (whose fence still holds a pending pin).
func TestCanceledFailureRetractsPromotedDelete(t *testing.T) {
	env := setupCopyIntentE2E(t, "cancel-promoted-delete")
	output := filepath.Join(filepath.Dir(env.source), "generated.jpg")
	record := &models.BatchFileOperation{BatchJobID: env.jobID, MovieID: env.movie.ID, OriginalPath: env.source, OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryResident: true, Delete: []string{output}})}
	require.NoError(t, env.repo.Create(t.Context(), record))
	// Compensation has removed its generated output. A new occupant at that
	// pathname is unrelated, and the managed-path report must retract the claim.
	require.NoError(t, afero.WriteFile(env.fs, output, []byte("later unrelated file"), 0644))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	state := &applyPipelineState{movie: &env.movie, organizeResult: &organizer.OrganizeResult{PrimaryResident: true, OriginalPath: env.source}, publicationRecovery: &models.PublicationRecovery{Managed: []string{output}}}
	require.NoError(t, env.orch.completeRevertLogWithStateErr(ctx, strconv.FormatUint(uint64(record.ID), 10), state, true))
	ledger := p3Ledger(t, env.repo, strconv.FormatUint(uint64(record.ID), 10))
	require.NotContains(t, ledger.Delete, output)
	undone, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Zero(t, undone.Failed)
	body, err := afero.ReadFile(env.fs, output)
	require.NoError(t, err)
	require.Equal(t, "later unrelated file", string(body))
}
