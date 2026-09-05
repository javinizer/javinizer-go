package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// newW241PrePublishRig builds the shared #241 batch-2 F1 rig: a REAL sqlite
// revert log + apply orchestrators over ONE memfs, a duplicate tracker primed
// with two moving claimants of w241Target, and both source files present.
func newW241PrePublishRig(t *testing.T) (repo *database.BatchFileOperationRepository, fs afero.Fs, tracker *organizer.DuplicateTracker, orchFor func() *applyOrchImpl, cmdFor func(movieID, src, name string) ApplyCmd) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo = database.NewBatchFileOperationRepository(db)
	fs = afero.NewMemMapFs()
	rl := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "job-w241", fs, nil, nil, nil)

	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))

	org := organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:  "shared",
		FileFormat:    "shared",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	tracker = organizer.NewDuplicateTracker(false)
	tracker.PrimeBatch([]organizer.DuplicatePriming{
		{SourcePath: "/in/A.mkv", TargetPath: w241Target, WillMove: true},
		{SourcePath: "/in/B.mkv", TargetPath: w241Target, WillMove: true},
	})
	orchFor = func() *applyOrchImpl {
		return &applyOrchImpl{fs: fs, organizer: org, revertLog: rl, nfo: &applyStubNFO{}}
	}
	cmdFor = func(movieID, src, name string) ApplyCmd {
		return ApplyCmd{
			Movie:    &models.Movie{ID: movieID, Title: movieID + " Title"},
			Match:    models.FileMatchInfo{MovieID: movieID, Path: src, Name: name, Extension: ".mkv"},
			DestPath: "/dest",
			Organize: OrganizeOptions{MoveFiles: true, ForceUpdate: true, DuplicateTracker: tracker},
		}
	}
	return repo, fs, tracker, orchFor, cmdFor
}

// TestApply_PrePublicationOwner_RevertCannotDragWinnerBytes is the codex PR
// #241 batch-2 F1 regression, end-to-end: a ForceUpdate primed owner whose
// source vanishes in the priming→worker gap skips validation, fails execute
// PRE-publish, and releases its claim; the promoted claimant then publishes
// the shared destination. The failed owner's row must journal NO target
// fields and finalize completed-noop, so reverting it (scrape-level and
// batch-level) is a pure no-op — ENOENT-free, with the winner's published
// bytes byte-intact and nothing dragged onto the departed owner's source.
func TestApply_PrePublicationOwner_RevertCannotDragWinnerBytes(t *testing.T) {
	ctx := context.Background()
	repo, fs, _, orchFor, cmdFor := newW241PrePublishRig(t)

	// The priming→worker vanish window: the owner's source disappears AFTER
	// PrimeBatch (production priming verifies sources — primed owners can
	// still vanish before their worker runs).
	require.NoError(t, fs.Remove("/in/A.mkv"))

	failRes, failErr := orchFor().Execute(ctx, cmdFor("ABC-100", "/in/A.mkv", "A.mkv"))
	require.Error(t, failErr, "the vanished-source owner fails its organize step")
	require.NotNil(t, failRes)
	assert.Equal(t, "organize", failRes.FailedStep)
	assert.True(t, failRes.PrePublication, "the apply-level marker reaches callers")
	require.NotNil(t, failRes.OrganizeResult)
	assert.True(t, failRes.OrganizeResult.PrePublication,
		"a non-PublishCompleted execute error marks the returned result pre-publication")
	assert.False(t, fsutil.PublishCompleted(failErr) || fsutil.PublishCompleted(failRes.OrganizeResult.Error),
		"the vanished source never published — the typed partial-publish class stays clear")
	assert.False(t, failRes.OrganizeResult.Moved)
	assert.Equal(t, w241Target, filepath.ToSlash(failRes.OrganizeResult.NewPath),
		"the failed result keeps naming the shared destination for display (intent, not outcome)")

	// The released claim PROMOTES the standby claimant: it now owns the key,
	// executes, and publishes the shared destination.
	okRes, okErr := orchFor().Execute(ctx, cmdFor("ABC-200", "/in/B.mkv", "B.mkv"))
	require.NoError(t, okErr)
	require.NotNil(t, okRes.OrganizeResult)
	require.True(t, okRes.OrganizeResult.Moved, "the promoted claimant published the shared destination")
	assert.Equal(t, []byte("b-bytes"), mustReadW241(t, fs, w241Target))

	// Journal truth: the failed owner's row is completed-noop with NO target
	// fields — not the pre-fix failed+NewPath=shared-target shape that armed
	// its revert against the winner's bytes.
	rowF := w241Row(t, repo, failRes.OperationID)
	assert.Equal(t, models.RevertStatusNoOp, rowF.RevertStatus)
	assert.Empty(t, rowF.NewPath, "pre-publication failure journals NO NewPath")
	assert.False(t, rowF.InPlaceRenamed)
	assert.Empty(t, rowF.NFOPath, "no NFO finger on the failed row names a shared path either")
	ledgerF, err := models.ParseGeneratedFiles(rowF.GeneratedFiles)
	require.NoError(t, err)
	assert.Empty(t, ledgerF.Delete, "no subtitle/download/extras finger names the shared destination")
	assert.Empty(t, ledgerF.MoveBack)
	assert.NotContains(t, slashNormalizeW241Paths(ledgerF.Roots), filepath.ToSlash("/dest/shared"),
		"the winner's leaf folder is never seeded from the failed owner's row")
	rowW := w241Row(t, repo, okRes.OperationID)
	assert.Equal(t, models.RevertStatusApplied, rowW.RevertStatus)
	assert.Equal(t, w241Target, filepath.ToSlash(rowW.NewPath), "the promoted winner owns the shared target's revert record")

	reverter := history.NewReverter(fs, repo)

	// Reverting the failed OWNER row: a pure no-op — no outcome, no exit-code
	// work, and the winner's published bytes stay exactly where they landed.
	rb, err := reverter.RevertScrape(ctx, "job-w241", "ABC-100")
	require.NoError(t, err, "the noop row is never a revert subject — no anchor probe, no ENOENT")
	assert.Zero(t, rb.Total)
	assert.Empty(t, rb.Outcomes)
	assert.Equal(t, []byte("b-bytes"), mustReadW241(t, fs, w241Target),
		"the winner's bytes are untouched by the failed owner's revert")
	exists, err := afero.Exists(fs, "/in/A.mkv")
	require.NoError(t, err)
	assert.False(t, exists, "nothing resurrects (or drags bytes onto) the departed owner's source")

	// The batch then fully reverts on the winner's real row alone.
	rb, err = reverter.RevertBatch(ctx, "job-w241")
	require.NoError(t, err)
	require.Equal(t, 1, rb.Total)
	assert.Equal(t, 1, rb.Succeeded)
	assert.Zero(t, rb.Skipped, "no anchor_missing outcome from the noop owner row")
	assert.Zero(t, rb.Failed)
	assert.Equal(t, []byte("b-bytes"), mustReadW241(t, fs, "/in/B.mkv"), "the winner's revert restores ITS source")
	exists, err = afero.Exists(fs, filepath.FromSlash(w241Target))
	require.NoError(t, err)
	assert.False(t, exists)
	_, err = reverter.RevertBatch(ctx, "job-w241")
	require.ErrorIs(t, err, history.ErrBatchAlreadyReverted, "noop + reverted leaves nothing behind")
}

