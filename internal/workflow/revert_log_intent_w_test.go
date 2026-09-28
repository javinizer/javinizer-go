package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pending MoveBack intents must survive later journal merges: dropping one
// erases the armed inverse of an already-moved file.
func TestCompletionLedgerMergePreservesPriorMoveBack(t *testing.T) {
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Roots:    []string{"/lib"},
		MoveBack: []models.FileMove{{OriginalPath: "/in/a.mp4", NewPath: "/lib/a.mp4"}},
	})
	subtitleOnly := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		MoveBack: []models.FileMove{{OriginalPath: "/in/a.srt", NewPath: "/lib/a.srt"}},
	})

	next, persist, _, err := completionLedgerMerge(prior, subtitleOnly, "")
	require.NoError(t, err)
	require.True(t, persist)
	assert.ElementsMatch(t,
		[]models.FileMove{{OriginalPath: "/in/a.mp4", NewPath: "/lib/a.mp4"}, {OriginalPath: "/in/a.srt", NewPath: "/lib/a.srt"}},
		next.MoveBack, "prior intents merge with fresh payloads")

	// Dedupe: prior holds two intents; the fresh payload restates one of them
	// (the outcome completion restating a subtitle move) and adds another —
	// the restated pair must appear once.
	prior = models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		MoveBack: []models.FileMove{
			{OriginalPath: "/in/a.mp4", NewPath: "/lib/a.mp4"},
			{OriginalPath: "/in/a.srt", NewPath: "/lib/a.srt"},
		},
	})
	restated := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		MoveBack: []models.FileMove{
			{OriginalPath: "/in/a.srt", NewPath: "/lib/a.srt"},
			{OriginalPath: "/in/b.srt", NewPath: "/lib/b.srt"},
		},
	})
	next, _, _, err = completionLedgerMerge(prior, restated, "")
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]models.FileMove{
			{OriginalPath: "/in/a.mp4", NewPath: "/lib/a.mp4"},
			{OriginalPath: "/in/a.srt", NewPath: "/lib/a.srt"},
			{OriginalPath: "/in/b.srt", NewPath: "/lib/b.srt"},
		},
		next.MoveBack, "identical intents are not duplicated")

	// Empty fresh payload keeps prior state (incl. MoveBack) untouched.
	next, persist, _, err = completionLedgerMerge(prior, "", "")
	require.NoError(t, err)
	if persist {
		require.Len(t, next.MoveBack, 1)
	}
}

// RecordDeleteIntent journals planned deletions without touching completion
// columns — a crash between install and outcome completion leaves the paths
// revertable.
func TestRecordDeleteIntentJournalsPlannedDeletions(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "delete-intent", "")
	fs, root, source, _, _, _, match := pr260FencedFiles(t, "delete-intent")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "delete-intent", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)

	planned := []string{filepath.Join(dest, "movie", "movie.nfo"), filepath.Join(dest, "movie", "poster.jpg")}
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, planned))
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, source, filepath.Join(dest, "movie", "movie.mp4")))

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	require.NotNil(t, row)
	assert.Empty(t, row.NewPath)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	assert.ElementsMatch(t, planned, gf.Delete)
	require.Len(t, gf.MoveBack, 1, "the pending move intent survives the delete-intent merge")
	assert.NotEmpty(t, gf.Roots)

	require.NoError(t, (noOpRevertLog{}).RecordDeleteIntent(context.Background(), opID, planned), "noop intent writer")
	require.NoError(t, log.RecordDeleteIntent(context.Background(), "", planned))

	// Delete carry dedupes paths already present in the fresh payload.
	again := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Delete: append([]string{planned[0]}, filepath.Join(dest, "movie", "extra.sup"))})
	mergedAgain, _, _, mergeErr := completionLedgerMerge(row.GeneratedFiles, again, "")
	require.NoError(t, mergeErr)
	require.Len(t, mergedAgain.Delete, 3, "planned + restated + new path, deduplicated")
	require.Error(t, log.RecordDeleteIntent(context.Background(), "abc", planned))
	require.Error(t, log.RecordDeleteIntent(context.Background(), "99999999", planned))
}

// A corrupt journal surfaces as a reconcile error; the no-op writer is
// covered explicitly.
func TestReconcileMoveIntentsErrorsOnCorruptJournal(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "reconcile-corrupt", "")
	fs, root, _, _, _, _, match := pr260FencedFiles(t, "reconcile-corrupt")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "reconcile-corrupt", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)
	id := mustParseOpID(t, opID)
	// force the journal to malformed JSON through the raw model update
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", id).Update("generated_files", "{broken").Error)

	err = log.ReconcileMoveIntents(context.Background(), opID, nil)
	require.Error(t, err, "malformed journal propagate")
	require.NoError(t, (noOpRevertLog{}).ReconcileMoveIntents(context.Background(), opID, nil))
}
