package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type atomicCompletionFS struct {
	afero.Fs
	committing          atomic.Bool
	writes, earlyWrites atomic.Int32
}

func (f *atomicCompletionFS) write() {
	f.writes.Add(1)
	if !f.committing.Load() {
		f.earlyWrites.Add(1)
	}
}
func (f *atomicCompletionFS) Rename(a, b string) error            { f.write(); return f.Fs.Rename(a, b) }
func (f *atomicCompletionFS) Remove(p string) error               { f.write(); return f.Fs.Remove(p) }
func (f *atomicCompletionFS) RemoveAll(p string) error            { f.write(); return f.Fs.RemoveAll(p) }
func (f *atomicCompletionFS) Mkdir(p string, m os.FileMode) error { f.write(); return f.Fs.Mkdir(p, m) }
func (f *atomicCompletionFS) MkdirAll(p string, m os.FileMode) error {
	f.write()
	return f.Fs.MkdirAll(p, m)
}
func (f *atomicCompletionFS) Create(p string) (afero.File, error) { f.write(); return f.Fs.Create(p) }
func (f *atomicCompletionFS) OpenFile(p string, flags int, m os.FileMode) (afero.File, error) {
	if flags&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		f.write()
	}
	return f.Fs.OpenFile(p, flags, m)
}
func (f *atomicCompletionFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}

type atomicCompletionFixture struct {
	publisherDB                               *database.DB
	publisher, observer                       *database.BatchFileOperationRepository
	row                                       *models.BatchFileOperation
	id                                        string
	fs                                        *atomicCompletionFS
	result                                    *ApplyResult
	kind, source, target, oldDir, newDir, nfo string
	primaryDev, primaryIno                    uint64
}

func newAtomicCompletionFixture(t *testing.T, kind string) *atomicCompletionFixture {
	t.Helper()
	db, dsn := pr260ArtifactDB(t)
	otherDB, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherDB.Close() })
	base := afero.NewOsFs()
	root := t.TempDir()
	f := &atomicCompletionFixture{publisherDB: db, publisher: database.NewBatchFileOperationRepository(db), observer: database.NewBatchFileOperationRepository(otherDB), fs: &atomicCompletionFS{Fs: base}, kind: kind, oldDir: filepath.Join(root, "incoming"), newDir: filepath.Join(root, "published")}
	f.source, f.target = filepath.Join(f.oldDir, "source.mp4"), filepath.Join(f.newDir, "renamed.mp4")
	f.nfo = filepath.Join(f.newDir, "movie.nfo")
	require.NoError(t, base.MkdirAll(f.newDir, 0755))
	if strings.HasPrefix(kind, "NoOp") || strings.HasSuffix(kind, "Nil") {
		require.NoError(t, base.MkdirAll(f.oldDir, 0755))
		require.NoError(t, afero.WriteFile(base, f.source, []byte("unmoved source"), 0644))
		require.NoError(t, afero.WriteFile(base, f.target, []byte("other operation primary"), 0644))
	} else {
		require.NoError(t, afero.WriteFile(base, f.target, []byte("published primary"), 0644))
	}
	require.NoError(t, afero.WriteFile(base, f.nfo, []byte("generated nfo"), 0644))
	identity, err := base.Stat(f.target)
	require.NoError(t, err)
	var strong bool
	f.primaryDev, f.primaryIno, strong = fsutil.BoundObjectIdentity(base, f.target, identity)
	require.True(t, strong, "capture native primary identity eagerly before Undo")
	f.row = &models.BatchFileOperation{BatchJobID: "atomic-completion", MovieID: "ATOMIC-001", OriginalPath: f.source, OriginalDirPath: f.oldDir, OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: "{}"}
	if strings.HasSuffix(kind, "Nil") {
		f.row.NewPath = f.target
	}
	require.NoError(t, f.publisher.Create(t.Context(), f.row))
	f.id = strconv.FormatUint(uint64(f.row.ID), 10)
	f.result = &ApplyResult{Movie: &models.Movie{ID: f.row.MovieID}, PublicationFinalized: !strings.HasPrefix(kind, "Intermediate"), NFOPath: f.nfo, OrganizeResult: &organizer.OrganizeResult{OriginalPath: f.source, NewPath: f.target, FolderPath: f.newDir, OldDirectoryPath: f.oldDir, Moved: true, InPlaceRenamed: kind == "IntermediateDirectory"}}
	if strings.HasPrefix(kind, "NoOp") {
		f.result.PrePublication = true
	}
	if strings.HasSuffix(kind, "Nil") {
		f.result = nil
	}
	return f
}

func (f *atomicCompletionFixture) complete(ctx context.Context, repo database.BatchFileOperationRepositoryInterface) error {
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), f.row.BatchJobID, f.fs, nil, nil, nil)
	if f.kind == "Failure" || f.kind == "NoOpFailure" || f.kind == "CompleteFailedNil" {
		return log.CompleteFailed(ctx, f.id, f.result)
	}
	return log.Complete(ctx, f.id, f.result)
}