// TestRevertLog_CompleteFailed_OrganizerPrePublicationMarker pins the
// CompleteFailed marker seam at the journal boundary: an
// OrganizeResult.PrePublication arrives WITHOUT the apply-level marker
// (callers that journal the organizer's returned failure directly) and must
// still finalize completed-noop with no target fields, while a non-marked
// partial failure keeps NewPath and the failed status (kept-revertable).
func TestRevertLog_CompleteFailed_OrganizerPrePublicationMarker(t *testing.T) {
	_, repo, _, rl := newW241Harness(t)
	ctx := context.Background()

	opPre := w241Begin(t, rl, "PPB-100", "/in/PPB-100.mkv")
	require.NoError(t, rl.CompleteFailed(ctx, opPre, &ApplyResult{
		Movie: &models.Movie{ID: "PPB-100"},
		OrganizeResult: &organizer.OrganizeResult{
			OriginalPath:   "/in/PPB-100.mkv",
			NewPath:        w241Target,
			FolderPath:     "/dest/shared",
			PrePublication: true,
		},
	}))
	rowPre := w241Row(t, repo, opPre)
	assert.Equal(t, models.RevertStatusNoOp, rowPre.RevertStatus,
		"the organizer-side marker alone finalizes completed-noop")
	assert.Empty(t, rowPre.NewPath)
	assert.False(t, rowPre.InPlaceRenamed)
	ledgerPre, err := models.ParseGeneratedFiles(rowPre.GeneratedFiles)
	require.NoError(t, err)
	assert.Empty(t, ledgerPre.Delete)
	assert.Empty(t, ledgerPre.MoveBack)

	opKeep := w241Begin(t, rl, "PPB-200", "/in/PPB-200.mkv")
	require.NoError(t, rl.CompleteFailed(ctx, opKeep, &ApplyResult{
		Movie: &models.Movie{ID: "PPB-200"},
		OrganizeResult: &organizer.OrganizeResult{
			OriginalPath: "/in/PPB-200.mkv",
			NewPath:      w241Target,
			FolderPath:   "/dest/shared",
		},
	}))
	rowKeep := w241Row(t, repo, opKeep)
	assert.Equal(t, models.RevertStatusFailed, rowKeep.RevertStatus,
		"an unmarked partial failure stays kept-revertable")
	assert.Equal(t, w241Target, filepath.ToSlash(rowKeep.NewPath))
}

// TestNoopJournal_ClassifierBoundary pins the no-mutation classifier's
// boundary: a nil ApplyResult (no partial state exists at all) reports
// false, as do partial-publish-shaped failures that must stay kept-revertable.
func TestNoopJournal_ClassifierBoundary(t *testing.T) {
	assert.False(t, noopJournal(nil))
	assert.False(t, noopJournal(&ApplyResult{OrganizeResult: &organizer.OrganizeResult{NewPath: w241Target}}))
	assert.True(t, noopJournal(&ApplyResult{PrePublication: true}))
	assert.True(t, noopJournal(&ApplyResult{OrganizeResult: &organizer.OrganizeResult{PrePublication: true}}))
	assert.True(t, noopJournal(&ApplyResult{OrganizeResult: &organizer.OrganizeResult{DuplicateSkipped: true}}))
}

func mustReadW241(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, filepath.FromSlash(path))
	require.NoError(t, err)
	return data
}
