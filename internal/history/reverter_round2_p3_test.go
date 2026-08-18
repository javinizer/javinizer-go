package history

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P3 R2-1: copy-mode (move_files=false, the default) applies journal
// media overwrites exactly like moves — the revert must replay that journal
// BEFORE the legacy copy-mode rejection, so those users can never end up
// with unrevertable artwork.
func TestRevert_CopyMode_RestoresJournalBeforeModeRejection(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/dst/CPY-001/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	newPath := "/dst/CPY-001/CPY-001.mkv"
	require.NoError(t, fs.MkdirAll("/dst/CPY-001", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, newPath, []byte("copied-video"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("new-poster"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("original-poster"), config.FilePerm))

	raw, err := jsonMarshalLedger(t, dest, backup)
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "CPY-001", OriginalPath: "/src/CPY-001.mkv", NewPath: newPath,
		OperationType: models.OperationTypeCopy, GeneratedFiles: raw,
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Failed, "copy-mode remains non-revertible at the row level")
	require.Contains(t, res.Outcomes[0].Error, "restored first",
		"the rejection names the journal replay")

	require.Equal(t, "original-poster", string(mustRead2(t, fs, dest)),
		"overwritten artwork restored despite the copy-mode rejection")
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "journal consumed — sweeper retains nothing")
}

// codex P3 R2-3: a backup from the pre-journal crash window (set aside,
// never recorded) lives in a directory discoverable via the row's delete
// ledger only — the sweep must find and restore it.
func TestSweep_PreJournalCrashWindow_DiscoverableViaDeleteLedger(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/CRASH/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/CRASH", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("pre-crash"), config.FilePerm))
	backdate(t, fs, backup)
	// No replacement journal anywhere — only a delete ledger naming dest.
	raw, err := json.Marshal(models.GeneratedFilesJSON{Delete: []string{dest}})
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "CRASH-1", OriginalPath: "/src/crash.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "pre-crash", string(mustRead2(t, fs, dest)),
		"pre-journal crash-window backup restored as the last copy")
}

// codex P3 R2-4: a consumption failure must leave the backup IN PLACE so the
// retry repeats an idempotent restore instead of meeting a dangling pointer.
func TestRevert_ConsumptionFailure_KeepsBackupRetryable(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := &failingUpdateRepo{p3OpRepo: newP3OpRepo()}
	ctx := context.Background()

	f := &p3Fixture{fs: fs, repo: repo.p3OpRepo}
	op, dest := f.addAppliedOp(t, "job-1", "CFL-001", false, "new", p3Replacement{seq: 1, backupBytes: "old"})

	sentinel := errors.New("db wedged")
	repo.updateErr = sentinel

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Failed)
	require.Contains(t, res.Outcomes[0].Error, "consumption")

	backup := dest + ".dlbak.a"
	require.Equal(t, "old", string(mustRead2(t, fs, backup)),
		"backup file survives a failed journal consumption")
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "entry still journaled")

	// Retry with the repo healed: idempotent restore completes the revert.
	repo.updateErr = nil
	res, err = r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
}

// failingUpdateRepo fails journal transaction calls until cleared. The
// consumption/marker persistence legs moved from Update to UpdateJournalInTx
// (review 4960250562), so the failure injection rides the transaction seam.
type failingUpdateRepo struct {
	*p3OpRepo
	updateErr error
}

func (m *failingUpdateRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	return m.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

func jsonMarshalLedger(t *testing.T, dest, backup string) (string, error) {
	t.Helper()
	raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1},
	}})
	return string(raw), err
}

func backdate(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(path, old, old))
}

var _ = assert.NoError // keep assert import if unused in patches
