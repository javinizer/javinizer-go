package history

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReviewPrimaryUndoReadOnlyStageRetry(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	require.NoError(t, os.Chmod(f.op.NewPath, 0400))
	bad := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "sealed", after: false}
	require.Error(t, beginPrimaryUndo(context.Background(), f.fs, bad, f.op))
	require.True(t, bad.fired)
	f.reopen()
	require.Equal(t, "copying", f.journal().State)
	require.NoError(t, f.resume(), "a failed sealed commit must be retryable for ordinary read-only media")
	f.success()
}

func TestReviewPrimaryUndoRetentionMustKeepRecoveryJournal(t *testing.T) {
	for _, withHook := range []bool{false, true} {
		t.Run(fmt.Sprint(withHook), func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			old := time.Now().Add(-48 * time.Hour)
			jobs := database.NewJobRepository(f.db)
			require.NoError(t, jobs.Create(context.Background(), &models.Job{ID: f.op.BatchJobID, Status: models.JobStatusOrganized, StartedAt: old, OrganizedAt: &old}))
			j := f.pause("copying")
			if withHook {
				jobs.SetOrganizedJobPruneHook(NewReplacementSweeper(f.fs, f.repo).PruneOperationBackups)
			}
			require.NoError(t, jobs.DeleteOrganizedOlderThan(context.Background(), time.Now().Add(-24*time.Hour)))
			f.reload()
			require.FileExists(t, j.Stage)
			var job models.Job
			require.NoError(t, f.db.Where("id = ?", f.op.BatchJobID).First(&job).Error)
			require.Equal(t, models.JobStatusOrganized, job.Status, "a skipped recovery must not retain the pruning fence")
			f.reopen()
			require.NoError(t, f.resume())
			f.success()
		})
	}
}

func TestReviewPrimaryUndoRestampedSourceChangeAfterHash(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("published")
	info, err := os.Stat(j.Source)
	require.NoError(t, err)
	replacement := []byte("new media bytes!!!!!!!!")
	require.Equal(t, int64(len(replacement)), info.Size())
	repo := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "unlinking", onlyHook: true, hook: func(*models.PrimaryUndo) {
		require.NoError(t, os.WriteFile(j.Source, replacement, info.Mode().Perm()))
		require.NoError(t, os.Chtimes(j.Source, info.ModTime(), info.ModTime()))
	}}
	err = cleanupPrimaryUndo(context.Background(), f.fs, repo, f.op, j)
	require.Error(t, err, "changed source payload during journal round trip must not be consumed on old hash")
	got, err := os.ReadFile(j.Source)
	require.NoError(t, err)
	require.Equal(t, replacement, got)
}

type reviewPausedProducer struct {
	afero.File
	ready, release string
	once           sync.Once
}

