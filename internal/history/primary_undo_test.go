package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type primaryUndoFixture struct {
	t    *testing.T
	db   *database.DB
	repo *database.BatchFileOperationRepository
	path string
	op   *models.BatchFileOperation
	fs   afero.Fs
}

func newPrimaryUndoFixture(t *testing.T) *primaryUndoFixture {
	t.Helper()
	if err := primaryUndoDurabilitySupported(); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	f := &primaryUndoFixture{t: t, path: filepath.Join(root, "history.sqlite"), fs: afero.NewOsFs()}
	f.open()
	require.NoError(t, f.db.AutoMigrate(&models.BatchFileOperation{}, &models.Job{}))
	source := filepath.Join(root, "library", "movie.mkv")
	original := filepath.Join(root, "incoming", "movie.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(source), 0755))
	require.NoError(t, os.MkdirAll(filepath.Dir(original), 0755))
	require.NoError(t, os.WriteFile(source, []byte("synthetic primary video"), 0600))
	stamp := time.Unix(1700000000, 0)
	require.NoError(t, os.Chtimes(source, stamp, stamp))
	f.op = &models.BatchFileOperation{BatchJobID: "cross-volume-undo", MovieID: "SYNTH-001", OriginalPath: original, NewPath: source, OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{filepath.Dir(source)}})}
	require.NoError(t, f.repo.Create(context.Background(), f.op))
	t.Cleanup(func() {
		if f.db != nil {
			require.NoError(t, f.db.Close())
		}
	})
	return f
}
func (f *primaryUndoFixture) open() {
	f.t.Helper()
	var err error
	f.db, err = database.New(&database.Config{Type: "sqlite", DSN: f.path, LogLevel: "silent"})
	require.NoError(f.t, err)
	f.repo = database.NewBatchFileOperationRepository(f.db)
}
func (f *primaryUndoFixture) reopen() {
	f.t.Helper()
	require.NoError(f.t, f.db.Close())
	f.open()
	f.reload()
}
func (f *primaryUndoFixture) reload() {
	f.t.Helper()
	var err error
	f.op, err = f.repo.FindByID(context.Background(), f.op.ID)
	require.NoError(f.t, err)
}
func (f *primaryUndoFixture) journal() *models.PrimaryUndo {
	f.t.Helper()
	f.reload()
	g, e := models.ParseGeneratedFiles(f.op.GeneratedFiles)
	require.NoError(f.t, e)
	return g.PrimaryUndo
}
func (f *primaryUndoFixture) resume() error {
	f.t.Helper()
	j := f.journal()
	require.NotNil(f.t, j)
	return resumePrimaryUndo(context.Background(), f.fs, f.repo, f.op, j)
}
func (f *primaryUndoFixture) success() {
	f.t.Helper()
	got, e := os.ReadFile(f.op.OriginalPath)
	require.NoError(f.t, e)
	require.Equal(f.t, "synthetic primary video", string(got))
	require.NoFileExists(f.t, f.op.NewPath)
	require.Equal(f.t, "done", f.journal().State)
}

type primaryUndoFaultRepo struct {
	database.BatchFileOperationRepositoryInterface
	state    string
	after    bool
	fired    bool
	hook     func(*models.PrimaryUndo)
	onlyHook bool
}

func (r *primaryUndoFaultRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	target := false
	err := r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(op *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, persist, err := fn(op)
		if err == nil && next.PrimaryUndo != nil && next.PrimaryUndo.State == r.state && !r.fired {
			r.fired = true
			target = true
			if r.hook != nil {
				r.hook(next.PrimaryUndo)
			}
			if !r.after && !r.onlyHook {
				return next, false, errors.New("synthetic SQLite journal failure")
			}
		}
		return next, persist, err
	})
	if err == nil && target && r.after && !r.onlyHook {
		return errors.New("synthetic lost commit acknowledgement")
	}
	return err
}

