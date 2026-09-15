package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestLegacyMovieCastCreatesUserCredits(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	movie := &models.Movie{
		ContentID: "legacy-cast-backfill",
		ID:        "LEGACY-CAST-BACKFILL",
		Actresses: []models.Actress{{FirstName: "Legacy"}},
	}
	saved, err := repos.MovieRepo.Upsert(t.Context(), movie)
	require.NoError(t, err)
	require.Len(t, saved.Actresses, 1)
	credits, err := repos.MovieCreditRepo.ListByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.Equal(t, saved.Actresses[0].ID, credits[0].ActressID)
	require.Equal(t, string(models.CreditOriginUser), credits[0].Origin)
	require.True(t, credits[0].OrderPinned)
}

func TestLegacyCastEditReconcilesCredits(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	original := models.Actress{FirstName: "Original"}
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &original))
	movie := creditMovie("review-cast-edit", []models.MovieCredit{{
		CreditedName: "Original",
		Scraped:      original,
	}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(t.Context(), movie, nil, nil)
	require.NoError(t, err)

	saved.Credits = nil
	saved.Actresses = []models.Actress{{FirstName: "Added"}}
	edited, err := repos.MovieRepo.Upsert(t.Context(), saved)
	require.NoError(t, err)
	require.Len(t, edited.Actresses, 1)
	added := edited.Actresses[0]
	require.True(t, added.Verified)
	require.Equal(t, ActressOriginUser, added.Origin)

	credits, err := repos.MovieCreditRepo.ListByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, credits, 2)
	byActress := make(map[uint]models.MovieCredit, len(credits))
	for _, credit := range credits {
		byActress[credit.ActressID] = credit
	}
	require.True(t, byActress[original.ID].Suppressed)
	require.Equal(t, string(models.CreditOriginUser), byActress[original.ID].Origin)
	require.False(t, byActress[added.ID].Suppressed)
	require.Equal(t, string(models.CreditOriginUser), byActress[added.ID].Origin)
	require.True(t, byActress[added.ID].OrderPinned)

	edited.Credits = nil
	edited.Actresses = []models.Actress{original, added}
	readded, err := repos.MovieRepo.Upsert(t.Context(), edited)
	require.NoError(t, err)
	require.Len(t, readded.Actresses, 2)
	restored, err := repos.MovieCreditRepo.FindByMovieAndActress(t.Context(), movie.ContentID, original.ID)
	require.NoError(t, err)
	require.False(t, restored.Suppressed)
	require.Equal(t, string(models.CreditOriginUser), restored.Origin)
}

func legacyCastErrorFixture(t *testing.T, suppressed bool) (*DB, *MovieRepository, models.Movie, models.Actress, models.MovieCredit) {
	t.Helper()
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	movie := models.Movie{ContentID: "legacy-error", ID: "LEGACY-ERROR"}
	actress := models.Actress{FirstName: "Existing", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&actress).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, Origin: string(models.CreditOriginScrape), Suppressed: suppressed}
	require.NoError(t, db.Create(&credit).Error)
	return db, repo, movie, actress, credit
}

func TestLegacyCastEditReconcileErrors(t *testing.T) {
	t.Run("upsert lookup", func(t *testing.T) {
		db, repo, movie, _, _ := legacyCastErrorFixture(t, false)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		_, err := repo.Upsert(t.Context(), &movie)
		require.Error(t, err)
	})
	t.Run("unsuppress", func(t *testing.T) {
		db, repo, movie, actress, _ := legacyCastErrorFixture(t, true)
		movie.Actresses = []models.Actress{actress}
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		err := repo.upserter.reconcileLegacyActressEditsTx(db.DB, &movie)
		require.Error(t, err)
	})
	t.Run("add", func(t *testing.T) {
		db, repo, movie, actress, _ := legacyCastErrorFixture(t, false)
		added := models.Actress{FirstName: "Added", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&added).Error)
		movie.Actresses = []models.Actress{actress, added}
		injectDatabaseCallbackError(t, db, "create", "movie_credits", 1)
		err := repo.upserter.reconcileLegacyActressEditsTx(db.DB, &movie)
		require.Error(t, err)
	})
	t.Run("dirty mark", func(t *testing.T) {
		db, repo, movie, actress, _ := legacyCastErrorFixture(t, false)
		added := models.Actress{FirstName: "Added", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&added).Error)
		movie.Actresses = []models.Actress{actress, added}
		require.NoError(t, db.Exec("CREATE TRIGGER fail_legacy_dirty BEFORE UPDATE OF render_dirty ON movies BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
		err := repo.upserter.reconcileLegacyActressEditsTx(db.DB, &movie)
		require.Error(t, err)
	})
	t.Run("suppress", func(t *testing.T) {
		db, repo, movie, _, _ := legacyCastErrorFixture(t, false)
		injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)
		err := repo.upserter.reconcileLegacyActressEditsTx(db.DB, &movie)
		require.Error(t, err)
	})
}

func TestResolveVerifiedSingleNameIdentity(t *testing.T) {
	for _, actress := range []models.Actress{{FirstName: "Firstonly"}, {LastName: "Lastonly"}} {
		t.Run(actress.FullName(), func(t *testing.T) {
			db := newCreditTestDB(t)
			actress.Verified = true
			actress.Origin = ActressOriginUser
			require.NoError(t, db.Create(&actress).Error)
			found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: actress.FirstName, LastName: actress.LastName})
			require.NoError(t, err)
			require.Equal(t, ResolutionMatched, outcome)
			require.Equal(t, actress.ID, found.ID)
		})
	}
}

func TestResolveVerifiedSingleNameAlias(t *testing.T) {
	for _, test := range []struct {
		actress models.Actress
		scraped models.Actress
		alias   string
	}{
		{actress: models.Actress{FirstName: "Canonical"}, scraped: models.Actress{FirstName: "AliasFirst"}, alias: "AliasFirst"},
		{actress: models.Actress{LastName: "Canonical"}, scraped: models.Actress{LastName: "AliasLast"}, alias: "AliasLast"},
	} {
		t.Run(test.alias, func(t *testing.T) {
			db := newCreditTestDB(t)
			test.actress.Verified = true
			test.actress.Origin = ActressOriginUser
			require.NoError(t, db.Create(&test.actress).Error)
			require.NoError(t, db.Create(&models.ActressAlias{AliasName: test.alias, CanonicalName: test.actress.FullName()}).Error)
			found, outcome, err := ResolveActressIdentityTx(db.DB, &test.scraped)
			require.NoError(t, err)
			require.Equal(t, ResolutionMatched, outcome)
			require.Equal(t, test.actress.ID, found.ID)
		})
	}
}

func TestResolveSingleNameCandidateLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	injectDatabaseCallbackError(t, db, "query", "actresses", 2)
	_, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: "Missing"})
	require.Error(t, err)
	require.Equal(t, ResolutionCandidateLinked, outcome)
}

func TestResolveAmbiguousSingleNameIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&models.Actress{FirstName: "Shared", Verified: true, Origin: ActressOriginUser}).Error)
	}
	found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: "Shared"})
	require.NoError(t, err)
	require.Equal(t, ResolutionAmbiguous, outcome)
	require.False(t, found.Verified)
}
