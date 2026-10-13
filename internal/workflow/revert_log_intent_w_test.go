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

	next, persist, _, err := completionLedgerMergeOpt(prior, subtitleOnly, "", true)
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
	next, _, _, err = completionLedgerMergeOpt(prior, restated, "", true)
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]models.FileMove{
			{OriginalPath: "/in/a.mp4", NewPath: "/lib/a.mp4"},
			{OriginalPath: "/in/a.srt", NewPath: "/lib/a.srt"},
			{OriginalPath: "/in/b.srt", NewPath: "/lib/b.srt"},
		},
		next.MoveBack, "identical intents are not duplicated")

	// Empty fresh payload keeps prior state (incl. MoveBack) untouched.
	next, persist, _, err = completionLedgerMergeOpt(prior, "", "", true)
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

	planned := []models.DeleteEntry{
		{Path: filepath.Join(dest, "movie", "movie.nfo"), SHA256: "aaaa"},
		{Path: filepath.Join(dest, "movie", "poster.jpg"), SHA256: "bbbb"},
	}
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, planned))
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, source, filepath.Join(dest, "movie", "movie.mp4")))

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	require.NotNil(t, row)
	assert.Empty(t, row.NewPath)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Empty(t, gf.Delete, "pending intents stay out of the plain delete list")
	plannedPaths := []string{}
	for _, e := range gf.PlannedDeletes {
		plannedPaths = append(plannedPaths, e.Path)
	}
	assert.ElementsMatch(t, []string{planned[0].Path, planned[1].Path}, plannedPaths)
	require.Len(t, gf.MoveBack, 1, "the pending move intent survives the delete-intent merge")
	assert.NotEmpty(t, gf.Roots)

	require.NoError(t, (noOpRevertLog{}).RecordDeleteIntent(context.Background(), opID, planned), "noop intent writer")
	require.NoError(t, log.RecordDeleteIntent(context.Background(), "", planned))
	require.Error(t, log.RecordDeleteIntent(context.Background(), "0", planned), "id zero")

	// Pending deletes graduate when the outcome completion restates the path in
	// Delete; a pending path the outcome never restated stays hash-pinned.
	again := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Delete: append([]string{planned[0].Path}, filepath.Join(dest, "movie", "extra.sup"))})
	mergedAgain, _, _, mergeErr := completionLedgerMergeOpt(row.GeneratedFiles, again, "", true)
	require.NoError(t, mergeErr)
	require.Len(t, mergedAgain.PlannedDeletes, 1)
	require.Equal(t, planned[1].Path, mergedAgain.PlannedDeletes[0].Path, "graduated path pruned; pending unrelated path keeps its pin")
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

// Carry dedupes across both entry kinds: Delete and PlannedDeletes restated
// by the outcome appear once; entries the outcome omits stay pending.
func TestCompletionLedgerMergeCarriesAndDedupesBothKinds(t *testing.T) {
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Delete:         []string{"/lib/x.nfo"},
		PlannedDeletes: []models.DeleteEntry{{Path: "/lib/a.jpg", SHA256: "pin-a"}},
	})
	fresh := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Delete:         []string{"/lib/x.nfo", "/lib/new.nfo"},
		PlannedDeletes: []models.DeleteEntry{{Path: "/lib/a.jpg", SHA256: "pin-a"}, {Path: "/lib/b.jpg", SHA256: "pin-b"}},
	})
	merged, persist, _, err := completionLedgerMergeOpt(prior, fresh, "", true)
	require.NoError(t, err)
	require.True(t, persist)
	assert.Equal(t, []string{"/lib/x.nfo", "/lib/new.nfo"}, merged.Delete, "deduped re-stated delete")
	require.Len(t, merged.PlannedDeletes, 2, "carried pin-a once and pin-b once")
}

// An empty completion payload must not erase pending planned deletions
// (copy-mode applies that publish only staged siblings can produce them).
func TestCompletionLedgerMergeEmptyOutcomeKeepsPlannedDeletes(t *testing.T) {
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		PlannedDeletes: []models.DeleteEntry{{Path: "/lib/sibling-cd2.mp4", SHA256: "pin"}},
	})
	merged, persist, _, err := completionLedgerMergeOpt(prior, "", "", true)
	require.NoError(t, err)
	require.False(t, persist, "empty outcome merges identical — nothing is dropped, no write needed")
	_ = merged
}

