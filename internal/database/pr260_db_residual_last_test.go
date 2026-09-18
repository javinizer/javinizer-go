package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPR260ResidualArtifactSecondCollisionReadFailure(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "residual-count", 3, true)
	const hook = "pr260_residual_second_count"
	reads := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table != "credit_collisions" {
			return
		}
		reads++
		if reads == 2 {
			tx.AddError(errors.New("second collision count fault"))
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove(hook) })
	called := false
	err := NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), "residual-count", 3, func(*models.Movie) error { called = true; return nil })
	require.ErrorContains(t, err, "second collision count fault")
	require.Equal(t, 2, reads)
	require.False(t, called)
	saved := loadArtifactPublicationMovie(t, db, "residual-count")
	require.True(t, saved.RenderDirty)
	require.EqualValues(t, 3, saved.RenderGeneration)
}

func TestPR260ResidualRestoreNewCollisionInsertRollsBack(t *testing.T) {
	db, service, credit, existing := collisionFixture(t)
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Update("thumb_url", "https://example.com/old.jpg").Error)
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	before := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	require.NoError(t, db.Exec("CREATE TRIGGER pr260_residual_insert BEFORE INSERT ON credit_collisions BEGIN SELECT RAISE(ABORT, 'new collision insert fault'); END").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_residual_insert").Error) })
	err := service.SetCreditSuppressed(context.Background(), credit.ID, false)
	require.ErrorContains(t, err, "new collision insert fault")
	var saved models.MovieCredit
	require.NoError(t, db.First(&saved, credit.ID).Error)
	require.True(t, saved.Suppressed)
	require.Equal(t, credit.ActressID, saved.ActressID)
	var collision models.CreditCollision
	require.NoError(t, db.First(&collision, existing.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, collision.Status)
	require.Equal(t, models.CollisionResolutionBySuppression, collision.Resolution)
	var count int64
	require.NoError(t, db.Model(&models.CreditCollision{}).Where("credit_id = ?", credit.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	after := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
	require.Equal(t, before.RenderDirty, after.RenderDirty)
}

func TestPR260ResidualArtifactCancellationAfterCollisionCheck(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "residual-cancel", 5, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const hook = "pr260_residual_cancel_after_count"
	checks := 0
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table != "credit_collisions" || tx.Error != nil {
			return
		}
		checks++
		if checks == 2 {
			cancel()
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove(hook) })
	called := false
	err := NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, "residual-cancel", 5, func(*models.Movie) error { called = true; return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 2, checks)
	require.False(t, called)
	saved := loadArtifactPublicationMovie(t, db, "residual-cancel")
	require.True(t, saved.RenderDirty)
	require.EqualValues(t, 5, saved.RenderGeneration)
}

func TestPR260ResidualJapaneseIdentityRestoresPinnedCollision(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Update("verified", false).Error)
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).Updates(map[string]interface{}{"credited_name": "", "credited_japanese_name": "日本語名"}).Error)
	require.NoError(t, db.Model(&models.CreditCollision{}).Where("id = ?", collision.ID).Updates(map[string]interface{}{"field": models.CreditFieldIdentityLink, "reported_value": "日本語名", "user_pinned": true}).Error)
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, false))
	var saved models.MovieCredit
	require.NoError(t, db.First(&saved, credit.ID).Error)
	require.False(t, saved.Suppressed)
	require.Equal(t, credit.ActressID, saved.ActressID)
	require.Equal(t, "日本語名", saved.CreditedJapaneseName)
	var restored models.CreditCollision
	require.NoError(t, db.First(&restored, collision.ID).Error)
	require.True(t, restored.UserPinned)
	require.Equal(t, models.CollisionStatusOpen, restored.Status)
	require.Equal(t, models.CreditFieldIdentityLink, restored.Field)
	require.Equal(t, "日本語名", restored.ReportedValue)
}

func TestPR260ResidualRelinkProjectionFailureRollsBackMerge(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	survivor := models.MovieCredit{MovieContentID: credit.MovieContentID, ActressID: target.ID, Origin: "scrape"}
	require.NoError(t, db.Create(&survivor).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", credit.MovieContentID, credit.ActressID).Error)
	require.NoError(t, db.Model(&models.CreditCollision{}).Where("id = ?", collision.ID).Update("user_pinned", true).Error)
	before := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	require.NoError(t, db.Exec("CREATE TRIGGER pr260_residual_projection BEFORE DELETE ON movie_actresses BEGIN SELECT RAISE(ABORT, 'projection delete fault'); END").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_residual_projection").Error) })
	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
	require.ErrorContains(t, err, "projection delete fault")
	var original, savedTarget models.MovieCredit
	require.NoError(t, db.First(&original, credit.ID).Error)
	require.NoError(t, db.First(&savedTarget, survivor.ID).Error)
	require.Equal(t, credit.ActressID, original.ActressID)
	require.Equal(t, target.ID, savedTarget.ActressID)
	var stored models.CreditCollision
	require.NoError(t, db.First(&stored, collision.ID).Error)
	require.Equal(t, credit.ID, stored.CreditID)
	require.True(t, stored.UserPinned)
	require.Equal(t, models.CollisionStatusOpen, stored.Status)
	var projection []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", credit.MovieContentID).Pluck("actress_id", &projection).Error)
	require.Equal(t, []uint{credit.ActressID}, projection)
	after := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
	require.Equal(t, before.RenderDirty, after.RenderDirty)
}
