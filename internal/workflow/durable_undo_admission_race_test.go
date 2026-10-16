package workflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/mattn/go-sqlite3"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The publisher and Undo use independent SQLite pools. Undo admission does
// not acquire the publisher's workflow journal mutex; the barriers exercise
// their durable transaction ordering. No sleep, external service, or mocked
// durable journal is involved.
type durableAdmissionFixture struct {
	db                  *database.DB
	repo, publisherRepo *database.BatchFileOperationRepository
	fs                  afero.Fs
	log                 RevertLog
	row                 *models.BatchFileOperation
	id                  OperationID
	primary, artifact   string
	replaced, backup    string
}

func newDurableAdmissionFixture(t *testing.T, replacement bool) *durableAdmissionFixture {
	t.Helper()
	db, dsn := pr260ArtifactDB(t)
	publisherDB, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = publisherDB.Close() })
	f := &durableAdmissionFixture{db: db, repo: database.NewBatchFileOperationRepository(db), publisherRepo: database.NewBatchFileOperationRepository(publisherDB), fs: afero.NewOsFs()}
	root := t.TempDir()
	f.primary = filepath.Join(root, "resident.mp4")
	f.artifact = filepath.Join(root, "installed.srt")
	require.NoError(t, afero.WriteFile(f.fs, f.primary, []byte("resident primary"), 0644))
	require.NoError(t, afero.WriteFile(f.fs, f.artifact, []byte("installed subtitle"), 0644))
	info, err := f.fs.Stat(f.artifact)
	require.NoError(t, err)
	dev, ino, strong := fsutil.BoundObjectIdentity(f.fs, f.artifact, info)
	require.True(t, strong)
	ledger := models.GeneratedFilesJSON{PrimaryResident: true, PlannedDeletes: []models.DeleteEntry{{
		Path: f.artifact, RecoveryPublication: true, IdentityPinned: true, IdentityStrong: true,
		IdentityDev: dev, IdentityIno: ino, IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix(),
	}}}
	if replacement {
		f.replaced = filepath.Join(root, "poster.jpg")
		// A legacy explicit backup path keeps the pre-revert directory sweep
		// read-only; recognizable .dlbak names get their own test below.
		f.backup = filepath.Join(root, "poster.prior")
		require.NoError(t, afero.WriteFile(f.fs, f.replaced, []byte("installed poster"), 0644))
		require.NoError(t, afero.WriteFile(f.fs, f.backup, []byte("prior poster"), 0644))
		installed, err := f.fs.Stat(f.replaced)
		require.NoError(t, err)
		backup, err := f.fs.Stat(f.backup)
		require.NoError(t, err)
		ledger.Replacements = []models.ReplacementEntry{{Destination: f.replaced, Backup: f.backup, DestSeq: 1, Installed: true,
			InstalledSize: installed.Size(), InstalledModUnix: installed.ModTime().Unix(), InstalledSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("installed poster"))),
			BackupSize: backup.Size(), BackupModUnix: backup.ModTime().Unix(), BackupSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("prior poster"))),
		}}
	}
	f.row = &models.BatchFileOperation{BatchJobID: "durable-admission", MovieID: "ADMISSION-001", OriginalPath: f.primary, OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
	require.NoError(t, f.repo.Create(t.Context(), f.row))
	f.id = strconv.FormatUint(uint64(f.row.ID), 10)
	f.log = NewDBRevertLog(f.publisherRepo, NewRevertLogConfig(true, nil), f.row.BatchJobID, f.fs, nil, nil, nil)
	return f
}

func (f *durableAdmissionFixture) completion(pending, finalized bool) *ApplyResult {
	return &ApplyResult{PublicationPending: pending, PublicationFinalized: finalized, Movie: &models.Movie{ID: f.row.MovieID}, OrganizeResult: &organizer.OrganizeResult{PrimaryResident: true, OriginalPath: f.primary, NewPath: f.primary}}
}

func (f *durableAdmissionFixture) assertUntouched(t *testing.T) {
	t.Helper()
	require.Equal(t, "resident primary", string(mustReadStagedTxn(t, f.fs, f.primary)))
	require.Equal(t, "installed subtitle", string(mustReadStagedTxn(t, f.fs, f.artifact)))
	if f.backup != "" {
		require.Equal(t, "prior poster", string(mustReadStagedTxn(t, f.fs, f.backup)))
		require.Equal(t, "installed poster", string(mustReadStagedTxn(t, f.fs, f.replaced)))
	}
}

