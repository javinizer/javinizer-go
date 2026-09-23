package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressDeleteInvalidatesEveryCreditingMovieBeforeCleanup(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	actress := models.Actress{FirstName: "Delete", LastName: "Identity", Verified: true, Origin: ActressOriginUser}
	other := models.Actress{FirstName: "Unaffected", LastName: "Identity", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Create(&other).Error)

	for _, id := range []string{"delete-visible", "delete-suppressed", "delete-unaffected"} {
		require.NoError(t, db.Create(&models.Movie{ContentID: id, ID: id, RenderGeneration: 7}).Error)
	}
	credits := []models.MovieCredit{
		{MovieContentID: "delete-visible", ActressID: actress.ID, Origin: string(models.CreditOriginUser)},
		{MovieContentID: "delete-suppressed", ActressID: actress.ID, Origin: string(models.CreditOriginUser), Suppressed: true},
		{MovieContentID: "delete-unaffected", ActressID: other.ID, Origin: string(models.CreditOriginUser)},
	}
	for i := range credits {
		require.NoError(t, db.Create(&credits[i]).Error)
	}
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "delete-visible", actress.ID).Error)

	require.NoError(t, repos.ActressRepo.Delete(context.Background(), actress.ID))

	for _, id := range []string{"delete-visible", "delete-suppressed"} {
		var movie models.Movie
		require.NoError(t, db.First(&movie, "content_id = ?", id).Error)
		require.True(t, movie.RenderDirty)
		require.Equal(t, int64(8), movie.RenderGeneration)
		called := false
		err := NewMovieRepository(db).WithApplyPublicationFence(context.Background(), id, 7, func(*models.Movie) error {
			called = true
			return nil
		})
		require.ErrorIs(t, err, ErrApplyPublicationStale)
		require.False(t, called)
	}
	var unaffected models.Movie
	require.NoError(t, db.First(&unaffected, "content_id = ?", "delete-unaffected").Error)
	require.False(t, unaffected.RenderDirty)
	require.Equal(t, int64(7), unaffected.RenderGeneration)

	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", actress.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("actress_id = ?", actress.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Table("movie_actresses").Where("actress_id = ?", actress.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestActressDeleteRollsBackDirtyAndDeleteFailures(t *testing.T) {
	for _, failure := range []string{"dirty", "projection", "delete"} {
		t.Run(failure, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			actress := models.Actress{FirstName: "Rollback", LastName: failure, Verified: true, Origin: ActressOriginUser}
			target := models.Actress{FirstName: "Target", LastName: failure, Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			require.NoError(t, db.Create(&target).Error)
			movie := models.Movie{ContentID: "delete-rollback-" + failure, ID: "delete-rollback-" + failure, RenderGeneration: 4}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)
			reassignment := models.MovieCreditReassignment{SourceActressID: actress.ID, TargetActressID: target.ID}
			require.NoError(t, db.Create(&reassignment).Error)
			collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}
			require.NoError(t, db.Create(&collision).Error)

			switch failure {
			case "dirty":
				require.NoError(t, db.Exec("CREATE TRIGGER fail_delete_dirty BEFORE UPDATE OF render_dirty ON movies BEGIN SELECT RAISE(ABORT, 'injected dirty failure'); END").Error)
			case "projection":
				require.NoError(t, db.Exec("CREATE TRIGGER fail_delete_projection BEFORE DELETE ON movie_actresses BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END").Error)
			default:
				injectDatabaseCallbackError(t, db, "delete", "actresses", 1)
			}
			require.Error(t, repo.Delete(context.Background(), actress.ID))

			var stored models.Movie
			require.NoError(t, db.First(&stored, "content_id = ?", movie.ContentID).Error)
			require.False(t, stored.RenderDirty)
			require.Equal(t, int64(4), stored.RenderGeneration)
			for table, where := range map[string]string{
				"actresses": "id = ?", "movie_credits": "actress_id = ?", "movie_actresses": "actress_id = ?",
				"movie_credit_reassignments": "source_actress_id = ?", "credit_collisions": "credit_id = ?",
			} {
				value := any(actress.ID)
				if table == "credit_collisions" {
					value = credit.ID
				}
				var count int64
				require.NoError(t, db.Table(table).Where(where, value).Count(&count).Error)
				require.Equal(t, int64(1), count, table)
			}
		})
	}
}