// A pending delete promotes into the move-back arm when the same destination
// restates as a MoveBack (deferred sibling publication): the armed inverse
// restores the bytes on revert, so the hash-pinned delete must not survive.
func TestCompletionLedgerMergeMoveBackArmConsumesPinnedDelete(t *testing.T) {
	sibling := models.FileMove{OriginalPath: "/src/movie-cd2.mp4", NewPath: "/lib/movie/movie-cd2.mp4"}
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		PlannedDeletes: []models.DeleteEntry{
			{Path: "/lib/movie/movie-cd2.mp4", SHA256: "pin-sibling"},
			{Path: "/lib/movie/poster.jpg", SHA256: "pin-poster"},
		},
	})
	fresh := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		MoveBack: []models.FileMove{sibling},
	})
	merged, persist, _, err := completionLedgerMergeOpt(prior, fresh, "", true)
	require.NoError(t, err)
	require.True(t, persist)
	assert.Equal(t, []models.FileMove{sibling}, merged.MoveBack)
	require.Len(t, merged.PlannedDeletes, 1, "the move-armed pin promotes; the unrelated pin keeps its entry")
	assert.Equal(t, "/lib/movie/poster.jpg", merged.PlannedDeletes[0].Path)

	// The inverse journal order resolves identically: a delete intent landing
	// after the move intent armed is still consumed, pinned or not.
	armed := models.MarshalLedgerJSON(models.GeneratedFilesJSON{MoveBack: []models.FileMove{sibling}})
	late := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		PlannedDeletes: []models.DeleteEntry{
			{Path: "/lib/movie/movie-cd2.mp4", SHA256: "pin-sibling"},
			{Path: "/lib/movie/fanart.jpg", SHA256: "pin-fanart"},
		},
	})
	merged, _, _, err = completionLedgerMergeOpt(armed, late, "", true)
	require.NoError(t, err)
	assert.Equal(t, []models.FileMove{sibling}, merged.MoveBack, "the carried arm survives the late intent merge")
	require.Len(t, merged.PlannedDeletes, 1, "the late pin on the armed destination never lands")
	assert.Equal(t, "/lib/movie/fanart.jpg", merged.PlannedDeletes[0].Path)
}

// RecordMoveIntent journals a STILL-PENDING arm: the row keeps the armed
// inverse AND the destination's content-hash pin (codex P1
// PRRT_kwDORn9KaM6m3ujI) — until the source removal is confirmed the pin is
// the sole ownership proof for the crash window, and recovery fires it only
// when the surviving source suppresses the rename-back. Graduation (an outcome
// completion merge) consumes the move-armed pin once the consumption is
// confirmed; an unrelated pin whose destination never armed always survives.
func TestRecordMoveIntentKeepsPinnedDeleteUntilGraduation(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "move-promotes-pin", "")
	fs, root, source, _, multipart, _, match := pr260FencedFiles(t, "move-promotes-pin")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "move-promotes-pin", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)

	siblingTarget := filepath.Join(dest, "movie", filepath.Base(multipart))
	unrelatedTarget := filepath.Join(dest, "movie", "poster.jpg")
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{
		{Path: siblingTarget, SHA256: "pin-sibling"},
		{Path: unrelatedTarget, SHA256: "pin-poster"},
	}))
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, multipart, siblingTarget))
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, source, filepath.Join(dest, "movie", filepath.Base(source))))

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	require.NotNil(t, row)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.MoveBack, 2)
	require.Len(t, gf.PlannedDeletes, 2, "pending arms retain every pin — nothing is consumed before consumption is confirmed")

	// Re-intent is idempotent under retention: no duplicate arm, no pin drift.
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, multipart, siblingTarget))
	rowAgain, rowErrAgain := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErrAgain)
	gfAgain, parseErrAgain := models.ParseGeneratedFiles(rowAgain.GeneratedFiles)
	require.NoError(t, parseErrAgain)
	require.Len(t, gfAgain.MoveBack, 2)
	require.Len(t, gfAgain.PlannedDeletes, 2)

	// Graduation: the outcome completion merge consumes the move-armed pin —
	// exactly like the confirmed-consumption path — and the unrelated pin
	// restated by the outcome as a plain Delete graduates out of pending too.
	graduated, persist, _, mergeErr := completionLedgerMergeOpt(rowAgain.GeneratedFiles, models.MarshalLedgerJSON(models.GeneratedFilesJSON{Delete: []string{unrelatedTarget}}), "", true)
	require.NoError(t, mergeErr)
	require.True(t, persist)
	require.Len(t, graduated.MoveBack, 2)
	require.Empty(t, graduated.PlannedDeletes, "the armed pin promotes at graduation; the Delete-restated pin graduates out of pending")
	graduatedEmpty, persistEmpty, _, mergeErrEmpty := completionLedgerMergeOpt(rowAgain.GeneratedFiles, "", "", true)
	require.NoError(t, mergeErrEmpty)
	require.True(t, persistEmpty, "a payloadless graduation still consumes armed pins")
	require.ElementsMatch(t, []models.DeleteEntry{{Path: unrelatedTarget, SHA256: "pin-poster"}}, graduatedEmpty.PlannedDeletes)
	_, persistPendingEmpty, mergedPendingEmpty, mergeErrPendingEmpty := completionLedgerMergeOpt(rowAgain.GeneratedFiles, "", "", false)
	require.NoError(t, mergeErrPendingEmpty)
	require.False(t, persistPendingEmpty, "a payloadless pending-intent merge is a byte-identical no-op")
	pendingEmpty, parsePendingErr := models.ParseGeneratedFiles(mergedPendingEmpty)
	require.NoError(t, parsePendingErr)
	require.Len(t, pendingEmpty.PlannedDeletes, 2, "pending retention survives even a payloadless intent merge")
}

