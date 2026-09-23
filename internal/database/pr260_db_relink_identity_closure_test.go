package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPR260RestoreJapaneseAndAliasIdentity(t *testing.T) {
	for _, mode := range []string{"japanese", "alias", "alias-query-fault"} {
		t.Run(mode, func(t *testing.T) {
			db, service, credit, collision := collisionFixture(t)
			require.NoError(t, db.Model(&credit).Updates(map[string]interface{}{"credited_name": "", "credited_japanese_name": "日本名"}).Error)
			require.NoError(t, db.Model(&collision).Updates(map[string]interface{}{"reported_value": "日本名", "canonical_value": "Truth Original"}).Error)
			if mode != "japanese" {
				require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Update("japanese_name", "別名").Error)
				require.NoError(t, db.Create(&models.ActressAlias{AliasName: "日本名", CanonicalName: "Original Truth"}).Error)
			} else {
				require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Update("japanese_name", "日本名").Error)
			}
			require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
			before := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
			const hook = "pr260_alias_restore_query_fault"
			if mode == "alias-query-fault" {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
					if tx.Statement.Table == "actress_aliases" {
						tx.AddError(errors.New("alias lookup fault"))
					}
				}))
				t.Cleanup(func() { db.Callback().Query().Remove(hook) })
			}
			err := service.SetCreditSuppressed(context.Background(), credit.ID, false)
			if mode == "alias-query-fault" {
				require.ErrorContains(t, err, "alias lookup fault")
				var stored models.MovieCredit
				require.NoError(t, db.First(&stored, credit.ID).Error)
				require.True(t, stored.Suppressed)
				require.Equal(t, credit.ActressID, stored.ActressID)
				db.Callback().Query().Remove(hook)
				require.Equal(t, before.RenderGeneration, loadArtifactPublicationMovie(t, db, credit.MovieContentID).RenderGeneration)
			} else {
				require.NoError(t, err)
				var stored models.MovieCredit
				require.NoError(t, db.First(&stored, credit.ID).Error)
				require.False(t, stored.Suppressed)
				require.Equal(t, "日本名", stored.CreditedJapaneseName)
				var open int64
				require.NoError(t, db.Model(&models.CreditCollision{}).Where("credit_id = ? AND status = ?", credit.ID, models.CollisionStatusOpen).Count(&open).Error)
				require.Zero(t, open, "Japanese canonical and recorded alias are both valid identities")
			}
		})
	}
}

func TestPR260RelinkMergeFailureRollsBackIdentityAndPins(t *testing.T) {
	db, service, source, collision := collisionFixture(t)
	target := models.Actress{FirstName: "Reported", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	survivor := models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID, Origin: "scrape"}
	require.NoError(t, db.Create(&survivor).Error)
	require.NoError(t, db.Model(&source).Updates(map[string]interface{}{"order_pinned": true, "order_index": 7, "user_override": true, "override_name": "Owner"}).Error)
	require.NoError(t, db.Model(&collision).Update("user_pinned", true).Error)
	before := loadArtifactPublicationMovie(t, db, source.MovieContentID)
	require.NoError(t, db.Exec("CREATE TRIGGER pr260_transfer_fault BEFORE UPDATE OF credit_id ON credit_collisions BEGIN SELECT RAISE(ABORT, 'transfer fault'); END").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_transfer_fault").Error) })
	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
	require.ErrorContains(t, err, "transfer fault")
	var original, kept models.MovieCredit
	require.NoError(t, db.First(&original, source.ID).Error)
	require.NoError(t, db.First(&kept, survivor.ID).Error)
	require.Equal(t, source.ActressID, original.ActressID)
	require.True(t, original.OrderPinned)
	require.Equal(t, 7, original.OrderIndex)
	require.True(t, original.UserOverride)
	require.False(t, kept.OrderPinned)
	require.False(t, kept.UserOverride)
	var pinned models.CreditCollision
	require.NoError(t, db.First(&pinned, collision.ID).Error)
	require.Equal(t, source.ID, pinned.CreditID)
	require.True(t, pinned.UserPinned)
	require.Equal(t, models.CollisionStatusOpen, pinned.Status)
	after := loadArtifactPublicationMovie(t, db, source.MovieContentID)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
	require.Equal(t, before.RenderDirty, after.RenderDirty)
}