func (f *durableAdmissionFixture) assertReverted(t *testing.T) {
	t.Helper()
	require.Equal(t, "resident primary", string(mustReadStagedTxn(t, f.fs, f.primary)))
	_, err := f.fs.Stat(f.artifact)
	require.ErrorIs(t, err, os.ErrNotExist)
	if f.backup != "" {
		require.Equal(t, "prior poster", string(mustReadStagedTxn(t, f.fs, f.replaced)))
		_, err = f.fs.Stat(f.backup)
		require.ErrorIs(t, err, os.ErrNotExist)
		require.Empty(t, p3Ledger(t, f.publisherRepo, f.id).Replacements)
	}
}

type durableAdmissionBarrierRepo struct {
	database.BatchFileOperationRepositoryInterface
	readReady chan *models.BatchFileOperation
	beforeTx  chan struct{}
	afterTx   chan struct{}
	resume    chan struct{}
	once      sync.Once
}

func (r *durableAdmissionBarrierRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	row, err := r.BatchFileOperationRepositoryInterface.FindByID(ctx, id)
	if err == nil && r.readReady != nil {
		r.once.Do(func() { r.readReady <- row; <-r.resume })
	}
	return row, err
}

func (r *durableAdmissionBarrierRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if r.beforeTx != nil {
		r.once.Do(func() { close(r.beforeTx); <-r.resume })
	}
	err := r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
	if err == nil && r.afterTx != nil {
		r.once.Do(func() { close(r.afterTx); <-r.resume })
	}
	return err
}

type durableAdmissionUndoResult struct {
	result *history.RevertBatchResult
	err    error
}

func runDurableAdmissionUndo(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface, job string) <-chan durableAdmissionUndoResult {
	done := make(chan durableAdmissionUndoResult, 1)
	go func() {
		defer close(done)
		result, err := history.NewReverter(fs, repo).RevertBatch(ctx, job)
		done <- durableAdmissionUndoResult{result: result, err: err}
	}()
	return done
}

func TestDurableUndoAdmissionPublicationCommitsAfterStaleRead(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement=%v", replacement), func(t *testing.T) {
			f := newDurableAdmissionFixture(t, replacement)
			untouched := &publicationNoMutationFS{Fs: f.fs}
			repo := &durableAdmissionBarrierRepo{BatchFileOperationRepositoryInterface: f.repo, readReady: make(chan *models.BatchFileOperation, 1), resume: make(chan struct{})}
			var release sync.Once
			done := runDurableAdmissionUndo(t.Context(), untouched, repo, f.row.BatchJobID)
			defer func() {
				release.Do(func() { close(repo.resume) })
				for range done {
				}
			}()
			var stale *models.BatchFileOperation
			select {
			case stale = <-repo.readReady:
			case outcome := <-done:
				t.Fatalf("Undo returned before its stale-read barrier: %+v", outcome)
			}
			ledger, err := models.ParseGeneratedFiles(stale.GeneratedFiles)
			require.NoError(t, err)
			require.False(t, ledger.PublicationPending)
			require.False(t, ledger.UndoStarted)
			require.NoError(t, f.log.Complete(t.Context(), f.id, f.completion(true, false)))
			held := p3Ledger(t, f.publisherRepo, f.id)
			require.True(t, held.PublicationPending)
			require.False(t, held.UndoStarted)
			release.Do(func() { close(repo.resume) })
			outcome := <-done
			require.NoError(t, outcome.err)
			require.Equal(t, 1, outcome.result.Failed)
			require.Contains(t, outcome.result.Outcomes[0].Error, "publication reconciliation is pending")
			require.Zero(t, untouched.writes, "stale read must not authorize any filesystem mutation")
			f.assertUntouched(t)
			require.Equal(t, held, p3Ledger(t, f.publisherRepo, f.id))
			// Only a durable finalization reopens Undo after publication wins.
			require.NoError(t, f.log.Complete(t.Context(), f.id, f.completion(false, true)))
			retried, err := history.NewReverter(f.fs, f.repo).RevertBatch(t.Context(), f.row.BatchJobID)
			require.NoError(t, err)
			require.Equal(t, 1, retried.Succeeded)
			require.Zero(t, retried.Failed)
			f.assertReverted(t)
		})
	}
}

