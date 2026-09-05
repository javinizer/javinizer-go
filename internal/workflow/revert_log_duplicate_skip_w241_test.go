package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// w241 harness variants: sqlite-backed revert log over a SHARED memfs so the
// same afero filesystem serves the real organizer, the revert log, and the
// history reverter (#241 P1).
func newW241Harness(t *testing.T) (*database.DB, *database.BatchFileOperationRepository, afero.Fs, RevertLog) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()
	rl := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "job-w241", fs, nil, nil, nil)
	return db, repo, fs, rl
}

func w241Row(t *testing.T, repo *database.BatchFileOperationRepository, opID OperationID) *models.BatchFileOperation {
	t.Helper()
	var id uint
	_, err := fmt.Sscanf(opID, "%d", &id)
	require.NoError(t, err)
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, row)
	return row
}

func w241Begin(t *testing.T, rl RevertLog, movieID, src string) OperationID {
	t.Helper()
	opID, err := rl.Begin(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: movieID},
		Match:    models.FileMatchInfo{MovieID: movieID, Path: src, Name: movieID + ".mkv", Extension: ".mkv"},
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, err)
	require.NotEmpty(t, opID)
	return opID
}

const w241Target = "/dest/shared/shared.mkv"

// TestRevertLog_Complete_DuplicateSkipped_JournalsNoPrimaryMove pins the
// #241 P1 journal rule on BOTH completion legs: the authorized duplicate
// skip lands NO NewPath / in-place / subtitle-move payload on the loser's
// row — its OrganizeResult.NewPath names the winner's shared destination for
// display only — while an ordinary moved result still journals its move, and
// the loser's begin-seeded ledger carries no winner-folder discovery root.
func TestRevertLog_Complete_DuplicateSkipped_JournalsNoPrimaryMove(t *testing.T) {
	_, repo, _, rl := newW241Harness(t)
	ctx := context.Background()

	opW := w241Begin(t, rl, "ABC-100", "/in/A.mkv")
	require.NoError(t, rl.Complete(ctx, opW, &ApplyResult{
		Movie: &models.Movie{ID: "ABC-100"},
		OrganizeResult: &organizer.OrganizeResult{
			OriginalPath: "/in/A.mkv",
			NewPath:      w241Target,
			FolderPath:   "/dest/shared",
			Moved:        true,
		},
	}))

	opL := w241Begin(t, rl, "ABC-200", "/in/B.mkv")
	require.NoError(t, rl.Complete(ctx, opL, &ApplyResult{
		Movie:   &models.Movie{ID: "ABC-200"},
		NFOPath: "/dest/shared/ABC-200.nfo",
		OrganizeResult: &organizer.OrganizeResult{
			OriginalPath:     "/in/B.mkv",
			NewPath:          w241Target,
			FolderPath:       "/dest/shared",
			Moved:            false,
			DuplicateSkipped: true,
			Warnings:         []string{"duplicate destination within batch: " + w241Target + " already claimed by /in/A.mkv (overwrite authorized)"},
		},
	}))

	opF := w241Begin(t, rl, "ABC-300", "/in/C.mkv")
	require.NoError(t, rl.CompleteFailed(ctx, opF, &ApplyResult{
		Movie:         &models.Movie{ID: "ABC-300"},
		DownloadPaths: []string{"/dest/shared/poster.jpg"},
		OrganizeResult: &organizer.OrganizeResult{
			OriginalPath:     "/in/C.mkv",
			NewPath:          w241Target,
			FolderPath:       "/dest/shared",
			Moved:            false,
			DuplicateSkipped: true,
		},
	}))

	rowW := w241Row(t, repo, opW)
	assert.Equal(t, w241Target, rowW.NewPath, "the winner's primary move stays journaled")
	assert.Equal(t, models.RevertStatusApplied, rowW.RevertStatus)
	ledgerW, err := models.ParseGeneratedFiles(rowW.GeneratedFiles)
	require.NoError(t, err)
	assert.Contains(t, ledgerW.Roots, "/dest/shared", "the moving result still seeds its leaf discovery root")

	rowL := w241Row(t, repo, opL)
	rowF := w241Row(t, repo, opF)
	for _, tc := range []struct {
		name string
		row  *models.BatchFileOperation
	}{
		{"Complete", rowL},
		{"CompleteFailed", rowF},
	} {
		row := tc.row
		assert.Empty(t, row.NewPath, "%s: the skipped duplicate journals NO primary-move record (#241 P1)", tc.name)
		assert.False(t, row.InPlaceRenamed, "%s: no in-place payload either", tc.name)
	}
	assert.Equal(t, models.RevertStatusApplied, rowL.RevertStatus)
	ledgerL, err := models.ParseGeneratedFiles(rowL.GeneratedFiles)
	require.NoError(t, err)
	assert.Empty(t, ledgerL.MoveBack, "the loser row carries no winner subtitle/asset move-backs")
	assert.NotContains(t, ledgerL.Roots, "/dest/shared", "the winner's folder is never seeded from the skipped duplicate's completion")
	assert.Contains(t, ledgerL.Delete, "/dest/shared/ABC-200.nfo", "the loser's own generated artifacts still journal")

	assert.Equal(t, models.RevertStatusFailed, rowF.RevertStatus, "the failed-skipped row keeps its failure status")
	ledgerF, err := models.ParseGeneratedFiles(rowF.GeneratedFiles)
	require.NoError(t, err)
	assert.Contains(t, ledgerF.Delete, "/dest/shared/poster.jpg", "download artifacts of the failed op still journal")
}

