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
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
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
	for _, finalFailure := range []string{"", "deadline-under-lock", "transaction"} {
		for _, move := range []bool{false, true} {
			for _, scenario := range []string{"compensated", "owned-refusal", "foreign-successor"} {
				t.Run(fmt.Sprintf("final=%s/move=%v/%s", finalFailure, move, scenario), func(t *testing.T) {
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
					repo := &reconciliationFaultRepo{BatchFileOperationRepositoryInterface: env.repo}
					log := &reconciliationFaultLog{RevertLog: NewDBRevertLog(repo, NewRevertLogConfig(true, nil), env.jobID, fs, nil, nil, nil), repo: repo, mode: finalFailure}
					orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, template.NewEngine(), log, nil, nil)
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
						require.True(t, ledger.PublicationPending, "write-ahead hold survives intermediate completion")
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
					if finalFailure != "" {
						if finalFailure == "deadline-under-lock" {
							require.ErrorIs(t, err, context.DeadlineExceeded)
						} else {
							require.ErrorIs(t, err, errReconcileTransaction)
						}
						held := p3Ledger(t, env.repo, result.OperationID)
						require.True(t, held.PublicationPending)
						row, e := env.repo.FindByID(t.Context(), uint(id))
						require.NoError(t, e)
						require.True(t, models.PrimaryUndoNeedsRetention(*row))
						// A new Reverter reads only persisted state, as after restart.
						untouched := &publicationNoMutationFS{Fs: env.fs}
						refused, e := history.NewReverter(untouched, env.repo).RevertBatch(t.Context(), env.jobID)
						require.NoError(t, e)
						require.Equal(t, 1, refused.Failed)
						require.Zero(t, untouched.writes, "pending publication must block all Undo filesystem writes")
						// Retry uses the original carried snapshot, never re-adopts by path.
						require.NoError(t, log.RevertLog.CompleteFailed(t.Context(), result.OperationID, log.saved))
					}
					ledger := p3Ledger(t, env.repo, result.OperationID)
					require.False(t, ledger.PublicationPending)
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

var errReconcileTransaction = errors.New("synthetic reconciliation transaction failure")

type reconciliationFaultRepo struct {
	database.BatchFileOperationRepositoryInterface
	fail bool
}

func (r *reconciliationFaultRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if r.fail {
		return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			return models.GeneratedFilesJSON{}, false, errReconcileTransaction
		})
	}
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
}

type reconciliationFaultLog struct {
	RevertLog
	repo  *reconciliationFaultRepo
	mode  string
	saved *ApplyResult
}

func (l *reconciliationFaultLog) CompleteFailed(ctx context.Context, id OperationID, result *ApplyResult) error {
	l.saved = result
	if l.mode == "transaction" {
		l.repo.fail = true
		defer func() { l.repo.fail = false }()
	}
	if l.mode == "deadline-under-lock" {
		expired, cancel := context.WithTimeout(ctx, time.Millisecond)
		defer cancel()
		release := fsutil.SharedJournalLocks().Acquire(id)
		done := make(chan error, 1)
		started := make(chan struct{})
		go func() { close(started); done <- l.RevertLog.CompleteFailed(expired, id, result) }()
		<-started
		<-expired.Done()
		release()
		return <-done
	}
	return l.RevertLog.CompleteFailed(ctx, id, result)
}

type publicationNoMutationFS struct {
	afero.Fs
	writes int
}

func (f *publicationNoMutationFS) Remove(p string) error    { f.writes++; return os.ErrPermission }
func (f *publicationNoMutationFS) RemoveAll(p string) error { f.writes++; return os.ErrPermission }
func (f *publicationNoMutationFS) Rename(a, b string) error { f.writes++; return os.ErrPermission }
func (f *publicationNoMutationFS) Mkdir(p string, m os.FileMode) error {
	f.writes++
	return os.ErrPermission
}
func (f *publicationNoMutationFS) MkdirAll(p string, m os.FileMode) error {
	f.writes++
	return os.ErrPermission
}
func (f *publicationNoMutationFS) Create(p string) (afero.File, error) {
	f.writes++
	return nil, os.ErrPermission
}
func (f *publicationNoMutationFS) OpenFile(p string, flag int, m os.FileMode) (afero.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		f.writes++
		return nil, os.ErrPermission
	}
	return f.Fs.OpenFile(p, flag, m)
}
func (f *publicationNoMutationFS) Chmod(p string, m os.FileMode) error {
	f.writes++
	return os.ErrPermission
}
func (f *publicationNoMutationFS) Chown(p string, u, g int) error {
	f.writes++
	return os.ErrPermission
}
func (f *publicationNoMutationFS) Chtimes(p string, a, b time.Time) error {
	f.writes++
	return os.ErrPermission
}

// A failed write-ahead checkpoint cannot publish. A failed terminal success
// cannot report success or expose the intermediate journal to Undo.
func TestResidentPublicationTerminalWrites(t *testing.T) {
	for _, checkpoint := range []bool{true, false} {
		t.Run(fmt.Sprintf("checkpoint=%v", checkpoint), func(t *testing.T) {
			env := setupCopyIntentE2E(t, "resident-terminal")
			require.NoError(t, env.fs.Remove(env.multipart))
			targetDir := filepath.Join(env.dest, env.movie.ID)
			require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
			primary := filepath.Join(targetDir, env.movie.ID+".mp4")
			require.NoError(t, os.Link(env.source, primary))
			repo := &reconciliationFaultRepo{BatchFileOperationRepositoryInterface: env.repo}
			log := &terminalPublicationFaultLog{RevertLog: NewDBRevertLog(repo, NewRevertLogConfig(true, nil), env.jobID, env.fs, nil, nil, nil), repo: repo, checkpoint: checkpoint}
			org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := newApplyOrchestrator(env.fs, org, nil, nil, nil, ApplyConfig{}, template.NewEngine(), log, nil, nil)
			result, err := orch.Execute(t.Context(), ApplyCmd{Movie: &env.movie, Match: env.match, DestPath: env.dest, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: postPublishFence{movie: &env.movie}})
			require.ErrorIs(t, err, errReconcileTransaction)
			ledger := p3Ledger(t, env.repo, result.OperationID)
			if checkpoint {
				require.False(t, ledger.PublicationPending)
				require.Empty(t, ledger.MoveBack)
				entries, e := afero.ReadDir(env.fs, targetDir)
				require.NoError(t, e)
				for _, entry := range entries {
					require.NotEqual(t, ".srt", filepath.Ext(entry.Name()))
				}
				require.FileExists(t, env.subtitle)
			} else {
				require.Equal(t, "revert_log_finalization", result.FailedStep)
				require.True(t, ledger.PublicationPending)
				fs := &publicationNoMutationFS{Fs: env.fs}
				refused, e := history.NewReverter(fs, env.repo).RevertBatch(t.Context(), env.jobID)
				require.NoError(t, e)
				require.Equal(t, 1, refused.Failed)
				require.Zero(t, fs.writes)
				require.NoError(t, log.RevertLog.Complete(t.Context(), result.OperationID, log.saved))
				require.False(t, p3Ledger(t, env.repo, result.OperationID).PublicationPending)
				undone, e := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
				require.NoError(t, e)
				require.Zero(t, undone.Failed)
				require.Equal(t, 1, undone.Succeeded)
				require.FileExists(t, env.subtitle)
			}
		})
	}
}

type terminalPublicationFaultLog struct {
	RevertLog
	repo       *reconciliationFaultRepo
	checkpoint bool
	saved      *ApplyResult
}

func (l *terminalPublicationFaultLog) Complete(ctx context.Context, id OperationID, result *ApplyResult) error {
	fail := result.PublicationFinalized || (l.checkpoint && result.PublicationPending)
	if fail {
		l.saved = result
		l.repo.fail = true
		defer func() { l.repo.fail = false }()
	}
	return l.RevertLog.Complete(ctx, id, result)
}

func TestPublicationPendingMergeCompatibility(t *testing.T) {
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{PublicationPending: true, PrimaryResident: true})
	for _, fresh := range []string{"", `{"delete":["owned"]}`, `{"move_back":[{"original_path":"a","new_path":"b"}]}`} {
		for _, promote := range []bool{false, true} {
			merged := mergeReplacementLedgerIntents(prior, fresh, promote)
			ledger, err := models.ParseGeneratedFiles(merged)
			require.NoError(t, err)
			require.True(t, ledger.PublicationPending)
		}
	}
	legacy, err := models.ParseGeneratedFiles(`{"delete":["legacy"]}`)
	require.NoError(t, err)
	require.False(t, legacy.PublicationPending)
	require.False(t, models.PrimaryUndoNeedsRetention(models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(legacy)}))
}