func TestActressMergeRetargetsStoredSourceCanonicalRepresentations(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", LastName: "Person", JapaneseName: "対象", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", LastName: "Person", JapaneseName: "移動元", Aliases: "Inline Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)

	stored := []models.ActressAlias{
		{AliasName: "Stored Japanese", CanonicalName: "移動元"},
		{AliasName: "Stored English Last First", CanonicalName: "Person Source"},
		{AliasName: "Stored English First Last", CanonicalName: "Source Person"},
		{AliasName: "Target Existing", CanonicalName: "対象"},
		{AliasName: "Unrelated", CanonicalName: "Someone Else"},
		{AliasName: "移動元", CanonicalName: "Other Person"},
	}
	for i := range stored {
		require.NoError(t, db.Create(&stored[i]).Error)
	}

	_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"dmm_id": "target"})
	require.NoError(t, err)

	for _, aliasName := range []string{"Stored Japanese", "Stored English Last First", "Stored English First Last", "Inline Source"} {
		var alias models.ActressAlias
		require.NoError(t, db.First(&alias, "alias_name = ?", aliasName).Error)
		require.Equal(t, "対象", alias.CanonicalName, aliasName)
		found, err := repo.FindVerifiedByAlias(context.Background(), aliasName)
		require.NoError(t, err, aliasName)
		require.Equal(t, target.ID, found.ID, aliasName)
	}
	for aliasName, canonical := range map[string]string{"Target Existing": "対象", "Unrelated": "Someone Else", "移動元": "Other Person"} {
		var alias models.ActressAlias
		require.NoError(t, db.First(&alias, "alias_name = ?", aliasName).Error)
		require.Equal(t, canonical, alias.CanonicalName)
	}
	var actresses int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
	require.Equal(t, int64(1), actresses)

	scraped := creditMovie("merge-alias-followup", []models.MovieCredit{{CreditedName: "Stored Japanese", Scraped: models.Actress{JapaneseName: "Stored Japanese"}}})
	saved, err := db.Repositories().MovieRepo.Upsert(context.Background(), scraped)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	require.Equal(t, target.ID, saved.Credits[0].ActressID)
	require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
	require.Equal(t, int64(1), actresses)
}

func TestPromoteCandidatePreservesEveryChangedCanonicalRepresentation(t *testing.T) {
	tests := []struct {
		name                                    string
		beforeFirst, beforeLast, beforeJapanese string
		afterFirst, afterLast, afterJapanese    string
		aliases                                 []string
	}{
		{"english changed", "Old", "English", "同じ", "New", "English", "同じ", []string{"English Old", "Old English"}},
		{"japanese changed", "Same", "English", "旧名", "Same", "English", "新名", []string{"旧名"}},
		{"both changed", "Old", "Name", "旧名", "New", "Name", "新名", []string{"旧名", "Name Old", "Old Name"}},
		{"unchanged", "Same", "Name", "同じ", "Same", "Name", "同じ", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			candidate := models.Actress{FirstName: tc.beforeFirst, LastName: tc.beforeLast, JapaneseName: tc.beforeJapanese, Origin: ActressOriginScrape}
			require.NoError(t, db.Create(&candidate).Error)
			require.NoError(t, repo.PromoteCandidate(context.Background(), candidate.ID, tc.afterFirst, tc.afterLast, tc.afterJapanese, ""))

			var count int64
			require.NoError(t, db.Model(&models.ActressAlias{}).Count(&count).Error)
			require.Equal(t, int64(len(tc.aliases)), count)
			for _, aliasName := range tc.aliases {
				found, err := repo.FindVerifiedByAlias(context.Background(), aliasName)
				require.NoError(t, err, aliasName)
				require.Equal(t, candidate.ID, found.ID)
			}
		})
	}
}