// TestRevertLog_DuplicateSkipLoser_RevertLeavesWinnerUntouched is the #241 P1
// revert-level regression: winner + authorized-duplicate loser complete a
// real primed organize onto ONE shared destination; reverting the LOSER must
// do nothing to the winner's moved video (no primary-move record exists for
// it), and the winner's own revert stays fully functional.
func TestRevertLog_DuplicateSkipLoser_RevertLeavesWinnerUntouched(t *testing.T) {
	_, repo, fs, rl := newW241Harness(t)
	ctx := context.Background()

	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	org := organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:  "shared",
		FileFormat:    "shared",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	tracker := organizer.NewDuplicateTracker(false)
	tracker.PrimeBatch([]organizer.DuplicatePriming{
		{SourcePath: "/in/A.mkv", TargetPath: w241Target, WillMove: true},
		{SourcePath: "/in/B.mkv", TargetPath: w241Target, WillMove: true},
	})
	w241Cmd := func(movieID, src, name string) organizer.OrganizeCmd {
		return organizer.OrganizeCmd{
			Match:            models.FileMatchInfo{MovieID: movieID, Path: src, Name: name, Extension: ".mkv"},
			Movie:            &models.Movie{ID: movieID},
			DestDir:          "/dest",
			MoveFiles:        true,
			ForceUpdate:      true,
			DuplicateTracker: tracker,
		}
	}

	resW, err := org.Organize(ctx, w241Cmd("ABC-100", "/in/A.mkv", "A.mkv"))
	require.NoError(t, err)
	require.True(t, resW.Moved)
	require.False(t, resW.DuplicateSkipped, "the winner is never the skip")
	resL, err := org.Organize(ctx, w241Cmd("ABC-200", "/in/B.mkv", "B.mkv"))
	require.NoError(t, err)
	require.True(t, resL.DuplicateSkipped)
	require.False(t, resL.Moved)
	require.Equal(t, w241Target, resL.NewPath, "visible winner/skip semantics: NewPath still names the shared destination")

	opW := w241Begin(t, rl, "ABC-100", "/in/A.mkv")
	opL := w241Begin(t, rl, "ABC-200", "/in/B.mkv")
	require.NoError(t, rl.Complete(ctx, opW, &ApplyResult{Movie: &models.Movie{ID: "ABC-100"}, OrganizeResult: resW}))
	require.NoError(t, rl.Complete(ctx, opL, &ApplyResult{Movie: &models.Movie{ID: "ABC-200"}, OrganizeResult: resL}))
	require.Equal(t, w241Target, w241Row(t, repo, opW).NewPath)
	require.Empty(t, w241Row(t, repo, opL).NewPath, "codex P1 (PR #241): the skipped loser has no primary-move revert record")

	reverter := history.NewReverter(fs, repo)

	// Revert the LOSER: the winner's moved video must be left exactly where
	// the batch put it — the pre-fix journal armed the winner's bytes as the
	// loser's primary moved file, so this revert could drag a-bytes onto
	// /in/B.mkv (or fail against the retained loser source).
	rb, err := reverter.RevertScrape(ctx, "job-w241", "ABC-200")
	require.NoError(t, err)
	require.Len(t, rb.Outcomes, 1)
	assert.NotEqual(t, models.RevertOutcomeReverted, rb.Outcomes[0].Outcome,
		"the loser revert must never treat the winner's video as movable (anchor-missing skip on real filesystems, destination-conflict refusal on memfs roots)")
	winnerBytes, err := afero.ReadFile(fs, w241Target)
	require.NoError(t, err)
	assert.Equal(t, []byte("a-bytes"), winnerBytes, "loser revert left the winner's video untouched")
	loserSrc, err := afero.ReadFile(fs, "/in/B.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("b-bytes"), loserSrc, "the skipped loser's untouched source is never dislodged either")

	// The winner's OWN revert is unaffected: its move reverts normally.
	rb, err = reverter.RevertScrape(ctx, "job-w241", "ABC-100")
	require.NoError(t, err)
	require.Len(t, rb.Outcomes, 1)
	assert.Equal(t, models.RevertOutcomeReverted, rb.Outcomes[0].Outcome)
	restored, err := afero.ReadFile(fs, "/in/A.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("a-bytes"), restored, "winner revert moved its video back")
	exists, err := afero.Exists(fs, w241Target)
	require.NoError(t, err)
	assert.False(t, exists, "the shared destination is vacated by the winner's revert only")
}