func TestPrimaryUndoCopyFirstPreservesModeAndTime(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	before, err := os.Stat(f.op.NewPath)
	require.NoError(t, err)
	require.NoError(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
	f.success()
	info, e := os.Stat(f.op.OriginalPath)
	require.NoError(t, e)
	require.Equal(t, before.Mode().Perm(), info.Mode().Perm())
	require.Equal(t, int64(1700000000), info.ModTime().Unix())
	f.reopen()
	result, e := NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
	require.NoError(t, e)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcome)
	require.Equal(t, models.RevertStatusReverted, f.op.RevertStatus)
}

func TestPrimaryUndoSQLiteFailuresReopenAndResume(t *testing.T) {
	for _, state := range []string{"planned", "copying", "sealed", "published", "unlinking", "done"} {
		for _, after := range []bool{false, true} {
			name := state + "/before"
			if after {
				name = state + "/after"
			}
			t.Run(name, func(t *testing.T) {
				f := newPrimaryUndoFixture(t)
				bad := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: state, after: after}
				require.Error(t, beginPrimaryUndo(context.Background(), f.fs, bad, f.op))
				require.True(t, bad.fired)
				f.reopen()
				j := f.journal()
				if state == "planned" && !after {
					require.Nil(t, j)
					require.NoError(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
				} else if state == "copying" && !after {
					// Creation happened, but its identity never committed. Equal bytes
					// or the attempt-shaped filename do not authorize adopting it.
					require.ErrorContains(t, f.resume(), "manual recovery")
					require.FileExists(t, f.op.NewPath)
					require.NoFileExists(t, f.op.OriginalPath)
					return
				} else if j.State == "unlinking" {
					require.ErrorContains(t, f.resume(), "manual recovery")
					require.Equal(t, "unlinking", f.journal().State)
					require.FileExists(t, f.op.OriginalPath)
					return
				} else {
					require.NoError(t, f.resume())
				}
				f.success()
			})
		}
	}
}

func TestPrimaryUndoPublishAndRemoveCrashCuts(t *testing.T) {
	for _, cut := range []string{"before-publish", "after-publish", "before-take", "after-take", "before-remove", "after-remove"} {
		t.Run(cut, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			originalPublish, originalRemove := primaryUndoPublish, primaryUndoRemove
			t.Cleanup(func() { primaryUndoPublish = originalPublish; primaryUndoRemove = originalRemove })
			primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
				kind := "publish"
				if strings.Contains(dst, ".undo-remove.") {
					kind = "take"
				}
				if cut == "before-"+kind {
					return errors.New("synthetic process cut")
				}
				err := originalPublish(fs, src, dst)
				if err == nil && cut == "after-"+kind {
					return errors.New("synthetic process cut after rename")
				}
				return err
			}
			primaryUndoRemove = func(fs afero.Fs, p string) error {
				if cut == "before-remove" {
					return errors.New("synthetic process cut")
				}
				err := originalRemove(fs, p)
				if err == nil && cut == "after-remove" {
					return errors.New("synthetic process cut after unlink")
				}
				return err
			}
			require.Error(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
			primaryUndoPublish, primaryUndoRemove = originalPublish, originalRemove
			f.reopen()
			if f.journal().State == "unlinking" {
				require.ErrorContains(t, f.resume(), "manual recovery")
				require.Equal(t, "unlinking", f.journal().State)
				require.FileExists(t, f.op.OriginalPath)
			} else {
				require.NoError(t, f.resume())
				f.success()
			}
		})
	}
}

