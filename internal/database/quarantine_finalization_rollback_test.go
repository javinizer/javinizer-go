package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func failNextQuarantineRecompute(t *testing.T, db *DB) {
	t.Helper()
	name := fmt.Sprintf("fail-quarantine-recompute-%p", t)
	fired := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if fired || tx.Statement == nil || tx.Statement.Table != "actresses" {
			return
		}
		sql := tx.Statement.SQL.String()
		if strings.Contains(sql, "verified = ?") && strings.Contains(sql, "ORDER BY id") && len(tx.Statement.Vars) > 0 && tx.Statement.Vars[0] == false {
			fired = true
			_ = tx.AddError(errors.New("quarantine recompute fault"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
}

func failNextResolvedActressReload(t *testing.T, db *DB) {
	t.Helper()
	name := fmt.Sprintf("fail-resolved-reload-%p", t)
	fired := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if fired || tx.Statement == nil || tx.Statement.Table != "actresses" {
			return
		}
		sql := tx.Statement.SQL.String()
		if strings.Contains(sql, "WHERE `actresses`.`id` = ?") {
			fired = true
			_ = tx.AddError(errors.New("resolved reload fault"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
}

func TestMovieUpsertFinalMaintenanceFailuresRollBack(t *testing.T) {
	t.Run("render invalidation", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewMovieRepository(db)
		movie := &models.Movie{ContentID: "final-render-rollback", ID: "FINAL-RENDER-ROLLBACK", Title: "Before"}
		_, err := repo.Upsert(context.Background(), movie)
		require.NoError(t, err)

		movie.Title = "After"
		injectDatabaseCallbackError(t, db, "query", "movies", 4)
		_, err = repo.Upsert(context.Background(), movie)
		require.Error(t, err)

		var stored models.Movie
		require.NoError(t, db.First(&stored, "content_id = ?", movie.ContentID).Error)
		require.Equal(t, "Before", stored.Title)
	})

	t.Run("quarantine recompute", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewMovieRepository(db)
		movie := &models.Movie{ContentID: "final-quarantine-rollback", ID: "FINAL-QUARANTINE-ROLLBACK", Title: "Before"}
		_, err := repo.Upsert(context.Background(), movie)
		require.NoError(t, err)

		movie.Title = "After"
		failNextQuarantineRecompute(t, db)
		_, err = repo.Upsert(context.Background(), movie)
		require.ErrorContains(t, err, "quarantine recompute fault")

		var stored models.Movie
		require.NoError(t, db.First(&stored, "content_id = ?", movie.ContentID).Error)
		require.Equal(t, "Before", stored.Title)
	})
	t.Run("credit reload", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewMovieRepository(db)
		movie := &models.Movie{ContentID: "final-credit-reload-rollback", ID: "FINAL-CREDIT-RELOAD-ROLLBACK", Title: "Before"}
		_, err := repo.Upsert(context.Background(), movie)
		require.NoError(t, err)

		movie.Title = "After"
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 5)
		_, err = repo.Upsert(context.Background(), movie)
		require.Error(t, err)

		var stored models.Movie
		require.NoError(t, db.First(&stored, "content_id = ?", movie.ContentID).Error)
		require.Equal(t, "Before", stored.Title)
	})
}

func TestActressMutationsRollBackWhenQuarantineFinalizationFails(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		actress := models.Actress{FirstName: "Before", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&actress).Error)
		actress.FirstName = "After"
		failNextQuarantineRecompute(t, db)
		require.ErrorContains(t, repo.Update(context.Background(), &actress), "quarantine recompute fault")
		var stored models.Actress
		require.NoError(t, db.First(&stored, actress.ID).Error)
		require.Equal(t, "Before", stored.FirstName)
	})

	t.Run("rename", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		actress := models.Actress{JapaneseName: "Before", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&actress).Error)
		failNextQuarantineRecompute(t, db)
		require.ErrorContains(t, repo.RenameNameFields(context.Background(), actress.ID, "", "", "After"), "quarantine recompute fault")
		var stored models.Actress
		require.NoError(t, db.First(&stored, actress.ID).Error)
		require.Equal(t, "Before", stored.JapaneseName)
	})

	t.Run("delete", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		actress := models.Actress{FirstName: "Keep", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&actress).Error)
		failNextQuarantineRecompute(t, db)
		require.ErrorContains(t, repo.Delete(context.Background(), actress.ID), "quarantine recompute fault")
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", actress.ID).Count(&count).Error)
		require.EqualValues(t, 1, count)
	})

	t.Run("merge", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
		source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&source).Error)
		failNextQuarantineRecompute(t, db)
		_, err := repo.merger.Merge(context.Background(), target.ID, source.ID, nil, db)
		require.ErrorContains(t, err, "quarantine recompute fault")
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Where("id IN ?", []uint{target.ID, source.ID}).Count(&count).Error)
		require.EqualValues(t, 2, count)
	})
}