func TestDurableUndoAdmissionRejectsCheckpointWhileStatusApplied(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement=%v", replacement), func(t *testing.T) {
			f := newDurableAdmissionFixture(t, replacement)
			refusingFS := &publicationNoMutationFS{Fs: f.fs}
			repo := &durableAdmissionBarrierRepo{BatchFileOperationRepositoryInterface: f.repo, afterTx: make(chan struct{}), resume: make(chan struct{})}
			var release sync.Once
			done := runDurableAdmissionUndo(t.Context(), refusingFS, repo, f.row.BatchJobID)
			defer func() {
				release.Do(func() { close(repo.resume) })
				for range done {
				}
			}()
			select {
			case <-repo.afterTx: // the real SQLite transaction has already COMMITted
			case outcome := <-done:
				t.Fatalf("Undo returned before its admission-commit barrier: %+v", outcome)
			}
			admitted, err := f.publisherRepo.FindByID(t.Context(), f.row.ID)
			require.NoError(t, err)
			require.Equal(t, models.RevertStatusApplied, admitted.RevertStatus)
			require.True(t, p3Ledger(t, f.publisherRepo, f.id).UndoStarted)
			require.Zero(t, refusingFS.writes, "Undo admission commits before its first filesystem write")
			checkpoint := f.completion(true, false)
			require.ErrorContains(t, f.log.Complete(t.Context(), f.id, checkpoint), "admitted to Undo")
			// The rejected checkpoint's failure path must not resurrect a stale
			// Delete contribution, including when Pending is false on that call.
			late := f.completion(false, false)
			late.DownloadPaths = []string{f.artifact}
			require.ErrorContains(t, f.log.CompleteFailed(t.Context(), f.id, late), "admitted to Undo")
			unchanged, err := f.publisherRepo.FindByID(t.Context(), f.row.ID)
			require.NoError(t, err)
			require.Equal(t, admitted.GeneratedFiles, unchanged.GeneratedFiles)
			require.Equal(t, models.RevertStatusApplied, unchanged.RevertStatus)
			release.Do(func() { close(repo.resume) })
			outcome := <-done
			require.NoError(t, outcome.err)
			require.Equal(t, 1, outcome.result.Failed)
			require.Positive(t, refusingFS.writes, "the admitted Undo reached the injected filesystem refusal")
			f.assertUntouched(t)
			require.True(t, p3Ledger(t, f.publisherRepo, f.id).UndoStarted)
			// A newly constructed Reverter on the other connection can retry
			// an admitted but failed Undo without reopening publication.
			retried, err := history.NewReverter(f.fs, f.publisherRepo).RevertBatch(t.Context(), f.row.BatchJobID)
			require.NoError(t, err)
			require.Equal(t, 1, retried.Succeeded)
			require.Zero(t, retried.Failed)
			f.assertReverted(t)
			terminal, err := f.publisherRepo.FindByID(t.Context(), f.row.ID)
			require.NoError(t, err)
			require.True(t, p3Ledger(t, f.publisherRepo, f.id).UndoStarted)
			require.ErrorContains(t, f.log.Complete(t.Context(), f.id, late), "admitted to Undo")
			require.ErrorContains(t, f.log.CompleteFailed(t.Context(), f.id, late), "admitted to Undo")
			after, err := f.publisherRepo.FindByID(t.Context(), f.row.ID)
			require.NoError(t, err)
			require.Equal(t, terminal.GeneratedFiles, after.GeneratedFiles, "completed Undo claims cannot be resurrected")
			require.Equal(t, models.RevertStatusReverted, after.RevertStatus)
		})
	}
}

type durableAdmissionCallbackFailureRepo struct {
	database.BatchFileOperationRepositoryInterface
	err       error
	attempted bool
}

func (r *durableAdmissionCallbackFailureRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, persist, err := fn(current)
		if err != nil {
			return next, persist, err
		}
		r.attempted = persist && next.UndoStarted
		return next, persist, r.err
	})
}

