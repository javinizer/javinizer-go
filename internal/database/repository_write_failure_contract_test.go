package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestTranslationInsertFailuresPreserveExistingLanguages(t *testing.T) {
	t.Run("actress", func(t *testing.T) {
		db := newRollbackContractDB(t)
		actress := models.Actress{FirstName: "Canonical", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, NewActressRepository(db).Create(t.Context(), &actress))
		repo := newActressTranslationRepository(db)
		require.NoError(t, repo.Upsert(t.Context(), &models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Existing", SourceName: "Original source"}))
		before := previewDatabaseState(t, db)
		require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TEMP TRIGGER reject_actress_translation BEFORE INSERT ON actress_translations
WHEN NEW.actress_id = %d AND NEW.language = 'fr' BEGIN SELECT RAISE(ABORT, 'actress translation rejected'); END`, actress.ID)).Error)
		t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_actress_translation").Error })

		incoming := models.ActressTranslation{ActressID: actress.ID, Language: " FR ", DisplayName: "New language", SourceName: "New source"}
		err := repo.Upsert(t.Context(), &incoming)
		require.ErrorContains(t, err, fmt.Sprintf("create actress translation %d/fr", actress.ID))
		requireTriggerFailure(t, err, "actress translation rejected")
		require.Equal(t, "fr", incoming.Language)
		require.Equal(t, before, previewDatabaseState(t, db))
		require.NoError(t, db.Exec("DROP TRIGGER reject_actress_translation").Error)
		require.NoError(t, repo.Upsert(t.Context(), &incoming))
		stored, err := repo.FindByActressAndLanguage(t.Context(), actress.ID, "fr")
		require.NoError(t, err)
		require.Equal(t, incoming.DisplayName, stored.DisplayName)
		translations, err := repo.FindAllByActress(t.Context(), actress.ID)
		require.NoError(t, err)
		require.Len(t, translations, 2)
	})

	t.Run("genre", func(t *testing.T) {
		db := newRollbackContractDB(t)
		genre, err := newGenreRepository(db).FindOrCreate(t.Context(), "Canonical genre")
		require.NoError(t, err)
		repo := newGenreTranslationRepository(db)
		require.NoError(t, repo.Upsert(t.Context(), &models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "Existing", SourceName: "Original source"}))
		before := previewDatabaseState(t, db)
		require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TEMP TRIGGER reject_genre_translation BEFORE INSERT ON genre_translations
WHEN NEW.genre_id = %d AND NEW.language = 'fr' BEGIN SELECT RAISE(ABORT, 'genre translation rejected'); END`, genre.ID)).Error)
		t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_genre_translation").Error })

		incoming := models.GenreTranslation{GenreID: genre.ID, Language: "fr", Name: "New language", SourceName: "New source"}
		err = repo.Upsert(t.Context(), &incoming)
		require.ErrorContains(t, err, fmt.Sprintf("create genre translation %d/fr", genre.ID))
		requireTriggerFailure(t, err, "genre translation rejected")
		require.Equal(t, before, previewDatabaseState(t, db))
		require.NoError(t, db.Exec("DROP TRIGGER reject_genre_translation").Error)
		require.NoError(t, repo.Upsert(t.Context(), &incoming))
		stored, err := repo.FindByGenreAndLanguage(t.Context(), genre.ID, "fr")
		require.NoError(t, err)
		require.Equal(t, incoming.Name, stored.Name)
		translations, err := repo.FindAllByGenre(t.Context(), genre.ID)
		require.NoError(t, err)
		require.Len(t, translations, 2)
	})
}

func TestNonJournalMetadataWriteFailurePreservesEntireOperation(t *testing.T) {
	db := newRollbackContractDB(t)
	repo := NewBatchFileOperationRepository(db)
	job := models.Job{ID: "metadata-failure", Status: models.JobStatusRunning, Files: "[]", StartedAt: time.Now().UTC()}
	require.NoError(t, NewJobRepository(db).Create(t.Context(), &job))
	op := models.BatchFileOperation{
		BatchJobID: job.ID, MovieID: "ORIGINAL", OriginalPath: "/source/original.mp4", NewPath: "/library/original.mp4",
		OperationType: models.OperationTypeMove, NFOSnapshot: "<movie>original</movie>", NFOPath: "/library/original.nfo",
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/library"}}),
		RevertStatus:   models.RevertStatusApplied, OriginalDirPath: "/source",
	}
	require.NoError(t, repo.Create(t.Context(), &op))
	original, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	before := previewDatabaseState(t, db)
	require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TEMP TRIGGER reject_operation_metadata BEFORE UPDATE OF new_path ON batch_file_operations
WHEN OLD.id = %d BEGIN SELECT RAISE(ABORT, 'operation metadata rejected'); END`, op.ID)).Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_operation_metadata").Error })

	incoming := *original
	incoming.MovieID = "CHANGED"
	incoming.NewPath = "/library/changed.mp4"
	incoming.NFOSnapshot = "<movie>changed</movie>"
	incoming.NFOPath = "/library/changed.nfo"
	incoming.InPlaceRenamed = true
	incoming.RevertStatus = models.RevertStatusFailed
	incoming.GeneratedFiles = `{"roots":["/stale-journal"]}`
	incoming.CreatedAt = original.CreatedAt.Add(-time.Hour)
	err = repo.UpdateNonJournalFields(t.Context(), &incoming)
	require.ErrorContains(t, err, "update non-journal fields")
	requireTriggerFailure(t, err, "operation metadata rejected")
	require.Equal(t, before, previewDatabaseState(t, db), "journal, status, metadata and both timestamps must survive the rejected UPDATE")

	require.NoError(t, db.Exec("DROP TRIGGER reject_operation_metadata").Error)
	require.NoError(t, repo.UpdateNonJournalFields(t.Context(), &incoming), "failed BEGIN IMMEDIATE work must release its writer lock")
	stored, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, incoming.MovieID, stored.MovieID)
	require.Equal(t, incoming.NewPath, stored.NewPath)
	require.Equal(t, incoming.NFOSnapshot, stored.NFOSnapshot)
	require.Equal(t, incoming.NFOPath, stored.NFOPath)
	require.True(t, stored.InPlaceRenamed)
	require.Equal(t, incoming.RevertStatus, stored.RevertStatus)
	require.Equal(t, incoming.CreatedAt, stored.CreatedAt)
	require.Equal(t, original.GeneratedFiles, stored.GeneratedFiles, "even a successful metadata update must not overwrite the journal")
}

func TestMissingOperationUnderLiveJobIsNotResurrected(t *testing.T) {
	db := newRollbackContractDB(t)
	job := models.Job{ID: "missing-operation", Status: models.JobStatusRunning, Files: "[]", StartedAt: time.Now().UTC()}
	require.NoError(t, NewJobRepository(db).Create(t.Context(), &job))
	before := previewDatabaseState(t, db)
	repo := NewBatchFileOperationRepository(db)
	err := repo.Update(t.Context(), &models.BatchFileOperation{ID: 730031, BatchJobID: job.ID, NewPath: "/must-not-exist", RevertStatus: models.RevertStatusApplied})
	require.ErrorIs(t, err, ErrNotFound)
	require.NotErrorIs(t, err, ErrJobPruning)
	require.Equal(t, before, previewDatabaseState(t, db))
	stored, err := repo.FindByID(t.Context(), 730031)
	require.ErrorIs(t, err, ErrNotFound)
	require.Nil(t, stored)
}