func TestResolutionAndCollisionRollBackWhenQuarantineFinalizationFails(t *testing.T) {
	t.Run("resolution", func(t *testing.T) {
		db := newCreditTestDB(t)
		failNextQuarantineRecompute(t, db)
		_, _, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 820001, JapaneseName: "Rollback Resolution"})
		require.ErrorContains(t, err, "quarantine recompute fault")
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Where("dmm_id = ?", 820001).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("service resolution", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		failNextQuarantineRecompute(t, db)
		_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionKeepIdentity, 0)
		require.ErrorContains(t, err, "quarantine recompute fault")
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, collision.Status)
	})
	t.Run("finalized reload", func(t *testing.T) {
		db := newCreditTestDB(t)
		failNextResolvedActressReload(t, db)
		_, _, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 820002, JapaneseName: "Rollback Reload"})
		require.ErrorContains(t, err, "resolved reload fault")
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Where("dmm_id = ?", 820002).Count(&count).Error)
		require.Zero(t, count)
	})

}

func TestCollisionTransactionBoundariesAndFaults(t *testing.T) {
	t.Run("resolve and close finalize", func(t *testing.T) {
		db, service, credit, collision := collisionFixture(t)
		require.NoError(t, service.Collisions.ResolveTx(db.DB, collision.ID, models.CollisionResolutionKeepIdentity))
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusResolved, collision.Status)
		require.NoError(t, db.Model(&collision).Updates(map[string]interface{}{colStatus: models.CollisionStatusOpen, colResolution: ""}).Error)
		require.NoError(t, service.Collisions.CloseByCreditTx(db.DB, credit.ID, models.CollisionResolutionByRemoval))
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusResolved, collision.Status)
	})

	t.Run("reopen update failure rolls back", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		require.NoError(t, db.Model(&collision).Updates(map[string]interface{}{colStatus: models.CollisionStatusResolved, colResolution: models.CollisionResolutionKeepIdentity}).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_collision_reopen BEFORE UPDATE ON credit_collisions BEGIN SELECT RAISE(ABORT, 'reopen fault'); END").Error)
		t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS fail_collision_reopen").Error })
		require.ErrorContains(t, service.Collisions.Reopen(context.Background(), collision.ID), "reopen fault")
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusResolved, collision.Status)
		require.False(t, collision.UserPinned)
	})

	t.Run("transfer update failure rolls back", func(t *testing.T) {
		db, service, credit, collision := collisionFixture(t)
		target := models.MovieCredit{MovieContentID: credit.MovieContentID, ActressID: credit.ActressID + 1000}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_collision_transfer BEFORE UPDATE ON credit_collisions BEGIN SELECT RAISE(ABORT, 'transfer fault'); END").Error)
		t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS fail_collision_transfer").Error })
		require.ErrorContains(t, service.Collisions.TransferTx(db.DB, credit.ID, target.ID, target.MovieContentID), "transfer fault")
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, credit.ID, collision.CreditID)
	})
}

func TestCreditRenderMutationFailureKeepsStoredState(t *testing.T) {
	db, service, credit, _ := collisionFixture(t)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_credit_override BEFORE UPDATE ON movie_credits BEGIN SELECT RAISE(ABORT, 'override fault'); END").Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS fail_credit_override").Error })
	require.ErrorContains(t, service.Credits.UpdateOverride(context.Background(), credit.ID, "Changed", true), "override fault")
	var stored models.MovieCredit
	require.NoError(t, db.First(&stored, credit.ID).Error)
	require.Empty(t, stored.OverrideName)
	require.False(t, stored.UserOverride)
}

func TestPromotionProjectionRestoreFailureRollsBackIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{FirstName: "Candidate", Origin: ActressOriginScrape}
	movie := models.Movie{ContentID: "promote-restore-rollback", ID: "PROMOTE-RESTORE-ROLLBACK"}
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_projection_restore BEFORE INSERT ON movie_actresses BEGIN SELECT RAISE(ABORT, 'restore fault'); END").Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS fail_projection_restore").Error })
	require.ErrorContains(t, repo.PromoteCandidate(context.Background(), candidate.ID, "Promoted", "Identity", "", ""), "restore fault")
	require.NoError(t, db.First(&candidate, candidate.ID).Error)
	require.False(t, candidate.Verified)
	require.Equal(t, "Candidate", candidate.FirstName)
	var associations int64
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, candidate.ID).Count(&associations).Error)
	require.Zero(t, associations)
}
