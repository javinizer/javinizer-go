package workflow

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-10 (codex follow-up, P1): after wave-9's
// mergeJournalInTx commits, Complete/CompleteFailed used to run a full GORM
// Save carrying the tx-derived journal bytes — any journal append/consume
// committed between the tx commit and that Save was silently erased or
// resurrected. Wave-10 routes the follow-up through UpdateNonJournalFields:
// generated_files is written ONLY by UpdateJournalInTx. These tests pin both
// the routing (recorded call sequence) and the durable race regression on
// sqlite with deterministic step ordering.

// w10RecordingRepo wraps the real sqlite-backed repository, recording the
// repository-method call sequence the revert log drives. Only the methods the
// wave-10 discipline constrains are overridden; the rest pass through the
// embedded interface unchanged.
type w10RecordingRepo struct {
	database.BatchFileOperationRepositoryInterface
	mu    sync.Mutex
	calls []string
	// appendOnNonJournal, when set, runs INSIDE UpdateNonJournalFields BEFORE
	// the delegate write lands — the deterministic stand-in for a third-party
	// journal commit squeezed between the completion's journal-tx commit and
	// its column update.
	appendOnNonJournal     func()
	appendOnNonJournalOnce sync.Once
}

func (w *w10RecordingRepo) note(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, name)
}

func (w *w10RecordingRepo) sequence() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...)
}

func (w *w10RecordingRepo) Update(ctx context.Context, op *models.BatchFileOperation) error {
	w.note("Update")
	return w.BatchFileOperationRepositoryInterface.Update(ctx, op)
}

func (w *w10RecordingRepo) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	w.note("UpdateNonJournalFields")
	if w.appendOnNonJournal != nil {
		w.appendOnNonJournalOnce.Do(w.appendOnNonJournal)
	}
	return w.BatchFileOperationRepositoryInterface.UpdateNonJournalFields(ctx, op)
}

func (w *w10RecordingRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	w.note("UpdateJournalInTx")
	return w.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
}

func (w *w10RecordingRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	w.note("FindByID")
	return w.BatchFileOperationRepositoryInterface.FindByID(ctx, id)
}

// newW10RevertLog builds a dbRevertLog over the real sqlite-backed repository
// wrapped in the recording seam.
func newW10RevertLog(t *testing.T) (RevertLog, *w10RecordingRepo) {
	t.Helper()
	rl, _ := newTestDBRevertLog(t)
	dbLog, ok := rl.(*dbRevertLog)
	require.True(t, ok, "test helper builds the db-backed revert log")
	rec := &w10RecordingRepo{BatchFileOperationRepositoryInterface: dbLog.repo}
	dbLog.repo = rec
	return dbLog, rec
}

func w10OperationJournal(t *testing.T, repo database.BatchFileOperationRepositoryInterface, opID OperationID) models.GeneratedFilesJSON {
	t.Helper()
	recordID, err := strconv.ParseUint(opID, 10, 64)
	require.NoError(t, err)
	row, err := repo.FindByID(context.Background(), uint(recordID))
	require.NoError(t, err)
	require.NotNil(t, row)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return gf
}