func TestPrimaryUndoRefusesSameBytesForeignSuccessors(t *testing.T) {
	for _, where := range []string{"source", "stage", "destination", "cleanup"} {
		t.Run(where, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			state := "sealed"
			if where == "destination" {
				state = "published"
			}
			if where == "cleanup" {
				state = "done"
			}
			// A done-before-write failure leaves the removal gone, so stop removal
			// instead when testing a foreign cleanup terminal.
			originalRemove := primaryUndoRemove
			t.Cleanup(func() { primaryUndoRemove = originalRemove })
			var err error
			if where == "cleanup" {
				primaryUndoRemove = func(afero.Fs, string) error { return errors.New("stop at cleanup") }
				err = beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op)
				primaryUndoRemove = originalRemove
			} else {
				bad := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: state, after: true}
				err = beginPrimaryUndo(context.Background(), f.fs, bad, f.op)
			}
			require.Error(t, err)
			j := f.journal()
			path := j.Source
			switch where {
			case "stage":
				path = j.Stage
			case "destination":
				path = j.Destination
			case "cleanup":
				path = j.Cleanup
			}
			originalInfo, e := os.Stat(path)
			require.NoError(t, e)
			saved := path + ".owner"
			require.NoError(t, os.Rename(path, saved))
			require.NoError(t, os.WriteFile(path, []byte("synthetic primary video"), originalInfo.Mode().Perm()))
			require.NoError(t, os.Chtimes(path, originalInfo.ModTime(), originalInfo.ModTime()))
			f.reopen()
			require.Error(t, f.resume())
			got, e := os.ReadFile(path)
			require.NoError(t, e)
			require.Equal(t, "synthetic primary video", string(got))
			require.FileExists(t, saved, "the actual owned object survives")
			if where != "cleanup" {
				require.FileExists(t, j.Source)
			}
		})
	}
}

func TestPrimaryUndoPublishSubstitutionRetainsLibraryAndForeign(t *testing.T) {
	for _, kind := range []string{"stage", "target"} {
		t.Run(kind, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			publish := primaryUndoPublish
			t.Cleanup(func() { primaryUndoPublish = publish })
			var foreign string
			primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
				if !strings.Contains(dst, ".undo-remove.") {
					foreign = dst
					if kind == "stage" {
						require.NoError(t, os.Rename(src, src+".owner"))
						require.NoError(t, os.WriteFile(src, []byte("foreign payload"), 0600))
					} else {
						require.NoError(t, os.WriteFile(dst, []byte("foreign payload"), 0600))
					}
				}
				return publish(fs, src, dst)
			}
			require.Error(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
			primaryUndoPublish = publish
			f.reopen()
			require.Error(t, f.resume())
			got, e := os.ReadFile(foreign)
			require.NoError(t, e)
			require.Equal(t, "foreign payload", string(got))
			require.FileExists(t, f.op.NewPath)
		})
	}
}

func TestPrimaryUndoLegacyOccupiedOriginalDoesNotAdoptBytes(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	require.NoError(t, os.WriteFile(f.op.OriginalPath, []byte("synthetic primary video"), 0600))
	result, err := revertPrimaryFileFS(context.Background(), f.fs, f.repo, f.op, func(string, string) {})
	require.NoError(t, err)
	require.Equal(t, models.RevertReasonDestinationConflict, result.Reason)
	require.Nil(t, f.journal())
	require.FileExists(t, f.op.OriginalPath)
	require.FileExists(t, f.op.NewPath)
}

func TestPrimaryUndoCompletedPublishErrorNeedsSealedIdentity(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	publish := primaryUndoPublish
	t.Cleanup(func() { primaryUndoPublish = publish })
	primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
		err := publish(fs, src, dst)
		if err == nil && !strings.Contains(dst, ".undo-remove.") {
			return fsutil.ErrPublishCompleted
		}
		return err
	}
	require.NoError(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
	f.success()
}