func (f *atomicCompletionFixture) read(t *testing.T) *models.BatchFileOperation {
	t.Helper()
	row, err := f.observer.FindByID(t.Context(), f.row.ID)
	require.NoError(t, err)
	require.NotNil(t, row)
	return row
}

func (f *atomicCompletionFixture) assertCommitted(t *testing.T, row *models.BatchFileOperation) {
	t.Helper()
	status := models.RevertStatusApplied
	if f.kind == "Failure" || strings.HasSuffix(f.kind, "Nil") {
		status = models.RevertStatusFailed
	}
	if strings.HasPrefix(f.kind, "NoOp") {
		status = models.RevertStatusNoOp
	}
	assert.Equal(t, status, row.RevertStatus)
	assert.Equal(t, f.oldDir, row.OriginalDirPath)
	ledger := applyAdmissionLedger(t, row)
	if strings.HasPrefix(f.kind, "Intermediate") {
		assert.Nil(t, ledger.ApplyTerminal)
	} else {
		assert.NotNil(t, ledger.ApplyTerminal)
	}
	if strings.HasPrefix(f.kind, "NoOp") || strings.HasSuffix(f.kind, "Nil") {
		assert.Empty(t, row.NewPath)
		assert.Empty(t, row.NFOPath)
		assert.Empty(t, ledger.Delete)
	} else {
		assert.Equal(t, f.target, row.NewPath)
		assert.Equal(t, f.nfo, row.NFOPath)
		assert.Equal(t, f.kind == "IntermediateDirectory", row.InPlaceRenamed)
		assert.Contains(t, ledger.Delete, f.nfo)
	}
}

func (f *atomicCompletionFixture) assertUndone(t *testing.T, outcome *history.RevertBatchResult) {
	t.Helper()
	if strings.HasPrefix(f.kind, "NoOp") {
		require.Equal(t, models.RevertStatusNoOp, f.read(t).RevertStatus)
		require.Zero(t, f.fs.writes.Load())
		require.Equal(t, "unmoved source", string(mustReadStagedTxn(t, f.fs.Fs, f.source)))
		require.Equal(t, "other operation primary", string(mustReadStagedTxn(t, f.fs.Fs, f.target)))
		require.Equal(t, "generated nfo", string(mustReadStagedTxn(t, f.fs.Fs, f.nfo)))
		return
	}
	require.Equal(t, 1, outcome.Succeeded, "%+v", outcome.Outcomes)
	require.Zero(t, outcome.Failed, "%+v", outcome.Outcomes)
	require.Zero(t, outcome.Skipped, "%+v", outcome.Outcomes)
	require.Equal(t, "published primary", string(mustReadStagedTxn(t, f.fs.Fs, f.source)))
	restored, statErr := f.fs.Stat(f.source)
	require.NoError(t, statErr)
	dev, ino, strong := fsutil.BoundObjectIdentity(f.fs.Fs, f.source, restored)
	require.True(t, strong, "capture restored identity eagerly at its new path")
	require.Equal(t, f.primaryDev, dev)
	require.Equal(t, f.primaryIno, ino, "Undo must restore the exact published primary object")
	_, err := f.fs.Stat(f.target)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, models.RevertStatusReverted, f.read(t).RevertStatus)
}

func atomicCompletionHooks(t *testing.T, db *database.DB, update func(int, string, string, int64), commit func() int) {
	t.Helper()
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	set := func(update func(int, string, string, int64), commit func() int) {
		conn, err := sqlDB.Conn(context.Background())
		require.NoError(t, err)
		require.NoError(t, conn.Raw(func(raw any) error {
			sqlite := raw.(*sqlite3.SQLiteConn)
			sqlite.RegisterUpdateHook(update)
			sqlite.RegisterCommitHook(commit)
			return nil
		}))
		require.NoError(t, conn.Close())
	}
	set(update, commit)
	t.Cleanup(func() { set(nil, nil) })
}

type atomicCompletionReturnBarrier struct {
	database.BatchFileOperationRepositoryInterface
	ready, resume chan struct{}
	once          sync.Once
}

func (r *atomicCompletionReturnBarrier) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	err := r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
	if err == nil {
		r.once.Do(func() { close(r.ready); <-r.resume })
	}
	return err
}

