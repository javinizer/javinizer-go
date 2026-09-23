package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func renameIdentityFixture(t *testing.T) (*DB, *ActressRepository, models.Actress, models.Movie, models.ActressAlias, models.CreditCollision) {
	t.Helper()
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Truth", LastName: "Original", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "rename-identity", ID: "rename-identity", RenderGeneration: 7}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Person Reported"}
	require.NoError(t, db.Create(&credit).Error)
	alias := models.ActressAlias{AliasName: "Adopted Alias", CanonicalName: actress.FullName()}
	require.NoError(t, db.Create(&alias).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Renamed Person", CanonicalValue: actress.FullName(), Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)
	return db, NewActressRepository(db), actress, movie, alias, collision
}

func TestPR260ReviewRenameRetargetsIdentityStateTransactionally(t *testing.T) {
	db, repo, actress, movie, alias, collision := renameIdentityFixture(t)
	unrelated := models.ActressAlias{AliasName: "Other Alias", CanonicalName: "Different Person"}
	require.NoError(t, db.Create(&unrelated).Error)

	require.NoError(t, repo.RenameNameFields(context.Background(), actress.ID, "Person", "Renamed", ""))
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, "Renamed Person", alias.CanonicalName)
	found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: "Alias", LastName: "Adopted"})
	require.NoError(t, err)
	require.Equal(t, ResolutionMatched, outcome)
	require.Equal(t, actress.ID, found.ID)
	var candidates int64
	require.NoError(t, db.Model(&models.Actress{}).Where("verified = ?", false).Count(&candidates).Error)
	require.Zero(t, candidates)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, "Renamed Person", collision.CanonicalValue)
	require.Equal(t, models.CollisionStatusResolved, collision.Status)
	require.NoError(t, db.First(&unrelated, unrelated.ID).Error)
	require.Equal(t, "Different Person", unrelated.CanonicalName)
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
	require.True(t, movie.RenderDirty)
	require.Equal(t, int64(8), movie.RenderGeneration)
}

func TestPR260ReviewRenameRollsBackAliasAndCollisionFailures(t *testing.T) {
	for _, tc := range []struct{ name, operation, table string }{
		{name: "alias retarget", operation: "update", table: "actress_aliases"},
		{name: "collision reconcile", operation: "update", table: "credit_collisions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo, actress, movie, alias, collision := renameIdentityFixture(t)
			injectDatabaseCallbackError(t, db, tc.operation, tc.table, 1)
			err := repo.RenameNameFields(context.Background(), actress.ID, "Person", "Renamed", "")
			require.Error(t, err)
			require.NoError(t, db.First(&actress, actress.ID).Error)
			require.Equal(t, "Truth", actress.FirstName)
			require.Equal(t, "Original", actress.LastName)
			require.NoError(t, db.First(&alias, alias.ID).Error)
			require.Equal(t, "Original Truth", alias.CanonicalName)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, models.CollisionStatusOpen, collision.Status)
			require.Equal(t, "Original Truth", collision.CanonicalValue)
			require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
			require.False(t, movie.RenderDirty)
			require.Equal(t, int64(7), movie.RenderGeneration)
		})
	}
}

func TestPR260ReviewRenameDatabaseFailuresRollback(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		db := newCreditTestDB(t)
		err := NewActressRepository(db).RenameNameFields(context.Background(), 999999, "New", "Name", "")
		require.Error(t, err)
	})

	t.Run("actress update", func(t *testing.T) {
		db, repo, actress, movie, alias, _ := renameIdentityFixture(t)
		injectDatabaseCallbackError(t, db, "update", "actresses", 1)
		err := repo.RenameNameFields(context.Background(), actress.ID, "Person", "Renamed", "")
		require.Error(t, err)
		require.NoError(t, db.First(&actress, actress.ID).Error)
		require.Equal(t, "Original Truth", actress.FullName())
		require.NoError(t, db.First(&alias, alias.ID).Error)
		require.Equal(t, "Original Truth", alias.CanonicalName)
		require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
		require.False(t, movie.RenderDirty)
	})

	t.Run("dirty generation", func(t *testing.T) {
		db, repo, actress, movie, alias, collision := renameIdentityFixture(t)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_rename_dirty BEFORE UPDATE OF render_dirty ON movies BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
		err := repo.RenameNameFields(context.Background(), actress.ID, "Person", "Renamed", "")
		require.Error(t, err)
		require.NoError(t, db.First(&actress, actress.ID).Error)
		require.Equal(t, "Original Truth", actress.FullName())
		require.NoError(t, db.First(&alias, alias.ID).Error)
		require.Equal(t, "Original Truth", alias.CanonicalName)
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, collision.Status)
		require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
		require.False(t, movie.RenderDirty)
		require.Equal(t, int64(7), movie.RenderGeneration)
	})
}