func TestPrimaryUndoNativeCrossVolumeReverter(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	second := os.Getenv("JAVINIZER_TEST_SECOND_VOLUME")
	if second == "" {
		var err error
		second, err = os.Getwd()
		require.NoError(t, err)
	}
	other, err := os.MkdirTemp(second, "primary-undo-native-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(other)) })
	source := filepath.Join(other, "library", "movie.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(source), 0755))
	require.NoError(t, os.WriteFile(source, []byte("synthetic primary video"), 0600))
	a, err := os.Open(source)
	require.NoError(t, err)
	aID, _, err := primaryUndoIdentity(a)
	require.NoError(t, err)
	require.NoError(t, a.Close())
	b, err := os.Open(f.op.NewPath)
	require.NoError(t, err)
	bID, _, err := primaryUndoIdentity(b)
	require.NoError(t, err)
	require.NoError(t, b.Close())
	if aID.Device == bID.Device {
		t.Skip("native cross-volume gate needs JAVINIZER_TEST_SECOND_VOLUME on a different volume")
	}
	t.Logf("native cross-volume source device=%d inode=%d, destination device=%d", aID.Device, aID.Inode, bID.Device)
	require.NoError(t, os.Remove(f.op.NewPath))
	f.op.NewPath = source
	require.NoError(t, f.repo.UpdateNonJournalFields(context.Background(), f.op))
	result, err := NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcome, result.Error)
	f.success()
	journal := f.journal()
	require.Equal(t, aID, journal.SourceIdentity, "durable source proof must match the admitted native object")
	restored, err := os.Open(f.op.OriginalPath)
	require.NoError(t, err)
	restoredID, _, err := primaryUndoIdentity(restored)
	require.NoError(t, err)
	require.NoError(t, restored.Close())
	require.True(t, primaryUndoSameObject(journal.StageIdentity, restoredID), "restored file must be the journal-owned published stage")
	require.Equal(t, bID.Device, restoredID.Device)
	require.Equal(t, aID.Mode, restoredID.Mode)
	t.Logf("native restored device=%d inode=%d; admitted source consumed only after owned publication", restoredID.Device, restoredID.Inode)
}

func TestPrimaryUndoRealSQLiteFaultRetainsSealedSource(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	require.NoError(t, f.db.Exec(`CREATE TRIGGER fail_primary_undo_publish BEFORE UPDATE OF generated_files ON batch_file_operations WHEN NEW.generated_files LIKE '%"state":"published"%' BEGIN SELECT RAISE(ABORT, 'synthetic disk write failure'); END`).Error)
	require.ErrorContains(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op), "synthetic disk write failure")
	require.FileExists(t, f.op.OriginalPath)
	require.FileExists(t, f.op.NewPath)
	require.Equal(t, "sealed", f.journal().State)
	require.NoError(t, f.db.Exec(`DROP TRIGGER fail_primary_undo_publish`).Error)
	f.reopen()
	require.NoError(t, f.resume())
	f.success()
}

func TestPrimaryUndoInterruptedCleanupRetainsTerminal(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	remove := primaryUndoRemove
	t.Cleanup(func() { primaryUndoRemove = remove })
	primaryUndoRemove = func(afero.Fs, string) error { return errors.New("retain first cleanup terminal") }
	require.Error(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
	first := f.journal().Cleanup
	require.FileExists(t, first)
	primaryUndoRemove = remove
	f.reopen()
	require.ErrorContains(t, f.resume(), "manual recovery")
	require.Equal(t, first, f.journal().Cleanup, "a restart must retain the original claim and its ownership")
	require.FileExists(t, first)
	require.FileExists(t, f.op.OriginalPath)
}

func TestPrimaryUndoPostProofDestinationSwapRetainsLibrary(t *testing.T) {
	for _, when := range []string{"journal", "take"} {
		t.Run(when, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			swap := func() {
				require.NoError(t, os.Rename(f.op.OriginalPath, f.op.OriginalPath+".owner"))
				require.NoError(t, os.WriteFile(f.op.OriginalPath, []byte("foreign new destination"), 0600))
			}
			var repo database.BatchFileOperationRepositoryInterface = f.repo
			publish := primaryUndoPublish
			t.Cleanup(func() { primaryUndoPublish = publish })
			if when == "journal" {
				repo = &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "unlinking", after: true, hook: func(*models.PrimaryUndo) { swap() }}
			} else {
				primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
					err := publish(fs, src, dst)
					if err == nil && strings.Contains(dst, ".undo-remove.") {
						swap()
					}
					return err
				}
			}
			require.Error(t, beginPrimaryUndo(context.Background(), f.fs, repo, f.op))
			j := f.journal()
			if when == "journal" {
				require.FileExists(t, j.Source)
			} else {
				require.FileExists(t, j.Cleanup)
			}
			got, e := os.ReadFile(j.Destination)
			require.NoError(t, e)
			require.Equal(t, "foreign new destination", string(got))
			require.FileExists(t, j.Destination+".owner")
		})
	}
}