func TestApplyCompletionAtomicVisibleBeforeJournalMethodReturns(t *testing.T) {
	for _, kind := range []string{"Success", "Failure", "NoOpSuccess", "NoOpFailure", "IntermediateMove", "IntermediateDirectory", "CompleteNil", "CompleteFailedNil"} {
		t.Run(kind, func(t *testing.T) {
			f := newAtomicCompletionFixture(t, kind)
			barrier := &atomicCompletionReturnBarrier{BatchFileOperationRepositoryInterface: f.publisher, ready: make(chan struct{}), resume: make(chan struct{})}
			done := make(chan error, 1)
			var release sync.Once
			go func() { defer close(done); done <- f.complete(t.Context(), barrier) }()
			defer func() {
				release.Do(func() { close(barrier.resume) })
				for range done {
				}
			}()
			select {
			case <-barrier.ready:
			case err := <-done:
				t.Fatalf("completion did not reach committed journal barrier: %v", err)
			}
			f.assertCommitted(t, f.read(t))
			if strings.HasSuffix(kind, "Nil") {
				require.Zero(t, f.fs.writes.Load())
				release.Do(func() { close(barrier.resume) })
				require.NoError(t, <-done)
				return
			}
			f.fs.committing.Store(true)
			// Public Undo runs while Complete is still paused at the former
			// journal/columns gap, through a wholly independent SQLite pool.
			undo, err := history.NewReverter(f.fs, f.observer).RevertBatch(t.Context(), f.row.BatchJobID)
			require.NoError(t, err)
			f.assertUndone(t, undo)
			if strings.HasPrefix(kind, "NoOp") {
				require.Zero(t, undo.Total)
				require.Zero(t, undo.Failed)
			}
			release.Do(func() { close(barrier.resume) })
			require.NoError(t, <-done, "completion must have no trailing stale metadata write")
		})
	}
}

type atomicCompletionUndoRepo struct {
	database.BatchFileOperationRepositoryInterface
	read                  chan *models.BatchFileOperation
	attempt               chan struct{}
	readOnce, attemptOnce sync.Once
}

func (r *atomicCompletionUndoRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	row, err := r.BatchFileOperationRepositoryInterface.FindByID(ctx, id)
	if err == nil {
		r.readOnce.Do(func() { copy := *row; r.read <- &copy })
	}
	return row, err
}
func (r *atomicCompletionUndoRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.attemptOnce.Do(func() { close(r.attempt) })
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
}

func TestApplyCompletionAtomicUndoSeesFreshMetadataAfterCommit(t *testing.T) {
	for _, kind := range []string{"Success", "Failure", "NoOpSuccess", "NoOpFailure", "IntermediateMove", "IntermediateDirectory"} {
		t.Run(kind, func(t *testing.T) {
			f := newAtomicCompletionFixture(t, kind)
			before := f.read(t)
			updated, resume := make(chan struct{}), make(chan struct{})
			var barrierOnce, release sync.Once
			atomicCompletionHooks(t, f.publisherDB, func(op int, db, table string, id int64) {
				if op == sqlite3.SQLITE_UPDATE && table == "batch_file_operations" && id == int64(f.row.ID) {
					barrierOnce.Do(func() { close(updated); <-resume })
				}
			}, func() int { f.fs.committing.Store(true); return 0 })
			done := make(chan error, 1)
			go func() { defer close(done); done <- f.complete(t.Context(), f.publisher) }()
			defer func() {
				release.Do(func() { close(resume) })
				for range done {
				}
			}()
			select {
			case <-updated:
			case err := <-done:
				t.Fatalf("completion never reached SQLite update hook: %v", err)
			}
			require.Equal(t, before, f.read(t), "another connection cannot see any partial transaction")
			observer := &atomicCompletionUndoRepo{BatchFileOperationRepositoryInterface: f.observer, read: make(chan *models.BatchFileOperation, 1), attempt: make(chan struct{})}
			undoDone := runDurableAdmissionUndo(t.Context(), f.fs, observer, f.row.BatchJobID)
			defer func() {
				release.Do(func() { close(resume) })
				for range undoDone {
				}
			}()
			select {
			case stale := <-observer.read:
				require.Equal(t, before, stale)
			case result := <-undoDone:
				t.Fatalf("Undo missed stale-read barrier: %+v", result)
			}
			select {
			case <-observer.attempt:
			case result := <-undoDone:
				t.Fatalf("Undo never attempted durable admission: %+v", result)
			}
			require.Zero(t, f.fs.writes.Load(), "Undo must not mutate files before the in-flight completion commits")
			release.Do(func() { close(resume) })
			require.NoError(t, <-done)
			outcome := <-undoDone
			require.NoError(t, outcome.err)
			require.Zero(t, f.fs.earlyWrites.Load(), "no filesystem write precedes the publisher COMMIT hook")
			f.assertUndone(t, outcome.result)
			if strings.HasPrefix(kind, "NoOp") {
				fresh, err := history.NewReverter(f.fs, f.observer).RevertBatch(t.Context(), f.row.BatchJobID)
				require.NoError(t, err)
				require.Zero(t, fresh.Total, "a fresh Undo excludes the completed no-op row")
				require.Zero(t, fresh.Failed)
				require.Zero(t, f.fs.writes.Load())
			}
		})
	}
}