func TestDurableUndoAdmissionFailurePrecedesFilesystemMutation(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		for _, failure := range []string{"callback", "commit"} {
			t.Run(fmt.Sprintf("replacement=%v/%s", replacement, failure), func(t *testing.T) {
				f := newDurableAdmissionFixture(t, replacement)
				var repo database.BatchFileOperationRepositoryInterface = f.repo
				callback := &durableAdmissionCallbackFailureRepo{BatchFileOperationRepositoryInterface: f.repo, err: errors.New("injected Undo admission callback failure")}
				var commits atomic.Int32
				clearHook := func() {}
				if failure == "callback" {
					repo = callback
				} else {
					// Reject the native SQLite COMMIT itself, after its UPDATE
					// succeeds. A single-connection pool pins the installed hook
					// to the same connection used by UpdateJournalInTx.
					sqlDB, err := f.db.DB.DB()
					require.NoError(t, err)
					sqlDB.SetMaxOpenConns(1)
					sqlDB.SetMaxIdleConns(1)
					setHook := func(hook func() int) {
						conn, err := sqlDB.Conn(context.Background())
						require.NoError(t, err)
						require.NoError(t, conn.Raw(func(raw any) error { raw.(*sqlite3.SQLiteConn).RegisterCommitHook(hook); return nil }))
						require.NoError(t, conn.Close())
					}
					setHook(func() int { commits.Add(1); return 1 })
					clearHook = func() { setHook(nil) }
					defer clearHook()
				}
				untouched := &publicationNoMutationFS{Fs: f.fs}
				result, err := history.NewReverter(untouched, repo).RevertBatch(t.Context(), f.row.BatchJobID)
				require.NoError(t, err)
				require.Equal(t, 1, result.Failed)
				require.Zero(t, untouched.writes)
				if failure == "callback" {
					require.True(t, callback.attempted, "failure follows the real admission callback")
					require.Contains(t, result.Outcomes[0].Error, callback.err.Error())
				} else {
					require.Equal(t, int32(1), commits.Load(), "native SQLite commit hook rejected the admission write")
				}
				stored, err := f.publisherRepo.FindByID(t.Context(), f.row.ID)
				require.NoError(t, err)
				require.Equal(t, f.row.GeneratedFiles, stored.GeneratedFiles)
				require.Equal(t, models.RevertStatusApplied, stored.RevertStatus)
				f.assertUntouched(t)
				clearHook()
				retried, err := history.NewReverter(f.fs, f.repo).RevertBatch(t.Context(), f.row.BatchJobID)
				require.NoError(t, err)
				require.Equal(t, 1, retried.Succeeded)
				require.Zero(t, retried.Failed)
				f.assertReverted(t)
			})
		}
	}
}

func TestDurableUndoAdmissionPreSweepCannotRestoreAfterCheckpoint(t *testing.T) {
	f := newDurableAdmissionFixture(t, true)
	recognizable := f.replaced + ".dlbak.0123456789abcdef"
	require.NoError(t, f.fs.Rename(f.backup, recognizable))
	f.backup = recognizable
	require.NoError(t, f.fs.Remove(f.replaced))
	require.NoError(t, f.repo.UpdateJournalInTx(t.Context(), f.row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		ledger.Replacements[0].Backup = recognizable
		ledger.Replacements[0].Installed = false // provable pre-install crash window
		return ledger, true, err
	}))
	repo := &durableAdmissionBarrierRepo{BatchFileOperationRepositoryInterface: f.repo, beforeTx: make(chan struct{}), resume: make(chan struct{})}
	var release sync.Once
	done := runDurableAdmissionUndo(t.Context(), f.fs, repo, f.row.BatchJobID)
	defer func() {
		release.Do(func() { close(repo.resume) })
		for range done {
		}
	}()
	select {
	case <-repo.beforeTx: // pre-sweep classified the old row, but has not admitted recovery
	case outcome := <-done:
		t.Fatalf("Undo returned before its sweep-admission barrier: %+v", outcome)
	}
	require.NoError(t, f.log.Complete(t.Context(), f.id, f.completion(true, false)))
	held := p3Ledger(t, f.publisherRepo, f.id)
	require.True(t, held.PublicationPending)
	require.False(t, held.UndoStarted)
	release.Do(func() { close(repo.resume) })
	outcome := <-done
	require.NoError(t, outcome.err)
	require.Equal(t, 1, outcome.result.Failed)
	require.Equal(t, "resident primary", string(mustReadStagedTxn(t, f.fs, f.primary)))
	require.Equal(t, "installed subtitle", string(mustReadStagedTxn(t, f.fs, f.artifact)))
	require.Equal(t, "prior poster", string(mustReadStagedTxn(t, f.fs, f.backup)))
	_, err := f.fs.Stat(f.replaced)
	require.ErrorIs(t, err, os.ErrNotExist, "pre-sweep may coordinate with .dlbusy markers but must not restore held bytes")
	require.Equal(t, held, p3Ledger(t, f.publisherRepo, f.id))
}