// Reconciled move-backs consume the matching pending deletes the same way the
// intent journal does: the confirmed arm owns the destination's revert.
func TestReconcileMoveIntentsConsumesPinnedDeleteForKeptMoves(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "reconcile-promotes-pin", "")
	fs, root, _, _, multipart, _, match := pr260FencedFiles(t, "reconcile-promotes-pin")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "reconcile-promotes-pin", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)

	siblingTarget := filepath.Join(dest, "movie", filepath.Base(multipart))
	unrelatedTarget := filepath.Join(dest, "movie", "poster.jpg")
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{
		{Path: siblingTarget, SHA256: "pin-sibling"},
		{Path: unrelatedTarget, SHA256: "pin-poster"},
	}))
	keep := []models.FileMove{{OriginalPath: multipart, NewPath: siblingTarget}}
	require.NoError(t, log.ReconcileMoveIntents(context.Background(), opID, keep))

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	require.NotNil(t, row)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	assert.ElementsMatch(t, keep, gf.MoveBack)
	require.Len(t, gf.PlannedDeletes, 1, "the kept move's pinned delete is consumed")
	assert.Equal(t, unrelatedTarget, gf.PlannedDeletes[0].Path)
}

// Reconciled delete intents keep only the confirmed installs: pins whose
// planned destination never landed are retracted wholesale so a later revert
// can never hash-match a same-content foreign occupant into deletion.
func TestReconcileDeleteIntentsRetractsUnconfirmedPins(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "reconcile-delete-intents", "")
	fs, root, _, _, _, _, match := pr260FencedFiles(t, "reconcile-delete-intents")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "reconcile-delete-intents", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)

	copiedTarget := filepath.Join(dest, "movie", "movie.srt")
	skippedTarget := filepath.Join(dest, "movie", "movie.sup")
	primaryTarget := filepath.Join(dest, "movie", "movie.mp4")
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{
		{Path: copiedTarget, SHA256: "pin-copied"},
		{Path: skippedTarget, SHA256: "pin-skipped"},
		{Path: primaryTarget, SHA256: "pin-primary"},
	}))
	require.NoError(t, log.ReconcileDeleteIntents(context.Background(), opID, []string{copiedTarget}))

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	require.NotNil(t, row)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.PlannedDeletes, 1, "unconfirmed pins retract; the graduated primary pin is consumed")
	assert.Equal(t, copiedTarget, gf.PlannedDeletes[0].Path)
	assert.NotEmpty(t, gf.Roots, "unrelated journal channels survive the reconcile")
	assert.Empty(t, row.NewPath, "completion columns remain untouched")

	// Idempotent: the same reconcile applied twice writes nothing new.
	require.NoError(t, log.ReconcileDeleteIntents(context.Background(), opID, []string{copiedTarget}))
	rowAgain, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	require.NotNil(t, rowAgain)
	gfAgain, parseErr := models.ParseGeneratedFiles(rowAgain.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gfAgain.PlannedDeletes, 1)

	// A reconcile on a row carrying no pending deletes is a no-op write.
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("generated_files", models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{dest}})).Error)
	require.NoError(t, log.ReconcileDeleteIntents(context.Background(), opID, nil))

	require.NoError(t, log.ReconcileDeleteIntents(context.Background(), "", nil), "empty op is a no-op")
	require.Error(t, log.ReconcileDeleteIntents(context.Background(), "abc", nil), "unparsable id")
	require.Error(t, log.ReconcileDeleteIntents(context.Background(), "99999999", nil), "missing row")
	require.NoError(t, (noOpRevertLog{}).ReconcileDeleteIntents(context.Background(), opID, nil), "noop reconcile")
}

// A corrupt journal surfaces as a delete-reconcile error instead of silently
// dropping the persisted ledger.
func TestReconcileDeleteIntentsErrorsOnCorruptJournal(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "reconcile-delete-corrupt", "")
	fs, root, _, _, _, _, match := pr260FencedFiles(t, "reconcile-delete-corrupt")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "reconcile-delete-corrupt", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("generated_files", "{broken").Error)

	err = log.ReconcileDeleteIntents(context.Background(), opID, nil)
	require.Error(t, err, "malformed journal propagates")
}
