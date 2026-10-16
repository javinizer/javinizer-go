package workflow

// POSTER-WRITE-HARDENING wave-15 (codex follow-up, P1) — completion after a
// concurrent revert: UpdateRevertStatus(Reverted) from another writer commits
// between the completion's FindByID-hydrated snapshot and its
// authoritative transaction. Completion refuses both journal and narrow columns
// atomically; CaptureSnapshot separately retains its guarded column write.
// Deterministic replay via the sqlite-backed repo wrapper below.

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/require"
)

// w15RevertingRepo commits an independent revert immediately before the selected
// authoritative write: UpdateJournalInTx for completions, or the existing guarded
// UpdateNonJournalFields for CaptureSnapshot. Both see the winning fresh row.
type w15RevertingRepo struct {
	database.BatchFileOperationRepositoryInterface
	once            sync.Once
	flipErr         error
	authoritative   *models.BatchFileOperation
	revertOnJournal bool
}

func (w *w15RevertingRepo) revertBeforeWrite(ctx context.Context, id uint) error {
	w.once.Do(func() {
		w.flipErr = w.BatchFileOperationRepositoryInterface.UpdateRevertStatus(ctx, id, models.RevertStatusReverted)
		if w.flipErr == nil {
			w.authoritative, w.flipErr = w.BatchFileOperationRepositoryInterface.FindByID(ctx, id)
		}
	})
	return w.flipErr
}

func (w *w15RevertingRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if w.revertOnJournal {
		if err := w.revertBeforeWrite(ctx, id); err != nil {
			return err
		}
	}
	return w.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
}

func (w *w15RevertingRepo) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	if !w.revertOnJournal {
		if err := w.revertBeforeWrite(ctx, op.ID); err != nil {
			return err
		}
	}
	return w.BatchFileOperationRepositoryInterface.UpdateNonJournalFields(ctx, op)
}

// newW15RevertLog builds a dbRevertLog over the real sqlite-backed repository
// wrapped in the reverting seam (same shape as the wave-10 recording seam).
func newW15RevertLog(t *testing.T) (*dbRevertLog, *w15RevertingRepo) {
	t.Helper()
	rl, _ := newTestDBRevertLog(t)
	dbLog, ok := rl.(*dbRevertLog)
	require.True(t, ok, "test helper builds the db-backed revert log")
	rec := &w15RevertingRepo{BatchFileOperationRepositoryInterface: dbLog.repo}
	dbLog.repo = rec
	return dbLog, rec
}

func w15Begin(t *testing.T, rl RevertLog, movieID string) OperationID {
	t.Helper()
	opID, err := rl.Begin(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: movieID, Title: movieID},
		Match:    models.FileMatchInfo{Path: "/src/" + movieID + ".mp4", MovieID: movieID},
		Organize: OrganizeOptions{MoveFiles: true},
		DestPath: "/dst/" + movieID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, opID)
	return opID
}

func w15Row(t *testing.T, repo database.BatchFileOperationRepositoryInterface, opID OperationID) *models.BatchFileOperation {
	t.Helper()
	recordID, err := strconv.ParseUint(opID, 10, 64)
	require.NoError(t, err)
	row, err := repo.FindByID(context.Background(), uint(recordID))
	require.NoError(t, err)
	require.NotNil(t, row)
	return row
}

