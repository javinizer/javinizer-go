package workflow

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-9 (codex review 4960250562 follow-up, P1):
// Complete and CompleteFailed must merge the apply outcome's generated-files
// payload against the row re-read INSIDE the journal transaction — never
// against the stale preRecord snapshot — so a concurrent journal mutation
// (append/consume in another process) is neither resurrected nor erased by
// the completion's full Save. Wave-10 (same review): the completion's
// follow-up column write no longer persists generated_files AT ALL
// (UpdateNonJournalFields) — UpdateJournalInTx owns the journal column — so
// a mutation committed after the journal tx survives the follow-up.

// runFnMock installs an UpdateJournalInTx expectation that executes fn
// against current and returns fn's error, mirroring the repository contract.
func runFnMock(repo *mocks.MockBatchFileOperationRepositoryInterface, id uint, current *models.BatchFileOperation, captured *string) {
	repo.On("UpdateJournalInTx", mock.Anything, id, mock.Anything).
		Return(func(_ context.Context, _ uint, fn database.JournalUpdateFn) error {
			next, persist, err := fn(current)
			if err == nil && persist && captured != nil {
				*captured = models.MarshalLedgerJSON(next)
			}
			return err
		})
}

// TestW9CompleteMergesAgainstFreshInTxRow: the row's journal CHANGES between
// Complete's preRecord read and its journal transaction (a foreign process
// appended a destination-B entry). The final journal must keep BOTH the
// concurrent entry and Complete's own contribution, and the follow-up
// non-journal column update carries the in-memory (tx-derived) journal bytes
// — but as of wave-10 it never WRITES them (UpdateJournalInTx owns that
// column), so nothing committed after the tx can be clobbered.
func TestW9CompleteMergesAgainstFreshInTxRow(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, nil, "job-w9", nil, nil, nil, nil)

	staleJournal := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1}},
	})
	// The row the transaction re-reads: a foreign append(+confirm) landed
	// after the preRecord snapshot was taken.
	freshJournal := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{
			{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1, Installed: true},
			{Destination: "/dst/b.jpg", Backup: "/dst/b.jpg.dlbak.2", DestSeq: 2},
		},
	})

	var txPersisted string
	var saved models.BatchFileOperation
	repo.On("FindByID", mock.Anything, uint(7)).Return(&models.BatchFileOperation{
		ID: 7, BatchJobID: "job-w9", MovieID: "W9-001",
		RevertStatus: models.RevertStatusApplied, GeneratedFiles: staleJournal,
	}, nil)
	runFnMock(repo, 7, &models.BatchFileOperation{ID: 7, GeneratedFiles: freshJournal, RevertStatus: models.RevertStatusApplied}, &txPersisted)
	repo.On("UpdateNonJournalFields", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).
		Run(func(args mock.Arguments) { saved = *args.Get(1).(*models.BatchFileOperation) }).Return(nil)

	err := log.Complete(context.Background(), "7", &ApplyResult{
		Movie:          &models.Movie{ID: "W9-001"},
		NFOPath:        "/dst/w9.nfo",
		DownloadPaths:  []string{"/dst/poster.jpg"},
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/w9.mp4", FolderPath: "/dst/lib"},
	})
	require.NoError(t, err)

	require.NotEmpty(t, txPersisted, "the journal transaction persisted the merge")
	gf, perr := models.ParseGeneratedFiles(txPersisted)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 2, "concurrent entry AND the pre-existing entry both survive")
	require.True(t, gf.Replacements[0].Installed, "the foreign confirm is preserved")
	require.Contains(t, gf.Delete, "/dst/w9.nfo", "Complete's own delete payload merges in")
	require.Contains(t, gf.Delete, "/dst/poster.jpg")
	require.Contains(t, gf.Roots, "/dst/lib", "R4-2 organizer leaf folder seeds a root")

	require.Equal(t, txPersisted, saved.GeneratedFiles,
		"the column update's in-memory record carries the tx-derived journal, never the stale snapshot")
	require.Equal(t, "/dst/w9.mp4", saved.NewPath)
	require.Equal(t, models.RevertStatusApplied, saved.RevertStatus)
	require.NotEqual(t, staleJournal, saved.GeneratedFiles, "the stale snapshot must not win")
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestW9CompleteFailedMergesAgainstFreshInTxRow is the CompleteFailed twin: a
// foreign consumption between preRecord and the transaction must not be
// resurrected, and the record is still marked failed with its journal intact.
func TestW9CompleteFailedMergesAgainstFreshInTxRow(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, nil, "job-w9", nil, nil, nil, nil)

	staleJournal := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{
			{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1},
			{Destination: "/dst/consumed.jpg", Backup: "/dst/consumed.jpg.dlbak.9", DestSeq: 9},
		},
	})
	// The fresh row: the consumed entry is already gone, a foreign root seeded.
	freshJournal := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1}},
		Roots:        []string{"/dst/foreign-root"},
	})

	var txPersisted string
	var saved models.BatchFileOperation
	repo.On("FindByID", mock.Anything, uint(8)).Return(&models.BatchFileOperation{
		ID: 8, BatchJobID: "job-w9", MovieID: "W9-002",
		RevertStatus: models.RevertStatusApplied, GeneratedFiles: staleJournal,
	}, nil)
	runFnMock(repo, 8, &models.BatchFileOperation{ID: 8, GeneratedFiles: freshJournal, RevertStatus: models.RevertStatusApplied}, &txPersisted)
	repo.On("UpdateNonJournalFields", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).
		Run(func(args mock.Arguments) { saved = *args.Get(1).(*models.BatchFileOperation) }).Return(nil)

	err := log.CompleteFailed(context.Background(), "8", &ApplyResult{
		Movie:          &models.Movie{ID: "W9-002"},
		NFOPath:        "/dst/w9b.nfo",
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/w9b.mp4"},
	})
	require.NoError(t, err)

	gf, perr := models.ParseGeneratedFiles(txPersisted)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 1, "the foreign consumption is NOT resurrected")
	require.Equal(t, "/dst/a.jpg", gf.Replacements[0].Destination)
	require.Contains(t, gf.Roots, "/dst/foreign-root", "the foreign seeded root survives")
	require.Contains(t, gf.Delete, "/dst/w9b.nfo")

	require.Equal(t, txPersisted, saved.GeneratedFiles)
	require.Equal(t, models.RevertStatusFailed, saved.RevertStatus, "CompleteFailed still marks the record failed")
	require.Equal(t, "/dst/w9b.mp4", saved.NewPath, "partial state remains persisted")
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestW9CompleteJournalTxNotFound: the row vanished between the preRecord
// read and the journal transaction → Complete surfaces the not-found leg
// instead of resurrecting the row through a full Save upsert.
func TestW9CompleteJournalTxNotFound(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, nil, "job-w9", nil, nil, nil, nil)

	repo.On("FindByID", mock.Anything, uint(9)).Return(&models.BatchFileOperation{ID: 9, RevertStatus: models.RevertStatusApplied}, nil)
	repo.On("UpdateJournalInTx", mock.Anything, uint(9), mock.Anything).
		Return(fmt.Errorf("journal tx: %w", database.ErrNotFound))

	err := log.Complete(context.Background(), "9", &ApplyResult{Movie: &models.Movie{ID: "W9-003"}, NFOPath: "/dst/x.nfo"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "record 9 not found")
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestW9CompleteFailedJournalTxError: a journal-transaction failure surfaces
// with the caller name and blocks the non-journal Save.
func TestW9CompleteFailedJournalTxError(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, nil, "job-w9", nil, nil, nil, nil)

	repo.On("FindByID", mock.Anything, uint(10)).Return(&models.BatchFileOperation{ID: 10, RevertStatus: models.RevertStatusApplied}, nil)
	repo.On("UpdateJournalInTx", mock.Anything, uint(10), mock.Anything).Return(fmt.Errorf("disk I/O error"))

	err := log.CompleteFailed(context.Background(), "10", &ApplyResult{Movie: &models.Movie{ID: "W9-004"}, NFOPath: "/dst/y.nfo"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "revert log CompleteFailed: persist journal for record 10")
	require.Contains(t, err.Error(), "disk I/O error")
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestW9CompletionLedgerMerge pins the merge helper: idempotent no-op when the
// merged bytes equal the current journal, folder-root append/dedupe, fresh-row
// replacement carriage, and the defensive unparseable-blob refusal.
func TestW9CompletionLedgerMerge(t *testing.T) {
	journal := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1}},
		Roots:        []string{"/dst/root"},
	})

	// No new payload, no folder root → byte-identical merge is a persist=false
	// no-op (the retry-after-commit leg).
	next, persist, merged, err := completionLedgerMerge(journal, "", "")
	require.NoError(t, err)
	require.False(t, persist)
	require.Equal(t, journal, merged)
	require.Empty(t, next.Replacements)

	// Folder root appends against the FRESH row's journal and dedupes.
	next, persist, merged, err = completionLedgerMerge(journal, "", "/dst/leaf")
	require.NoError(t, err)
	require.True(t, persist)
	require.Contains(t, merged, "/dst/leaf")
	require.Len(t, next.Roots, 2)
	require.Len(t, next.Replacements, 1, "fresh-row replacements carry through")
	_, persist, _, err = completionLedgerMerge(merged, "", "/dst/leaf")
	require.NoError(t, err)
	require.False(t, persist, "re-appending an existing root is a no-op")

	// Defensive: a merged blob that reparses to garbage refuses the
	// transaction instead of persisting unverifiable bytes (unreachable through
	// mergeReplacementLedger's byte contract — prior malformed degrades to
	// newRaw, so injecting a malformed newRaw drives the refusal leg directly).
	_, persist, merged, err = completionLedgerMerge(`{"replacements":broken`, `{"delete":broken`, "")
	require.Error(t, err)
	require.False(t, persist)
	require.Empty(t, merged)
}

