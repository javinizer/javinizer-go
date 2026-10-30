package history_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Abort a real journal transaction at the attempted consumption, before any
// durable write. All reads, admission, and other journal mutations delegate.
type consumptionFailureRepository struct {
	database.BatchFileOperationRepositoryInterface
	backup   string
	fault    error
	failures int
}

func (r *consumptionFailureRepository) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, changed, err := fn(current)
		if err != nil || !changed || r.fault == nil {
			return next, changed, err
		}
		before, parseErr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if parseErr != nil {
			return next, changed, parseErr
		}
		had, kept := false, false
		for _, entry := range before.Replacements {
			had = had || entry.Backup == r.backup
		}
		for _, entry := range next.Replacements {
			kept = kept || entry.Backup == r.backup
		}
		if had && !kept {
			r.failures++
			return next, false, r.fault
		}
		return next, changed, nil
	})
}

// A permission failure can prevent moving the restored destination aside even
// after the backup has been re-armed. The failed operation mutates no pathname.
type undoQuarantineFailureFS struct {
	failedRename [2]string
	*afero.OsFs
	destination string
	deny        bool
	failures    int
}

func (f *undoQuarantineFailureFS) Rename(oldname, newname string) error {
	if f.deny && filepath.Clean(oldname) == f.destination {
		f.failures++
		f.failedRename = [2]string{oldname, newname}
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: os.ErrPermission}
	}
	return f.OsFs.Rename(oldname, newname)
}

func TestSweepCompensationFailureRetainsBytesUntilExplicitUndo(t *testing.T) {
	ctx := t.Context()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "history.sqlite"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repository := database.NewBatchFileOperationRepository(db)
	base := &afero.OsFs{}
	root := filepath.Join(t.TempDir(), "owned-compensation")
	primary := filepath.Join(root, "movie.mp4")
	destination := filepath.Join(root, "poster.jpg")
	backup := destination + ".dlbak.0123456789abcdef"
	require.NoError(t, base.MkdirAll(root, 0700))
	require.NoError(t, afero.WriteFile(base, primary, []byte("original primary bytes"), 0600))
	oldBytes := []byte("previous poster bytes")
	require.NoError(t, afero.WriteFile(base, backup, oldBytes, 0600))
	stamp := time.Unix(1700000000, 0)
	require.NoError(t, base.Chtimes(backup, stamp, stamp))
	backupInfo, err := base.Stat(backup)
	require.NoError(t, err)
	sum := sha256.Sum256(oldBytes)
	entry := models.ReplacementEntry{Destination: destination, Backup: backup, DestSeq: 1, BackupSize: backupInfo.Size(), BackupModUnix: backupInfo.ModTime().Unix(), BackupSHA256: hex.EncodeToString(sum[:])}
	retain := true
	// Empty NewPath and Installed=false model Begin plus RecordReplacement,
	// interrupted before installing the replacement or completing the operation.
	op := &models.BatchFileOperation{BatchJobID: "compensation-contract", MovieID: "SYNTH-001", OriginalPath: primary, OriginalDirPath: root, OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{root}, UndoRetention: &retain, Replacements: []models.ReplacementEntry{entry}})}
	require.NoError(t, repository.Create(ctx, op))
	transactionFault := errors.New("journal storage write unavailable")
	repo := &consumptionFailureRepository{BatchFileOperationRepositoryInterface: repository, backup: backup, fault: transactionFault}
	fs := &undoQuarantineFailureFS{OsFs: base, destination: destination, deny: true}
	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	healed, err := history.NewReplacementSweeper(fs, repo).Sweep(ctx)
	t.Logf("sweep diagnostics: %s", logs.String())
	require.NoError(t, err, "individual recovery failure is reported by no heal and diagnostic")
	require.Zero(t, healed)
	require.Equal(t, 1, repo.failures, "the real transaction attempted journal consumption")
	require.Equal(t, 1, fs.failures, "restore undo reached the filesystem rename boundary")
	require.Equal(t, destination, fs.failedRename[0])
	require.Contains(t, filepath.Base(fs.failedRename[1]), "poster.jpg.dlq.")
	require.Contains(t, logs.String(), "consumption failed AND restore-undo failed")
	primaryAfterFailure, err := afero.ReadFile(base, primary)
	require.NoError(t, err)
	require.Equal(t, "original primary bytes", string(primaryAfterFailure))
	names, err := afero.ReadDir(base, root)
	require.NoError(t, err)
	actualNames := make([]string, 0, len(names))
	for _, name := range names {
		actualNames = append(actualNames, name.Name())
	}
	require.ElementsMatch(t, []string{filepath.Base(primary), filepath.Base(destination), filepath.Base(backup)}, actualNames)
	for _, path := range []string{destination, backup} {
		got, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, oldBytes, got, path)
	}
	row, err := repository.FindByID(ctx, op.ID)
	require.NoError(t, err)
	ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Equal(t, []models.ReplacementEntry{entry}, ledger.Replacements)
	require.Equal(t, models.RevertStatusApplied, row.RevertStatus)

	// A fresh sweep has no authority to discard either copy merely because both
	// now exist. Removing the faults must not turn this into an automatic heal.
	repo.fault = nil
	fs.deny = false
	healed, err = history.NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Zero(t, healed)
	for _, path := range []string{destination, backup} {
		got, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, oldBytes, got)
	}

	retained, err := repository.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, row.GeneratedFiles, retained.GeneratedFiles)
	require.Equal(t, row.RevertStatus, retained.RevertStatus)

	// Explicit Undo owns journal replay and can complete recovery normally.
	result, err := history.NewReverter(fs, repository).RevertBatch(ctx, op.BatchJobID)
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded)
	require.Zero(t, result.Failed)
	row, err = repository.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	ledger, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, ledger.Replacements)
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	got, err := afero.ReadFile(base, destination)
	require.NoError(t, err)
	require.Equal(t, oldBytes, got)
	got, err = afero.ReadFile(base, primary)
	require.NoError(t, err)
	require.Equal(t, "original primary bytes", string(got))
}