func TestApplyCompletionAtomicFailureRollsBackAndRetries(t *testing.T) {
	for _, kind := range []string{"Success", "Failure", "NoOpSuccess", "NoOpFailure", "IntermediateMove", "IntermediateDirectory", "CompleteNil", "CompleteFailedNil"} {
		for _, failure := range []string{"columns", "commit"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				f := newAtomicCompletionFixture(t, kind)
				before := f.read(t)
				var reject atomic.Bool
				reject.Store(true)
				var commits atomic.Int32
				if failure == "columns" {
					require.NoError(t, f.publisherDB.Exec("CREATE TRIGGER reject_completion_columns BEFORE UPDATE OF new_path ON batch_file_operations BEGIN SELECT RAISE(ABORT, 'injected completion columns failure'); END").Error)
				} else {
					atomicCompletionHooks(t, f.publisherDB, nil, func() int {
						commits.Add(1)
						if reject.Load() {
							return 1
						}
						return 0
					})
				}
				err := f.complete(t.Context(), f.publisher)
				require.Error(t, err)
				if failure == "columns" {
					require.Contains(t, err.Error(), "injected completion columns failure")
				} else {
					require.Equal(t, int32(1), commits.Load())
				}
				require.Equal(t, before, f.read(t), "journal, terminal, status, paths, and timestamp must all roll back")
				require.Zero(t, f.fs.writes.Load())
				if failure == "columns" {
					require.NoError(t, f.publisherDB.Exec("DROP TRIGGER reject_completion_columns").Error)
				} else {
					reject.Store(false)
				}
				require.NoError(t, f.complete(t.Context(), f.publisher))
				f.assertCommitted(t, f.read(t))
			})
		}
	}
}

func TestApplyCompletionAtomicExactTerminalRetryDoesNotUpdateRow(t *testing.T) {
	for _, kind := range []string{"Success", "Failure", "NoOpSuccess", "NoOpFailure", "CompleteNil", "CompleteFailedNil"} {
		t.Run(kind, func(t *testing.T) {
			f := newAtomicCompletionFixture(t, kind)
			require.NoError(t, f.complete(t.Context(), f.publisher))
			before := f.read(t)
			var updates atomic.Int32
			atomicCompletionHooks(t, f.publisherDB, func(op int, db, table string, id int64) {
				if op == sqlite3.SQLITE_UPDATE && table == "batch_file_operations" {
					updates.Add(1)
				}
			}, nil)
			require.NoError(t, f.complete(t.Context(), f.publisher))
			require.Equal(t, before, f.read(t), "exact retry cannot rewrite any metadata, ledger byte, or timestamp")
			require.Zero(t, updates.Load(), "exact terminal retry is read-only inside its admission transaction")
		})
	}
}

type atomicCompletionReloadFailureRepo struct {
	database.BatchFileOperationRepositoryInterface
	admitted atomic.Bool
	missing  bool
}

func (r *atomicCompletionReloadFailureRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	err := r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
	if err == nil {
		r.admitted.Store(true)
	}
	return err
}
func (r *atomicCompletionReloadFailureRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	if r.admitted.Load() {
		if r.missing {
			return nil, nil
		}
		return nil, errors.New("injected admitted Undo reload failure")
	}
	return r.BatchFileOperationRepositoryInterface.FindByID(ctx, id)
}

func TestApplyCompletionAtomicUndoReloadFailureKeepsAdmissionWithoutFilesystemWrites(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "read-error"
		if missing {
			name = "missing-row"
		}
		t.Run(name, func(t *testing.T) {
			f := newAtomicCompletionFixture(t, "Success")
			require.NoError(t, f.complete(t.Context(), f.publisher))
			repo := &atomicCompletionReloadFailureRepo{BatchFileOperationRepositoryInterface: f.observer, missing: missing}
			outcome, err := history.NewReverter(f.fs, repo).RevertBatch(t.Context(), f.row.BatchJobID)
			require.NoError(t, err)
			require.Equal(t, 1, outcome.Failed)
			require.Contains(t, outcome.Outcomes[0].Error, "reload admitted Undo operation")
			require.Zero(t, f.fs.writes.Load())
			admitted := f.read(t)
			require.True(t, applyAdmissionLedger(t, admitted).UndoStarted)
			require.Equal(t, models.RevertStatusApplied, admitted.RevertStatus)
			require.Equal(t, "published primary", string(mustReadStagedTxn(t, f.fs.Fs, f.target)))
			f.fs.committing.Store(true)
			retried, err := history.NewReverter(f.fs, f.observer).RevertBatch(t.Context(), f.row.BatchJobID)
			require.NoError(t, err)
			f.assertUndone(t, retried)
		})
	}
}