func (f *reviewPausedProducer) Fd() uintptr { return f.File.(interface{ Fd() uintptr }).Fd() }
func (f *reviewPausedProducer) Write(p []byte) (int, error) {
	var gateErr error
	f.once.Do(func() {
		if gateErr = os.WriteFile(f.ready, []byte("ready"), 0600); gateErr != nil {
			return
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(f.release); err == nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		gateErr = errors.New("release timeout")
	})
	if gateErr != nil {
		return 0, gateErr
	}
	return f.File.Write(p)
}
func TestReviewPrimaryUndoSeparateProducerHelper(t *testing.T) {
	path := os.Getenv("REVIEW_PRIMARY_UNDO_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	repo := database.NewBatchFileOperationRepository(db)
	op, err := repo.FindByID(context.Background(), 1)
	require.NoError(t, err)
	primaryUndoPrepareStage = func(_ afero.Fs, file afero.File, info os.FileInfo) error {
		if f, ok := file.(*reviewPausedProducer); ok {
			file = f.File
		}
		return fsutil.SyncStagedMetadata(afero.NewOsFs(), file, info)
	}
	fs := &primaryUndoFaultFS{Fs: afero.NewOsFs(), open: func(path string, flags int, file afero.File) (afero.File, error) {
		if strings.HasPrefix(path, op.OriginalPath+".undo.") && flags&os.O_RDWR != 0 {
			return &reviewPausedProducer{File: file, ready: os.Getenv("REVIEW_READY"), release: os.Getenv("REVIEW_RELEASE")}, nil
		}
		return file, nil
	}}
	result, err := NewReverter(fs, repo).revertFile(context.Background(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome, "stale producer must not seal after a newer generation completes")
}
func TestReviewPrimaryUndoSeparateStaleProducer(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("planned")
	ready, release := filepath.Join(t.TempDir(), "ready"), filepath.Join(t.TempDir(), "release")
	cmd := exec.Command(os.Args[0], "-test.run=^TestReviewPrimaryUndoSeparateProducerHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "REVIEW_PRIMARY_UNDO_DB="+f.path, "REVIEW_READY="+ready, "REVIEW_RELEASE="+release)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("producer not ready: %s", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	result, err := NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcome)
	require.NoError(t, os.WriteFile(release, []byte("go"), 0600))
	require.NoError(t, cmd.Wait(), out.String())
	got, err := os.ReadFile(j.Destination)
	require.NoError(t, err)
	require.Equal(t, "synthetic primary video", string(got))
	require.NoFileExists(t, j.Source)
	f.reload()
	require.Equal(t, models.RevertStatusReverted, f.op.RevertStatus, "losing stale producer must not regress completed status")
}

func TestReviewPrimaryUndoDestinationRestampedDuringFinalSourceHash(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("published")
	info, err := os.Stat(j.Destination)
	require.NoError(t, err)
	foreign := []byte("new media bytes!!!!!!!!")
	require.Equal(t, info.Size(), int64(len(foreign)))
	armed, changed := false, false
	repo := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "unlinking", onlyHook: true, hook: func(*models.PrimaryUndo) { armed = true }}
	open := restoreOpenReplacementSource
	t.Cleanup(func() { restoreOpenReplacementSource = open })
	restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
		file, err := open(fs, path)
		if err != nil {
			return nil, err
		}
		if armed && path == j.Source {
			return &primaryUndoFaultFile{File: file, fail: func(op string, _ int) error {
				if op == "read" && !changed {
					changed = true
					require.NoError(t, os.WriteFile(j.Destination, foreign, info.Mode().Perm()))
					require.NoError(t, os.Chtimes(j.Destination, info.ModTime(), info.ModTime()))
				}
				return nil
			}}, nil
		}
		return file, nil
	}
	require.Error(t, cleanupPrimaryUndo(context.Background(), f.fs, repo, f.op, j))
	require.True(t, changed)
	got, err := os.ReadFile(j.Destination)
	require.NoError(t, err)
	require.Equal(t, foreign, got)
	got, err = os.ReadFile(j.Cleanup)
	require.NoError(t, err)
	require.Equal(t, "synthetic primary video", string(got))
	require.Equal(t, "unlinking", f.journal().State)
}

func TestReviewPrimaryUndoCleanupClaimProcessHelper(t *testing.T) {
	path := os.Getenv("PRIMARY_UNDO_CLAIM_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	repo := database.NewBatchFileOperationRepository(db)
	op, err := repo.FindByID(context.Background(), 1)
	require.NoError(t, err)
	publish := primaryUndoPublish
	primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
		if src == op.NewPath {
			if err := os.WriteFile(os.Getenv("PRIMARY_UNDO_CLAIM_READY"), []byte("ready"), 0600); err != nil {
				return err
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(os.Getenv("PRIMARY_UNDO_CLAIM_RELEASE")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					return errors.New("claim release timeout")
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		return publish(fs, src, dst)
	}
	primaryUndoRemove = func(afero.Fs, string) error { return errors.New("synthetic process interruption before claim removal") }
	result, err := NewReverter(afero.NewOsFs(), repo).revertFile(context.Background(), op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
}

func TestReviewPrimaryUndoSeparateCleanupClaimRetainsEndpoints(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	f.pause("published")
	ready, release := filepath.Join(t.TempDir(), "ready"), filepath.Join(t.TempDir(), "release")
	cmd := exec.Command(os.Args[0], "-test.run=^TestReviewPrimaryUndoCleanupClaimProcessHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "PRIMARY_UNDO_CLAIM_DB="+f.path, "PRIMARY_UNDO_CLAIM_READY="+ready, "PRIMARY_UNDO_CLAIM_RELEASE="+release)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("claim producer not ready: %s", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	j := f.journal()
	require.Equal(t, "unlinking", j.State)
	before := f.op.GeneratedFiles
	result, err := NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	require.Contains(t, result.Error, "manual recovery")
	f.reload()
	require.Equal(t, before, f.op.GeneratedFiles, "a resumer must not supersede the delayed process's claim")
	require.FileExists(t, j.Source)
	require.NoError(t, os.WriteFile(release, []byte("go"), 0600))
	require.NoError(t, cmd.Wait(), out.String())
	got, err := os.ReadFile(j.Cleanup)
	require.NoError(t, err)
	require.Equal(t, "synthetic primary video", string(got))
	got, err = os.ReadFile(j.Destination)
	require.NoError(t, err)
	require.Equal(t, "synthetic primary video", string(got))
	require.NoFileExists(t, j.Source)
	f.reopen()
	require.ErrorContains(t, f.resume(), "manual recovery")
	require.Equal(t, before, f.op.GeneratedFiles)
	got, err = os.ReadFile(j.Cleanup)
	require.NoError(t, err)
	require.Equal(t, "synthetic primary video", string(got))
}

func TestReviewPrimaryUndoPruneClaimWinsAdmission(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	old := time.Now().Add(-48 * time.Hour)
	jobs := database.NewJobRepository(f.db)
	require.NoError(t, jobs.Create(context.Background(), &models.Job{ID: f.op.BatchJobID, Status: models.JobStatusOrganized, StartedAt: old, OrganizedAt: &old}))
	claimed, release := make(chan struct{}), make(chan struct{})
	jobs.SetOrganizedJobPruneHook(func(context.Context, []models.BatchFileOperation) error { close(claimed); <-release; return nil })
	done := make(chan error, 1)
	go func() { done <- jobs.DeleteOrganizedOlderThan(context.Background(), time.Now().Add(-24*time.Hour)) }()
	<-claimed
	err := beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op)
	require.Error(t, err)
	require.FileExists(t, f.op.NewPath)
	require.NoFileExists(t, f.op.OriginalPath)
	stages, err := filepath.Glob(f.op.OriginalPath + ".undo.*")
	require.NoError(t, err)
	require.Empty(t, stages, "a failed admission must not create a stage")
	close(release)
	require.NoError(t, <-done)
	require.FileExists(t, f.op.NewPath)
}

func TestReviewPrimaryUndoUnmetJournalDurabilityRetainsSource(t *testing.T) {
	for _, admitted := range []bool{false, true} {
		t.Run(fmt.Sprint(admitted), func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			if admitted {
				f.pause("published")
			}
			sqlDB, err := f.db.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, f.db.Exec("PRAGMA journal_mode=DELETE").Error)
			if admitted {
				err = f.resume()
			} else {
				err = beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op)
			}
			require.ErrorContains(t, err, "requires SQLite WAL")
			got, err := os.ReadFile(f.op.NewPath)
			require.NoError(t, err)
			require.Equal(t, "synthetic primary video", string(got))
			if admitted {
				require.Equal(t, "published", f.journal().State)
				require.FileExists(t, f.op.OriginalPath)
			} else {
				require.Nil(t, f.journal())
				require.NoFileExists(t, f.op.OriginalPath)
			}
		})
	}
}