func TestPrimaryUndoSealedCopyDoesNotAcceptMissingLibraryBeforeCleanup(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	bad := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "published", after: true}
	require.Error(t, beginPrimaryUndo(context.Background(), f.fs, bad, f.op))
	require.NoError(t, os.Rename(f.op.NewPath, f.op.NewPath+".external"))
	f.reopen()
	require.Error(t, f.resume())
	require.FileExists(t, f.op.OriginalPath)
	require.FileExists(t, f.op.NewPath+".external")
	require.Equal(t, "published", f.journal().State)
}

func TestPrimaryUndoDanglingOriginalOccupantRetained(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	if err := os.Symlink("does-not-exist", f.op.OriginalPath); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	require.Error(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
	target, err := os.Readlink(f.op.OriginalPath)
	require.NoError(t, err)
	require.Equal(t, "does-not-exist", target)
	require.Nil(t, f.journal())
	require.FileExists(t, f.op.NewPath)
}

type primaryUndoBlockingFile struct {
	afero.File
	entered chan struct{}
	resume  <-chan struct{}
	once    *sync.Once
}

func (f *primaryUndoBlockingFile) Read(p []byte) (int, error) {
	f.once.Do(func() { close(f.entered); <-f.resume })
	return f.File.Read(p)
}
func (f *primaryUndoBlockingFile) Fd() uintptr { return f.File.(interface{ Fd() uintptr }).Fd() }

func TestPrimaryUndoStreamingDoesNotHoldSQLiteWriter(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	other, err := database.New(&database.Config{Type: "sqlite", DSN: f.path, LogLevel: "silent"})
	require.NoError(t, err)
	defer other.Close()
	otherRepo := database.NewBatchFileOperationRepository(other)
	entered, release := make(chan struct{}), make(chan struct{})
	originalOpen := restoreOpenReplacementSource
	var once sync.Once
	t.Cleanup(func() { restoreOpenReplacementSource = originalOpen })
	restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
		file, err := originalOpen(fs, path)
		if err == nil && path == f.op.NewPath {
			return &primaryUndoBlockingFile{File: file, entered: entered, resume: release, once: &once}, nil
		}
		return file, err
	}
	// Begin opens the source for admission without reading; exactly the first
	// copy read is blocked. Restore the opener after that gate to avoid holding
	// the later content-proof streams as well.
	done := make(chan error, 1)
	go func() { done <- beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op) }()
	<-entered
	// Mutating the hook while the worker is blocked would be a race, so use a
	// one-shot blocking wrapper that only closes its own channel once instead.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = otherRepo.UpdateJournalInTx(ctx, f.op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, e := models.ParseGeneratedFiles(current.GeneratedFiles)
		ledger.Roots = append(ledger.Roots, "unrelated-concurrent-root")
		return ledger, true, e
	})
	close(release)
	require.NoError(t, err, "the streaming copy must not hold BEGIN IMMEDIATE")
	require.NoError(t, <-done)
	f.success()
	ledger, e := models.ParseGeneratedFiles(f.op.GeneratedFiles)
	require.NoError(t, e)
	require.Contains(t, ledger.Roots, "unrelated-concurrent-root")
}

func TestPrimaryUndoPruneFenceAndTerminalStatusPreventAdmission(t *testing.T) {
	for _, kind := range []string{"pruning", "terminal"} {
		t.Run(kind, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			if kind == "pruning" {
				require.NoError(t, f.db.Create(&models.Job{ID: f.op.BatchJobID, Status: models.JobStatus("pruning")}).Error)
			} else {
				require.NoError(t, f.repo.UpdateRevertStatus(context.Background(), f.op.ID, models.RevertStatusReverted))
			}
			require.Error(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
			require.FileExists(t, f.op.NewPath)
			require.NoFileExists(t, f.op.OriginalPath)
			require.Nil(t, f.journal())
		})
	}
}
