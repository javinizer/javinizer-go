package history

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReverterACDurabilityCovW27_MissingDestinationMarkerPersistFailureUndoesRestore(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup := newW27ArmedReplacement(t, base, "W27-UNDO")
	// This helper deliberately leaves dest absent: W27 covers the R9-2
	// crash-window compensation branch, not an installed destination.
	removeErr := errors.New("backup remove wedged")
	fs := &w27RemoveFailFs{Fs: base, backup: backup, backupErr: removeErr, failBackup: true}
	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	markerErr := errors.New("marker update transient")
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: markerErr}

	restored, err := NewReverter(fs, failingRepo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, removeErr)
	require.True(t, restored[dest])
	_, err = base.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist, "marker persistence failure must undo the restore")
	require.Equal(t, "old", string(mustRead2(t, base, backup)))

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the failed cleanup must leave the entry armed")
	require.False(t, gf.Replacements[0].Installed)
	require.False(t, gf.Replacements[0].RestorePending)
}

func TestReverterACDurabilityCovW27_MissingDestinationUndoFailureUsesCompoundWarning(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup := newW27ArmedReplacement(t, base, "W27-UNDO-FAIL")
	// Keep the pre-restore destination missing so the injected undo failure
	// exercises the legitimate R9-2 remove path.
	removeErr := errors.New("backup remove wedged")
	undoErr := errors.New("restore undo wedged")
	fs := &w27RemoveFailFs{
		Fs:         base,
		backup:     backup,
		backupErr:  removeErr,
		dest:       dest,
		destErr:    undoErr,
		failBackup: true,
		failDest:   true,
	}
	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	markerErr := errors.New("marker update transient")
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: markerErr}

	var logs bytes.Buffer
	restoreLogOutput := logging.SetOutput(&logs)
	defer restoreLogOutput()

	_, err := NewReverter(fs, failingRepo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, removeErr)
	require.Equal(t, "old", string(mustRead2(t, base, dest)), "a failed undo leaves the restored bytes in place")
	require.Equal(t, "old", string(mustRead2(t, base, backup)))

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
	require.False(t, gf.Replacements[0].RestorePending, "the failed marker update remains best-effort armed")
	absoluteBackup, err := filepath.Abs(backup)
	require.NoError(t, err)
	requireLogPathContains(t, logs.String(), absoluteBackup)
	require.Contains(t, logs.String(), "AND restore-undo failed")
	require.Contains(t, logs.String(), undoErr.Error())
}

func TestReverterACDurabilityCovW27_MissingDestinationFreshSweeperRetriesAfterExplicitUndo(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup := newW27ArmedReplacement(t, base, "W27-RESTART")
	fs := &w27RemoveFailFs{
		Fs:         base,
		backup:     backup,
		backupErr:  errors.New("backup remove wedged"),
		failBackup: true,
	}
	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: errors.New("marker update transient")}

	_, err := NewReverter(fs, failingRepo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	_, err = base.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, "old", string(mustRead2(t, base, backup)))

	// A new sweeper has no process-local pending-removal fallback. It must be
	// able to recover from the durable armed-row + backup + absent-destination
	// state left by the explicit reverter.
	fs.failBackup = false
	failingRepo.updateErr = nil
	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func newW27ArmedReplacement(t *testing.T, fs afero.Fs, suffix string) (string, string) {
	t.Helper()
	dest := "/out/" + suffix + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	return dest, backup
}

func w27CreateArmedReplacementRow(t *testing.T, repo *p3OpRepo, dest, backup string) *models.BatchFileOperation {
	t.Helper()
	op := &models.BatchFileOperation{
		BatchJobID:    "job-w27",
		MovieID:       "W27",
		OriginalPath:  "/src/W27.mkv",
		NewPath:       "/out/W27/W27.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: dest,
				Backup:      backup,
				DestSeq:     1,
			}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op
}

type w27RemoveFailFs struct {
	afero.Fs
	backup     string
	backupErr  error
	failBackup bool
	dest       string
	destErr    error
	failDest   bool
}

func (f *w27RemoveFailFs) Remove(name string) error {
	clean := filepath.Clean(filepath.FromSlash(name))
	backupClean := filepath.Clean(filepath.FromSlash(f.backup))
	// Wave-26: the removal gate unlinks the QUARANTINE name
	// (backup + ".dlq." + token) instead of the journaled pathname, so the
	// wedge matches both spellings — the quarantined verified object moves
	// back to the journaled name by compensation, which keeps every
	// assertion below (backup bytes preserved at the journaled name, armed
	// entry, clean retry) true for the quarantine flow.
	if f.failBackup && (clean == backupClean || strings.HasPrefix(clean, backupClean+".dlq.")) {
		return f.backupErr
	}
	// Wave-35: the destination undo unlink quarantines first, so the unlinked
	// spelling is the quarantine sibling (dest + ".dlq." + token); wedge both
	// spellings. The failed-unlink compensation moves the verified object
	// back onto dest no-replace, so the test's byte-retention assertions hold.
	if f.failDest && (clean == filepath.Clean(filepath.FromSlash(f.dest)) ||
		strings.HasPrefix(clean, filepath.Clean(filepath.FromSlash(f.dest))+backupQuarantineSuffix)) {
		return f.destErr
	}
	return f.Fs.Remove(name)
}
