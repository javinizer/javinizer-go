package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedArtifactPublicationMovie(t *testing.T, db *DB, contentID string, generation int64, dirty bool) {
	t.Helper()
	require.NoError(t, db.Create(&models.Movie{
		ContentID:        contentID,
		ID:               contentID,
		Title:            "Artifact fence movie",
		RenderGeneration: generation,
		RenderDirty:      dirty,
	}).Error)
}

func loadArtifactPublicationMovie(t *testing.T, db *DB, contentID string) models.Movie {
	t.Helper()
	var movie models.Movie
	require.NoError(t, db.First(&movie, "content_id = ?", contentID).Error)
	return movie
}

func artifactPublicationFencer(t *testing.T, db *DB) ApplyArtifactPublicationFencer {
	t.Helper()
	fencer, ok := interface{}(NewMovieRepository(db)).(ApplyArtifactPublicationFencer)
	require.True(t, ok)
	return fencer
}

func TestApplyArtifactPublicationFence_StaleGenerationDoesNotCallback(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "artifact-stale"
	seedArtifactPublicationMovie(t, db, contentID, 8, true)
	called := false
	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(context.Background(), contentID, 7, func(*models.Movie) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, ErrApplyPublicationStale)
	assert.False(t, called)
}

func TestApplyArtifactPublicationFence_OpenCollisionBlocksCallback(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "artifact-collision"
	seedArtifactPublicationMovie(t, db, contentID, 3, true)
	require.NoError(t, db.Create(&models.CreditCollision{
		CreditID:       1,
		MovieContentID: contentID,
		Field:          models.CreditFieldCreditedName,
		Status:         models.CollisionStatusOpen,
		ReportedValue:  "reported",
		CanonicalValue: "canonical",
	}).Error)
	called := false
	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(context.Background(), contentID, 3, func(*models.Movie) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked)
	assert.False(t, called)
}

func TestApplyArtifactPublicationFence_SuccessClearsDirtyRetainsGeneration(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "artifact-success"
	seedArtifactPublicationMovie(t, db, contentID, 11, true)
	called := false
	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(context.Background(), contentID, 11, func(movie *models.Movie) error {
		called = true
		assert.Equal(t, int64(11), movie.RenderGeneration)
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	persisted := loadArtifactPublicationMovie(t, db, contentID)
	assert.False(t, persisted.RenderDirty)
	assert.Equal(t, int64(11), persisted.RenderGeneration)
}

func TestApplyArtifactPublicationFence_CallbackErrorLeavesDirty(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "artifact-callback-error"
	seedArtifactPublicationMovie(t, db, contentID, 5, true)
	callbackErr := errors.New("publication failed")
	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(context.Background(), contentID, 5, func(*models.Movie) error {
		return callbackErr
	})
	require.ErrorIs(t, err, callbackErr)
	persisted := loadArtifactPublicationMovie(t, db, contentID)
	assert.True(t, persisted.RenderDirty)
	assert.Equal(t, int64(5), persisted.RenderGeneration)
}

func TestApplyArtifactPublicationFence_CancelledContextReleasesWriter(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "artifact-cancelled"
	seedArtifactPublicationMovie(t, db, contentID, 6, true)
	ctx, cancel := context.WithCancel(context.Background())
	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(ctx, contentID, 6, func(*models.Movie) error {
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, loadArtifactPublicationMovie(t, db, contentID).RenderDirty)
	require.NoError(t, artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(context.Background(), contentID, 6, func(*models.Movie) error {
		return nil
	}))
	assert.False(t, loadArtifactPublicationMovie(t, db, contentID).RenderDirty)
}

func TestApplyArtifactPublicationFence_SameGenerationRerenderSucceeds(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "artifact-rerender"
	seedArtifactPublicationMovie(t, db, contentID, 9, true)
	calls := 0
	fencer := artifactPublicationFencer(t, db)
	publish := func(*models.Movie) error {
		calls++
		return nil
	}
	require.NoError(t, fencer.WithApplyArtifactPublicationFence(context.Background(), contentID, 9, publish))
	require.NoError(t, fencer.WithApplyArtifactPublicationFence(context.Background(), contentID, 9, publish))
	assert.Equal(t, 2, calls)
	assert.False(t, loadArtifactPublicationMovie(t, db, contentID).RenderDirty)
}

func TestApplyArtifactPublicationFence_CleanAdmissionStaysDirtyOnFailure(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "artifact-clean-admission"
	seedArtifactPublicationMovie(t, db, contentID, 10, false)
	callbackErr := errors.New("partial publication")
	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(context.Background(), contentID, 10, func(*models.Movie) error {
		return callbackErr
	})
	require.ErrorIs(t, err, callbackErr)
	assert.True(t, loadArtifactPublicationMovie(t, db, contentID).RenderDirty)
}

func TestApplyPublicationFence_NeverClearsArtifactDirty(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const contentID = "result-fence-dirty"
	seedArtifactPublicationMovie(t, db, contentID, 12, true)
	repo := NewMovieRepository(db)
	fencer, ok := interface{}(repo).(ApplyPublicationFencer)
	require.True(t, ok)
	require.NoError(t, fencer.WithApplyPublicationFence(context.Background(), contentID, 12, func(movie *models.Movie) error {
		assert.True(t, movie.RenderDirty)
		return nil
	}))
	persisted := loadArtifactPublicationMovie(t, db, contentID)
	assert.True(t, persisted.RenderDirty)
	assert.Equal(t, int64(12), persisted.RenderGeneration)
}