func TestInPlaceResidentPublicationHoldAfterFailedReconciliation(t *testing.T) {
	env := setupCopyIntentE2E(t, "inplace-publication-hold")
	env.dest = t.TempDir()
	dir := filepath.Join(env.dest, "incoming")
	require.NoError(t, env.fs.MkdirAll(dir, 0755))
	env.movie.ID = "ABC-123"
	env.source = filepath.Join(dir, "abc-123-source.mp4")
	env.subtitle = filepath.Join(dir, "abc-123-source.srt")
	require.NoError(t, afero.WriteFile(env.fs, env.source, []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(env.fs, env.subtitle, []byte("subtitle"), 0644))
	env.match = models.FileMatchInfo{Path: env.source, Name: filepath.Base(env.source), Extension: ".mp4", MovieID: env.movie.ID}
	mt, e := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, e)

	require.NoError(t, env.fs.Remove(env.multipart))
	alias := filepath.Join(filepath.Dir(env.source), env.movie.ID+".mp4")
	require.NoError(t, os.Link(env.source, alias))
	repo := &reconciliationFaultRepo{BatchFileOperationRepositoryInterface: env.repo}
	log := &reconciliationFaultLog{RevertLog: NewDBRevertLog(repo, NewRevertLogConfig(true, nil), env.jobID, env.fs, nil, nil, nil), repo: repo, mode: "transaction"}
	org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), mt)
	orch := newApplyOrchestrator(env.fs, org, nil, nil, nil, ApplyConfig{}, template.NewEngine(), log, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fence := postPublishFence{movie: &env.movie, err: context.Canceled, after: func() {
		rows, e := env.repo.FindByBatchJobID(t.Context(), env.jobID)
		require.NoError(t, e)
		require.Len(t, rows, 1)
		ledger, e := models.ParseGeneratedFiles(rows[0].GeneratedFiles)
		require.NoError(t, e)
		require.True(t, ledger.PublicationPending)
		require.NotNil(t, ledger.DirectoryMove)
		cancel()
	}}
	result, err := orch.Execute(ctx, ApplyCmd{Movie: &env.movie, Match: env.match, DestPath: env.dest, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace, PublicationFence: fence})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, errReconcileTransaction)
	require.True(t, p3Ledger(t, env.repo, result.OperationID).PublicationPending)
	untouched := &publicationNoMutationFS{Fs: env.fs}
	refused, e := history.NewReverter(untouched, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, e)
	require.Equal(t, 1, refused.Failed)
	require.Zero(t, untouched.writes)
	require.NoError(t, log.RevertLog.CompleteFailed(t.Context(), result.OperationID, log.saved))
	require.False(t, p3Ledger(t, env.repo, result.OperationID).PublicationPending)
	undone, e := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, e)
	require.Zero(t, undone.Failed)
	require.FileExists(t, env.source)
	require.FileExists(t, alias)
	require.FileExists(t, env.subtitle)
}