// Complete with a result: the concurrent revert keeps the row reverted while
// the incoming journal and all completion columns are refused together.
func TestW15CompleteKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-R1")
	require.NoError(t, rl.RecordReplacement(ctx, opID, "/dst/W15-R1/poster.jpg", "/dst/W15-R1/poster.jpg.dlbak.1"))

	rec.revertOnJournal = true
	require.ErrorContains(t, rl.Complete(ctx, opID, &ApplyResult{
		Movie:          &models.Movie{ID: "W15-R1"},
		NFOPath:        "/dst/W15-R1/lib/W15-R1.nfo",
		DownloadPaths:  []string{"/dst/W15-R1/lib/W15-R1-poster.jpg"},
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/W15-R1/lib/W15-R1.mp4", FolderPath: "/dst/W15-R1/lib"},
	}), "admitted to Undo or is terminal")

	row := w15Row(t, rec, opID)
	require.NotNil(t, rec.authoritative)
	require.Equal(t, rec.authoritative, row, "refusal preserves every concurrent-revert field and timestamp")
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the concurrent revert is never clobbered back to live by the completion")
	require.NotNil(t, row.RevertedAt, "the revert stamp survives")
	require.Empty(t, row.NewPath, "stale completion columns must not persist")

	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the previously armed journal remains unchanged")
	require.Empty(t, gf.Delete, "refused completion cannot publish incoming inverse work")
}

// CompleteFailed with a result keeps the same posture: reverted stays
// authoritative and all partial-failure columns are refused.
func TestW15CompleteFailedKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-F1")

	rec.revertOnJournal = true
	require.ErrorContains(t, rl.CompleteFailed(ctx, opID, &ApplyResult{
		Movie:          &models.Movie{ID: "W15-F1"},
		NFOPath:        "/dst/W15-F1/lib/W15-F1.nfo",
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/W15-F1/lib/W15-F1.mp4"},
	}), "admitted to Undo or is terminal")

	row := w15Row(t, rec, opID)
	require.NotNil(t, rec.authoritative)
	require.Equal(t, rec.authoritative, row, "refusal preserves every concurrent-revert field and timestamp")
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"CompleteFailed's Failed mark never flips a concurrently reverted row back")
	require.NotNil(t, row.RevertedAt)
	require.Empty(t, row.NewPath, "stale partial-failure columns must not persist")
}

// Complete with a nil result (the plain failure mark) never resurrects a
// concurrently reverted row as failed.
func TestW15CompleteNilResultKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-N1")

	rec.revertOnJournal = true
	require.ErrorContains(t, rl.Complete(ctx, opID, nil), "admitted to Undo or is terminal")

	row := w15Row(t, rec, opID)
	require.NotNil(t, rec.authoritative)
	require.Equal(t, rec.authoritative, row, "refusal preserves every concurrent-revert field and timestamp")
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the failed mark is suppressed, not clobbering the revert")
	require.NotNil(t, row.RevertedAt)
}

// CompleteFailed with a nil result: same suppression through the same seam.
func TestW15CompleteFailedNilResultKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-N2")

	rec.revertOnJournal = true
	require.ErrorContains(t, rl.CompleteFailed(ctx, opID, nil), "admitted to Undo or is terminal")

	row := w15Row(t, rec, opID)
	require.NotNil(t, rec.authoritative)
	require.Equal(t, rec.authoritative, row, "refusal preserves every concurrent-revert field and timestamp")
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	require.NotNil(t, row.RevertedAt)
}

// CaptureSnapshot has no error return, but its stale snapshot columns are
// refused and the complete concurrent-revert row remains unchanged.
func TestW15CaptureSnapshotKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-SNAP")

	rl.CaptureSnapshot(ctx, opID, ApplyCmd{
		Movie: &models.Movie{ID: "W15-SNAP", Title: "W15-SNAP"},
		Match: models.FileMatchInfo{Path: "/src/W15-SNAP.mp4", MovieID: "W15-SNAP"},
	})

	row := w15Row(t, rec, opID)
	require.NotNil(t, rec.authoritative)
	require.Equal(t, rec.authoritative, row, "refusal preserves every concurrent-revert field and timestamp")
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the snapshot write never resurrects the concurrently reverted row")
	require.NotNil(t, row.RevertedAt)
	// OriginalDirPath is filepath.Dir-derived from the source path, so its
	// separator spelling follows the host OS — compare slash-normalized (the
	// wave-13 w7/r6 alignment pattern) instead of a literal "/src".
	require.Equal(t, filepath.ToSlash("/src"), filepath.ToSlash(row.OriginalDirPath),
		"the original source directory remains unchanged")
}