func TestPromoteCandidateAliasFailureRollsBackIdentityAndProjection(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{FirstName: "Old", LastName: "Candidate", JapaneseName: "旧名", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "promote-alias-rollback", ID: "promote-alias-rollback", RenderGeneration: 12}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, Origin: string(models.CreditOriginScrape)}).Error)
	injectDatabaseCallbackError(t, db, "create", "actress_aliases", 1)

	err := repo.PromoteCandidate(context.Background(), candidate.ID, "New", "Candidate", "新名", "new-thumb")
	require.Error(t, err)

	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.False(t, stored.Verified)
	require.Equal(t, ActressOriginScrape, stored.Origin)
	require.Equal(t, "Old", stored.FirstName)
	require.Equal(t, "旧名", stored.JapaneseName)
	require.Empty(t, stored.ThumbURL)
	var storedMovie models.Movie
	require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
	require.False(t, storedMovie.RenderDirty)
	require.Equal(t, int64(12), storedMovie.RenderGeneration)
	var aliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
	require.Zero(t, aliases)
}

func TestActressMergeFailuresRollBackIdentityGraph(t *testing.T) {
	for _, failure := range []string{"alias write", "source delete"} {
		t.Run(failure, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			target := models.Actress{FirstName: "Target", LastName: "Rollback", JapaneseName: "対象", Verified: true, Origin: ActressOriginUser}
			source := models.Actress{FirstName: "Source", LastName: "Rollback", JapaneseName: "移動元", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			movie := models.Movie{ContentID: "merge-rollback-" + failure, ID: "merge-rollback-" + failure, RenderGeneration: 5}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, source.ID).Error)
			collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}
			require.NoError(t, db.Create(&collision).Error)
			storedAlias := models.ActressAlias{AliasName: "Stored Rollback", CanonicalName: source.JapaneseName}
			require.NoError(t, db.Create(&storedAlias).Error)

			if failure == "alias write" {
				injectDatabaseCallbackError(t, db, "create", "actress_aliases", 1)
			} else {
				injectDatabaseCallbackError(t, db, "delete", "actresses", 1)
			}
			_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"dmm_id": "target"})
			require.Error(t, err)

			for _, actress := range []models.Actress{target, source} {
				var stored models.Actress
				require.NoError(t, db.First(&stored, actress.ID).Error)
				require.Equal(t, actress.JapaneseName, stored.JapaneseName)
			}
			var storedCredit models.MovieCredit
			require.NoError(t, db.First(&storedCredit, credit.ID).Error)
			require.Equal(t, source.ID, storedCredit.ActressID)
			var storedCollision models.CreditCollision
			require.NoError(t, db.First(&storedCollision, collision.ID).Error)
			require.Equal(t, credit.ID, storedCollision.CreditID)
			require.NoError(t, db.First(&storedAlias, storedAlias.ID).Error)
			require.Equal(t, source.JapaneseName, storedAlias.CanonicalName)
			var sourceAssociations int64
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, source.ID).Count(&sourceAssociations).Error)
			require.Equal(t, int64(1), sourceAssociations)
			var storedMovie models.Movie
			require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
			require.False(t, storedMovie.RenderDirty)
			require.Equal(t, int64(5), storedMovie.RenderGeneration)
		})
	}
}

func TestActressMergeTargetAliasRetargetFailureRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", LastName: "Retarget", JapaneseName: "対象旧", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", LastName: "Retarget", JapaneseName: "対象新", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	alias := models.ActressAlias{AliasName: "Target History", CanonicalName: target.JapaneseName}
	require.NoError(t, db.Create(&alias).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_merge_target_alias BEFORE UPDATE ON actress_aliases WHEN OLD.alias_name = 'Target History' BEGIN SELECT RAISE(ABORT, 'injected target alias failure'); END").Error)

	_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"dmm_id": "target", "japanese_name": "source"})
	require.Error(t, err)
	var storedTarget models.Actress
	require.NoError(t, db.First(&storedTarget, target.ID).Error)
	require.Equal(t, target.JapaneseName, storedTarget.JapaneseName)
	var storedSource models.Actress
	require.NoError(t, db.First(&storedSource, source.ID).Error)
	require.Equal(t, source.JapaneseName, storedSource.JapaneseName)
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, target.JapaneseName, alias.CanonicalName)
}