// TestW10CompletePreservesForeignAppendBetweenTxAndColumnUpdate is the
// finding's regression (a): the completion's journal tx commits, THEN a
// third-party UpdateJournalInTx append commits, THEN the completion's column
// update lands. Deterministic ordering via the recording seam — under the
// wave-9 full Save the foreign entry was erased by the tx-snapshot rewrite;
// now the scoped update cannot touch generated_files at all.
func TestW10CompletePreservesForeignAppendBetweenTxAndColumnUpdate(t *testing.T) {
	rl, rec := newW10RevertLog(t)
	ctx := context.Background()

	opID, err := rl.Begin(ctx, ApplyCmd{
		Movie:    &models.Movie{ID: "W10-R1", Title: "race"},
		Match:    models.FileMatchInfo{Path: "/src/W10-R1.mp4", MovieID: "W10-R1"},
		Organize: OrganizeOptions{MoveFiles: true},
		DestPath: "/dst/w10r1",
	})
	require.NoError(t, err)
	require.NoError(t, rl.RecordReplacement(ctx, opID, "/dst/w10r1/poster.jpg", "/dst/w10r1/poster.jpg.dlbak.1"))

	// Third-party append armed to land immediately before the completion's
	// column update — i.e. strictly AFTER the completion's journal tx commit.
	rec.appendOnNonJournal = func() {
		recordID, perr := strconv.ParseUint(opID, 10, 64)
		require.NoError(t, perr)
		require.NoError(t, rec.UpdateJournalInTx(ctx, uint(recordID), func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			gf, gerr := models.ParseGeneratedFiles(current.GeneratedFiles)
			if gerr != nil {
				return models.GeneratedFilesJSON{}, false, gerr
			}
			gf.Replacements = append(gf.Replacements, models.ReplacementEntry{
				Destination: "/dst/w10r1/foreign.jpg", Backup: "/dst/w10r1/foreign.jpg.dlbak.f", DestSeq: 7,
			})
			return gf, true, nil
		}))
	}

	require.NoError(t, rl.Complete(ctx, opID, &ApplyResult{
		Movie:          &models.Movie{ID: "W10-R1"},
		NFOPath:        "/dst/w10r1/lib/W10-R1.nfo",
		DownloadPaths:  []string{"/dst/w10r1/lib/W10-R1-poster.jpg"},
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/w10r1/lib/W10-R1.mp4", FolderPath: "/dst/w10r1/lib"},
	}))
	// Capture the completion's repo-call sequence BEFORE the assertions below
	// run their own FindByID lookups.
	seq := rec.sequence()

	gf := w10OperationJournal(t, rec, opID)
	require.Len(t, gf.Replacements, 2, "completion merge + foreign append both persist")
	require.Equal(t, "/dst/w10r1/poster.jpg", gf.Replacements[0].Destination, "downloader arm survives")
	require.Equal(t, "/dst/w10r1/foreign.jpg", gf.Replacements[1].Destination,
		"the foreign append between tx commit and column update survives (wave-9 Save erased it)")
	require.Contains(t, gf.Roots, "/dst/w10r1", "Begin-seeded root survives")
	require.Contains(t, gf.Roots, "/dst/w10r1/lib", "organizer leaf root merged by the tx survives")
	require.Contains(t, gf.Delete, "/dst/w10r1/lib/W10-R1.nfo", "completion payload merged by the tx survives")

	recordID, _ := strconv.ParseUint(opID, 10, 64)
	row, err := rec.FindByID(ctx, uint(recordID))
	require.NoError(t, err)
	require.Equal(t, "/dst/w10r1/lib/W10-R1.mp4", row.NewPath, "completion's non-journal columns still persist")
	require.Equal(t, models.RevertStatusApplied, row.RevertStatus)

	require.NotContains(t, seq, "Update", "completions never full-Save the journal anymore")
	require.Equal(t, []string{"UpdateJournalInTx", "UpdateNonJournalFields", "UpdateJournalInTx"}, seq[len(seq)-3:],
		"completion journal tx → scoped column write (whose hook commits the foreign append); no Save anywhere")
}

