package workflow

// POSTER-WRITE-HARDENING wave-15 (codex follow-up, P1) — completion after a
// concurrent revert: UpdateRevertStatus(Reverted) from another writer commits
// between the completion's FindByID-hydrated snapshot and its
// UpdateNonJournalFields publish. The repository (see the database w15 tests)
// now suppresses the stale status write and reports ErrOperationRowReverted;
// the revert log tolerates it through persistNonJournalColumns: warn through
// the logger seam, reflect the reverted truth on the in-memory record, and
// return nil — the external contract is unchanged except that a reverted row
// is never flipped back to live. Deterministic replay via the repo wrapper
// below (sqlite-backed).

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

// w15RevertingRepo wraps the real sqlite-backed repository: the FIRST
// UpdateNonJournalFields call commits a concurrent revert
// (UpdateRevertStatus(Reverted)) strictly after the caller hydrated its
// snapshot and strictly before the completion's column publish — the
// deterministic stand-in for the wave-15 completion-vs-revert race.
type w15RevertingRepo struct {
	database.BatchFileOperationRepositoryInterface
	once    sync.Once
	flipErr error
}

func (w *w15RevertingRepo) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	w.once.Do(func() {
		w.flipErr = w.BatchFileOperationRepositoryInterface.UpdateRevertStatus(ctx, op.ID, models.RevertStatusReverted)
	})
	if w.flipErr != nil {
		return w.flipErr
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
// the completion's non-journal columns and tx-merged journal still persist.
func TestW15CompleteKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-R1")
	require.NoError(t, rl.RecordReplacement(ctx, opID, "/dst/W15-R1/poster.jpg", "/dst/W15-R1/poster.jpg.dlbak.1"))

	require.NoError(t, rl.Complete(ctx, opID, &ApplyResult{
		Movie:          &models.Movie{ID: "W15-R1"},
		NFOPath:        "/dst/W15-R1/lib/W15-R1.nfo",
		DownloadPaths:  []string{"/dst/W15-R1/lib/W15-R1-poster.jpg"},
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/W15-R1/lib/W15-R1.mp4", FolderPath: "/dst/W15-R1/lib"},
	}), "the lost status race is tolerated, not surfaced as an apply error")

	row := w15Row(t, rec, opID)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the concurrent revert is never clobbered back to live by the completion")
	require.NotNil(t, row.RevertedAt, "the revert stamp survives")
	require.Equal(t, "/dst/W15-R1/lib/W15-R1.mp4", row.NewPath,
		"the completion's non-journal columns still persisted")

	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the tx-merged journal still landed")
	require.Contains(t, gf.Delete, "/dst/W15-R1/lib/W15-R1.nfo")
}

// CompleteFailed with a result keeps the same posture: reverted stays
// authoritative and the partial-failure columns persist.
func TestW15CompleteFailedKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-F1")

	require.NoError(t, rl.CompleteFailed(ctx, opID, &ApplyResult{
		Movie:          &models.Movie{ID: "W15-F1"},
		NFOPath:        "/dst/W15-F1/lib/W15-F1.nfo",
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/W15-F1/lib/W15-F1.mp4"},
	}))

	row := w15Row(t, rec, opID)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"CompleteFailed's Failed mark never flips a concurrently reverted row back")
	require.NotNil(t, row.RevertedAt)
	require.Equal(t, "/dst/W15-F1/lib/W15-F1.mp4", row.NewPath,
		"the partial state still persisted")
}

// Complete with a nil result (the plain failure mark) never resurrects a
// concurrently reverted row as failed.
func TestW15CompleteNilResultKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-N1")

	require.NoError(t, rl.Complete(ctx, opID, nil))

	row := w15Row(t, rec, opID)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the failed mark is suppressed, not clobbering the revert")
	require.NotNil(t, row.RevertedAt)
}

// CompleteFailed with a nil result: same suppression through the same seam.
func TestW15CompleteFailedNilResultKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-N2")

	require.NoError(t, rl.CompleteFailed(ctx, opID, nil))

	row := w15Row(t, rec, opID)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	require.NotNil(t, row.RevertedAt)
}

// CaptureSnapshot tolerates the race too: snapshot columns persist, the
// reverted status stays.
func TestW15CaptureSnapshotKeepsConcurrentRevert(t *testing.T) {
	rl, rec := newW15RevertLog(t)
	ctx := context.Background()
	opID := w15Begin(t, rl, "W15-SNAP")

	rl.CaptureSnapshot(ctx, opID, ApplyCmd{
		Movie: &models.Movie{ID: "W15-SNAP", Title: "W15-SNAP"},
		Match: models.FileMatchInfo{Path: "/src/W15-SNAP.mp4", MovieID: "W15-SNAP"},
	})

	row := w15Row(t, rec, opID)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the snapshot write never resurrects the concurrently reverted row")
	require.NotNil(t, row.RevertedAt)
	// OriginalDirPath is filepath.Dir-derived from the source path, so its
	// separator spelling follows the host OS — compare slash-normalized (the
	// wave-13 w7/r6 alignment pattern) instead of a literal "/src".
	require.Equal(t, filepath.ToSlash("/src"), filepath.ToSlash(row.OriginalDirPath),
		"the snapshot's non-journal columns still persisted")
}