type publicationColumnFaultRepo struct {
	database.BatchFileOperationRepositoryInterface
}

func (r publicationColumnFaultRepo) UpdateNonJournalFields(context.Context, *models.BatchFileOperation) error {
	return errReconcileTransaction
}

func TestPublicationPendingJournalCommitSurvivesColumnFailure(t *testing.T) {
	env := setupCopyIntentE2E(t, "publication-columns")
	output := filepath.Join(filepath.Dir(env.source), "later.jpg")
	require.NoError(t, afero.WriteFile(env.fs, output, []byte("unrelated"), 0644))
	row := &models.BatchFileOperation{BatchJobID: env.jobID, MovieID: env.movie.ID, OriginalPath: env.source, OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryResident: true, PublicationPending: true, Delete: []string{output}})}
	require.NoError(t, env.repo.Create(t.Context(), row))
	id := strconv.FormatUint(uint64(row.ID), 10)
	log := NewDBRevertLog(publicationColumnFaultRepo{env.repo}, NewRevertLogConfig(true, nil), env.jobID, env.fs, nil, nil, nil)
	err := log.CompleteFailed(t.Context(), id, &ApplyResult{OrganizeResult: &organizer.OrganizeResult{PrimaryResident: true, OriginalPath: env.source}, PublicationRecovery: &models.PublicationRecovery{Managed: []string{output}}})
	require.ErrorIs(t, err, errReconcileTransaction)
	ledger := p3Ledger(t, env.repo, id)
	require.False(t, ledger.PublicationPending, "hold clears only with committed safe journal, even if subsequent status update fails")
	require.Empty(t, ledger.Delete)
	undone, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Zero(t, undone.Failed)
	body, err := afero.ReadFile(env.fs, output)
	require.NoError(t, err)
	require.Equal(t, "unrelated", string(body))
}
