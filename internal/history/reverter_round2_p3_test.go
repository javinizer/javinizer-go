package history

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
// media overwrites exactly like moves — the revert replays that journal
// BEFORE any operation-type leg, so those users can never end up with
// unrevertable artwork. codex P2 (PR #241 F2): the copy-mode rejection that
// used to follow is gone — the revert continues into the non-destructive
// cleanup leg, consuming the journal's generated-file Delete entries
// (copied subtitles here), keeping the copied primary, and marking the row
// reverted.
func TestRevert_CopyMode_RestoresJournalThenCleansGeneratedArtifacts(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/dst/CPY-001/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	newPath := "/dst/CPY-001/CPY-001.mkv"
	copiedSub := "/dst/CPY-001/CPY-001.srt"
	require.NoError(t, fs.MkdirAll("/dst/CPY-001", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, newPath, []byte("copied-video"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, copiedSub, []byte("copied-subtitle"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("new-poster"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("original-poster"), config.FilePerm))

	raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, DestSeq: 1}},
		Delete:       []string{copiedSub},
	})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "CPY-001", OriginalPath: "/src/CPY-001.mkv", NewPath: newPath,
		OperationType: models.OperationTypeCopy, GeneratedFiles: raw,
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded, "copy-mode ops revert via the non-destructive cleanup leg (PR #241 F2)")
	require.Equal(t, models.RevertOutcomeReverted, res.Outcomes[0].Outcome)

	require.Equal(t, "original-poster", string(mustRead2(t, fs, dest)),
		"overwritten artwork restored before the cleanup leg")
	_, statErr := fs.Stat(copiedSub)
	require.True(t, os.IsNotExist(statErr), "the journaled copied-subtitle Delete entry is consumed")
	require.Equal(t, "copied-video", string(mustRead2(t, fs, newPath)),
		"the copied primary is retained — copy-mode revert never moves back or unlinks it")
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
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

func backdate(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(path, old, old))
}

var _ = assert.NoError // keep assert import if unused in patches