// TestW9ConcurrentCompletesNoLostEntries is the sqlite-backed regression (b):
// two goroutines complete the SAME operation row concurrently. The serialized
// journal transaction must keep every journaled replacement and every seeded
// root, and the row must end in a coherent post-completion state.
func TestW9ConcurrentCompletesNoLostEntries(t *testing.T) {
	rl, db := newTestDBRevertLog(t)
	ctx := context.Background()

	opID, err := rl.Begin(ctx, ApplyCmd{
		Movie:    &models.Movie{ID: "W9-RACE", Title: "race"},
		Match:    models.FileMatchInfo{Path: "/src/W9-RACE.mp4", MovieID: "W9-RACE"},
		Organize: OrganizeOptions{MoveFiles: true},
		DestPath: "/dst/seeded-root",
	})
	require.NoError(t, err)

	// Arm the downloader journal entry completions must never clobber.
	require.NoError(t, rl.RecordReplacement(ctx, opID, "/dst/w9race/poster.jpg", "/dst/w9race/poster.jpg.dlbak.1"))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, side := range []string{"a", "b"} {
		wg.Add(1)
		go func(side string) {
			defer wg.Done()
			errs <- rl.Complete(ctx, opID, &ApplyResult{
				Movie:          &models.Movie{ID: "W9-RACE"},
				NFOPath:        "/dst/w9race/" + side + ".nfo",
				DownloadPaths:  []string{"/dst/w9race/" + side + "-poster.jpg"},
				OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/w9race/W9-RACE.mp4", FolderPath: "/dst/leaf-" + side},
			})
		}(side)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	recordID, err := strconv.ParseUint(opID, 10, 64)
	require.NoError(t, err)
	var record models.BatchFileOperation
	require.NoError(t, db.Where("id = ?", uint(recordID)).First(&record).Error)

	require.Equal(t, models.RevertStatusApplied, record.RevertStatus, "concurrent completions leave a coherent applied status")
	require.Equal(t, "/dst/w9race/W9-RACE.mp4", record.NewPath)

	gf, err := models.ParseGeneratedFiles(record.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the armed replacement entry survives both completions")
	require.Equal(t, "/dst/w9race/poster.jpg", gf.Replacements[0].Destination)
	require.Contains(t, gf.Roots, "/dst/seeded-root", "the Begin-time discovery root survives")
	require.Contains(t, gf.Roots, "/dst/leaf-a", "completion A's organizer root is not lost")
	require.Contains(t, gf.Roots, "/dst/leaf-b", "completion B's organizer root is not lost")
	require.NotEmpty(t, gf.Delete, "one completion's delete payload wins by merge semantics")
}

// TestW9CompleteThenCompleteFailedKeepsFailedStatus: a later failure records
// over the completed row without losing its journal; the status flips to
// failed exactly once and stays there.
func TestW9CompleteThenCompleteFailedKeepsFailedStatus(t *testing.T) {
	rl, db := newTestDBRevertLog(t)
	ctx := context.Background()

	opID, err := rl.Begin(ctx, ApplyCmd{
		Movie:    &models.Movie{ID: "W9-FLIP", Title: "flip"},
		Match:    models.FileMatchInfo{Path: "/src/W9-FLIP.mp4", MovieID: "W9-FLIP"},
		Organize: OrganizeOptions{MoveFiles: true},
		DestPath: "/dst/flip-root",
	})
	require.NoError(t, err)
	require.NoError(t, rl.RecordReplacement(ctx, opID, "/dst/flip/poster.jpg", "/dst/flip/poster.jpg.dlbak.1"))

	require.NoError(t, rl.Complete(ctx, opID, &ApplyResult{
		Movie:          &models.Movie{ID: "W9-FLIP"},
		DownloadPaths:  []string{"/dst/flip/a-poster.jpg"},
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/flip/W9-FLIP.mp4", FolderPath: "/dst/flip"},
	}))
	require.NoError(t, rl.CompleteFailed(ctx, opID, &ApplyResult{
		Movie:          &models.Movie{ID: "W9-FLIP"},
		NFOPath:        "/dst/flip/final.nfo",
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dst/flip/W9-FLIP.mp4"},
	}))

	recordID, err := strconv.ParseUint(opID, 10, 64)
	require.NoError(t, err)
	var record models.BatchFileOperation
	require.NoError(t, db.Where("id = ?", uint(recordID)).First(&record).Error)

	require.Equal(t, models.RevertStatusFailed, record.RevertStatus, "the failure status sticks")
	gf, err := models.ParseGeneratedFiles(record.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the journal entry survives the status flip")
	require.Equal(t, "/dst/flip/poster.jpg", gf.Replacements[0].Destination)
	require.Contains(t, gf.Roots, "/dst/flip", "the completed root survives")
	require.Contains(t, gf.Delete, "/dst/flip/final.nfo", "the latest payload merges against the fresh row")
}

// TestW9CompleteJournalsCopiedSubtitlesAsDelete pins the #224 phase E
// copy/move distinction at the Complete journal boundary: copy-installed
// subtitles join Delete (their source was retained), move-installed join
// MoveBack, skipped/planned entries journal nothing.
func TestW9CompleteJournalsCopiedSubtitlesAsDelete(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, nil, "job-w9", nil, nil, nil, nil)

	var txPersisted string
	repo.On("FindByID", mock.Anything, uint(11)).Return(&models.BatchFileOperation{
		ID: 11, BatchJobID: "job-w9", MovieID: "W9-011",
		RevertStatus: models.RevertStatusApplied,
	}, nil)
	runFnMock(repo, 11, &models.BatchFileOperation{ID: 11, RevertStatus: models.RevertStatusApplied}, &txPersisted)
	repo.On("UpdateNonJournalFields", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(nil)

	err := log.Complete(context.Background(), "11", &ApplyResult{
		Movie: &models.Movie{ID: "W9-011"},
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:    "/dst/w9-011.mp4",
			FolderPath: "/dst/lib",
			Subtitles: []organizer.SubtitleResult{
				{SubtitleMove: models.SubtitleMove{OriginalPath: "/src/m.srt", NewPath: "/dst/lib/m.srt", Moved: true}},
				{SubtitleMove: models.SubtitleMove{OriginalPath: "/src/c.srt", NewPath: "/dst/lib/c.srt", Copied: true}},
				{SubtitleMove: models.SubtitleMove{OriginalPath: "/src/s.srt", NewPath: "/dst/lib/s.srt"}, Skipped: true},
				{SubtitleMove: models.SubtitleMove{OriginalPath: "/src/p.srt", NewPath: "/dst/lib/p.srt"}, Planned: true},
			},
		},
	})
	require.NoError(t, err)

	gf, perr := models.ParseGeneratedFiles(txPersisted)
	require.NoError(t, perr)
	assert.Equal(t, []string{"/dst/lib/c.srt"}, gf.Delete)
	require.Len(t, gf.MoveBack, 1)
	assert.Equal(t, models.FileMove{OriginalPath: "/src/m.srt", NewPath: "/dst/lib/m.srt"}, gf.MoveBack[0])
}

// TestW9CompleteFailedJournalsCopiedSubtitlesAsDelete is the CompleteFailed
// twin of the #224 phase E journal boundary.
func TestW9CompleteFailedJournalsCopiedSubtitlesAsDelete(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, nil, "job-w9", nil, nil, nil, nil)

	var txPersisted string
	var saved models.BatchFileOperation
	repo.On("FindByID", mock.Anything, uint(12)).Return(&models.BatchFileOperation{
		ID: 12, BatchJobID: "job-w9", MovieID: "W9-012",
		RevertStatus: models.RevertStatusApplied,
	}, nil)
	runFnMock(repo, 12, &models.BatchFileOperation{ID: 12, RevertStatus: models.RevertStatusApplied}, &txPersisted)
	repo.On("UpdateNonJournalFields", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).
		Run(func(args mock.Arguments) { saved = *args.Get(1).(*models.BatchFileOperation) }).Return(nil)

	err := log.CompleteFailed(context.Background(), "12", &ApplyResult{
		Movie: &models.Movie{ID: "W9-012"},
		OrganizeResult: &organizer.OrganizeResult{
			NewPath: "/dst/w9-012.mp4",
			Subtitles: []organizer.SubtitleResult{
				{SubtitleMove: models.SubtitleMove{OriginalPath: "/src/c.srt", NewPath: "/dst/lib/c.srt", Copied: true}},
			},
		},
	})
	require.NoError(t, err)

	gf, perr := models.ParseGeneratedFiles(txPersisted)
	require.NoError(t, perr)
	assert.Equal(t, []string{"/dst/lib/c.srt"}, gf.Delete)
	assert.Empty(t, gf.MoveBack)
	assert.Equal(t, models.RevertStatusFailed, saved.RevertStatus)
}