// TestW10CompletionFailuresPreserveJournalAndSkipFullSave pins the
// Complete(nil) and CompleteFailed(nil)/result failure surfaces (b): the
// generated_files column is never rewritten outside the journal transaction
// — a failed apply's armed replacement entries stay revertable — and the
// recorded repo-method sequence contains no full Save.
func TestW10CompletionFailuresPreserveJournalAndSkipFullSave(t *testing.T) {
	rl, rec := newW10RevertLog(t)
	ctx := context.Background()

	begin := func(t *testing.T, movieID string) OperationID {
		opID, err := rl.Begin(ctx, ApplyCmd{
			Movie:    &models.Movie{ID: movieID, Title: movieID},
			Match:    models.FileMatchInfo{Path: "/src/" + movieID + ".mp4", MovieID: movieID},
			Organize: OrganizeOptions{MoveFiles: true},
			DestPath: "/dst/" + movieID,
		})
		require.NoError(t, err)
		require.NoError(t, rl.RecordReplacement(ctx, opID, "/dst/"+movieID+"/poster.jpg", "/dst/"+movieID+"/poster.jpg.dlbak.1"))
		return opID
	}
	t.Run("Complete nil result keeps the armed journal", func(t *testing.T) {
		opID := begin(t, "W10-N1")
		before := w10OperationJournal(t, rec, opID)

		require.NoError(t, rl.Complete(ctx, opID, nil))

		after := w10OperationJournal(t, rec, opID)
		require.Equal(t, before, after, "the journal column is untouched by a nil-result completion")
		recordID, _ := strconv.ParseUint(opID, 10, 64)
		row, err := rec.FindByID(ctx, uint(recordID))
		require.NoError(t, err)
		require.Equal(t, models.RevertStatusFailed, row.RevertStatus, "the record is still marked failed")
	})

	t.Run("CompleteFailed nil result keeps the armed journal", func(t *testing.T) {
		opID := begin(t, "W10-N2")
		before := w10OperationJournal(t, rec, opID)

		require.NoError(t, rl.CompleteFailed(ctx, opID, nil))

		after := w10OperationJournal(t, rec, opID)
		require.Equal(t, before, after, "the journal column is untouched by a nil-result failure completion")
		recordID, _ := strconv.ParseUint(opID, 10, 64)
		row, err := rec.FindByID(ctx, uint(recordID))
		require.NoError(t, err)
		require.Equal(t, models.RevertStatusFailed, row.RevertStatus)
	})

	t.Run("CompleteFailed with result merges through the tx only", func(t *testing.T) {
		opID := begin(t, "W10-F1")
		require.NoError(t, rl.CompleteFailed(ctx, opID, &ApplyResult{
			Movie:          &models.Movie{ID: "W10-F1"},
			NFOPath:        "/dst/W10-F1/lib/W10-F1.nfo",
			OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/W10-F1/lib/W10-F1.mp4"},
		}))
		gf := w10OperationJournal(t, rec, opID)
		require.Len(t, gf.Replacements, 1, "armed entry kept revertable across the failure record")
		require.Contains(t, gf.Delete, "/dst/W10-F1/lib/W10-F1.nfo", "partial payload merged by the tx")
		recordID, _ := strconv.ParseUint(opID, 10, 64)
		row, err := rec.FindByID(ctx, uint(recordID))
		require.NoError(t, err)
		require.Equal(t, models.RevertStatusFailed, row.RevertStatus)
		require.Equal(t, "/dst/W10-F1/lib/W10-F1.mp4", row.NewPath)
	})

	require.NotContains(t, rec.sequence(), "Update",
		"no completion route ever calls the full-Save Update (b: recorded method discipline)")
}

// TestW10CaptureSnapshotUsesNonJournalWrite pins the sibling route (b): the
// snapshot write after FindByID must not full-Save the journal column either.
func TestW10CaptureSnapshotUsesNonJournalWrite(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, &RevertLogConfig{AllowRevert: true}, "job-w10", afero.NewMemMapFs(), nil, nil, nil)

	preRecord := &models.BatchFileOperation{
		ID:           3,
		BatchJobID:   "job-w10",
		RevertStatus: models.RevertStatusApplied,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1}},
		}),
	}
	repo.On("FindByID", mock.Anything, uint(3)).Return(preRecord, nil)
	repo.On("UpdateNonJournalFields", mock.Anything, preRecord).Return(nil)

	log.CaptureSnapshot(context.Background(), "3", ApplyCmd{
		Movie: &models.Movie{ID: "W10-SNAP"},
		Match: models.FileMatchInfo{Path: "/src/W10-SNAP.mp4"},
	})

	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything,
		"snapshot enrichment never full-Saves the row (journal belongs to UpdateJournalInTx)")
	repo.AssertCalled(t, "UpdateNonJournalFields", mock.Anything, mock.Anything)
}
