package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func postRegisteredMerge(t *testing.T, repo database.ActressRepositoryInterface, targetID, sourceID uint, resolutions map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	RegisterRoutes(group, ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}})
	payload, err := json.Marshal(map[string]any{"target_id": targetID, "source_id": sourceID, "resolutions": resolutions})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/merge", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func TestRegisteredMergeRetainsCandidateAmbiguityAcrossRestartAndRetries(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "merge-ambiguity.db")
	open := func() *database.DB {
		db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(ctx))
		return db
	}
	movieInput := func(id string) *models.Movie {
		return &models.Movie{
			ContentID: id,
			ID:        id,
			Title:     "Merge ambiguity " + id,
			Credits: []models.MovieCredit{{
				MovieContentID: id,
				CreditedName:   "Reported Alpha",
				Origin:         string(models.CreditOriginScrape),
				Scraped:        models.Actress{DMMID: 88201, FirstName: "Alpha", LastName: "Reported"},
			}},
		}
	}
	db := open()
	canonical := models.Actress{FirstName: "Beta", LastName: "Canonical", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&canonical).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Reported Alpha", CanonicalName: "Canonical Beta"}).Error)
	movies := database.NewMovieRepository(db)
	first, err := movies.Upsert(ctx, movieInput("merge-api-same"))
	require.NoError(t, err)
	require.Len(t, first.Credits, 1)
	candidateID := first.Credits[0].ActressID
	otherCandidate := models.Actress{FirstName: "Other", LastName: "Candidate", Origin: database.ActressOriginScrape}
	require.NoError(t, db.Create(&otherCandidate).Error)
	res := postRegisteredMerge(t, database.NewActressRepository(db), candidateID, otherCandidate.ID, nil)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	var survivor models.Actress
	require.NoError(t, db.First(&survivor, candidateID).Error)
	require.False(t, survivor.Verified)
	require.True(t, survivor.AmbiguityQuarantined)
	require.NoError(t, db.Close())

	db = open()
	t.Cleanup(func() { _ = db.Close() })
	movies = database.NewMovieRepository(db)
	resolved, outcome, err := database.ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 88201, FirstName: "Alpha", LastName: "Reported"})
	require.NoError(t, err)
	require.Equal(t, database.ResolutionAmbiguous, outcome)
	require.Equal(t, candidateID, resolved.ID)

	same, err := movies.Upsert(ctx, movieInput("merge-api-same"))
	require.NoError(t, err)
	other, err := movies.Upsert(ctx, movieInput("merge-api-other"))
	require.NoError(t, err)
	for _, movie := range []*models.Movie{same, other} {
		require.Empty(t, movie.Actresses)
		require.Equal(t, candidateID, movie.Credits[0].ActressID)
		require.ErrorIs(t, movies.WithApplyArtifactPublicationFence(ctx, movie.ContentID, movie.RenderGeneration, func(*models.Movie) error { return nil }), database.ErrApplyArtifactPublicationBlocked)
	}
	counts, err := database.NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{same.ContentID, other.ContentID})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[same.ContentID])
	require.EqualValues(t, 1, counts[other.ContentID])

	beforeMerge := map[string]int64{same.ContentID: same.RenderGeneration, other.ContentID: other.RenderGeneration}
	res = postRegisteredMerge(t, database.NewActressRepository(db), canonical.ID, candidateID, nil)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	survivor = models.Actress{}
	require.NoError(t, db.First(&survivor, canonical.ID).Error)
	require.True(t, survivor.Verified)
	require.False(t, survivor.AmbiguityQuarantined)
	require.Equal(t, 88201, survivor.DMMID)
	var sourceCount int64
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", candidateID).Count(&sourceCount).Error)
	require.Zero(t, sourceCount)
	var alias models.ActressAlias
	require.NoError(t, db.Where("alias_name = ?", "Reported Alpha").First(&alias).Error)
	require.Equal(t, "Canonical Beta", alias.CanonicalName)
	for _, contentID := range []string{same.ContentID, other.ContentID} {
		movie, err := movies.FindByContentID(ctx, contentID)
		require.NoError(t, err)
		require.Len(t, movie.Actresses, 1)
		require.Equal(t, canonical.ID, movie.Actresses[0].ID)
		require.Equal(t, beforeMerge[contentID]+1, movie.RenderGeneration)
		require.NoError(t, movies.WithApplyArtifactPublicationFence(ctx, contentID, movie.RenderGeneration, func(*models.Movie) error { return nil }))
	}
	counts, err = database.NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{same.ContentID, other.ContentID})
	require.NoError(t, err)
	require.Zero(t, counts[same.ContentID])
	require.Zero(t, counts[other.ContentID])
}

func TestRegisteredMergeVerifiedSourceClosesDifferentlyNamedIdentityGates(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "merge-verified-source.db")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))

	verified := models.Actress{FirstName: "Beta", LastName: "Canonical", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Reported Alpha", CanonicalName: "Canonical Beta"}).Error)
	movies := database.NewMovieRepository(db)
	upsert := func(id string, dmmID int, reported string) *models.Movie {
		t.Helper()
		movie, upsertErr := movies.Upsert(ctx, &models.Movie{ContentID: id, ID: id, Title: id, Credits: []models.MovieCredit{{
			MovieContentID: id, CreditedName: reported, Origin: string(models.CreditOriginScrape),
			Scraped: models.Actress{DMMID: dmmID, FirstName: "Alpha", LastName: "Reported"},
		}}})
		require.NoError(t, upsertErr)
		return movie
	}
	first := upsert("merge-reverse-one", 88202, "Reported Alpha")
	second := upsert("merge-reverse-two", 88202, "Reported Alpha")
	candidateID := first.Credits[0].ActressID
	require.Equal(t, candidateID, second.Credits[0].ActressID)

	unrelatedCanonical := models.Actress{JapaneseName: "Unrelated Canonical", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&unrelatedCanonical).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Unrelated Reported", CanonicalName: "Unrelated Canonical"}).Error)
	unrelated, err := movies.Upsert(ctx, &models.Movie{ContentID: "merge-unrelated", ID: "merge-unrelated", Title: "unrelated", Credits: []models.MovieCredit{{
		MovieContentID: "merge-unrelated", CreditedName: "Unrelated Reported", Origin: string(models.CreditOriginScrape),
		Scraped: models.Actress{DMMID: 88203, JapaneseName: "Unrelated Reported"},
	}}})
	require.NoError(t, err)
	unrelatedGeneration := unrelated.RenderGeneration

	res := postRegisteredMerge(t, database.NewActressRepository(db), candidateID, verified.ID, map[string]string{
		"first_name": "source", "last_name": "source",
	})
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	var survivor models.Actress
	require.NoError(t, db.First(&survivor, candidateID).Error)
	require.True(t, survivor.Verified)
	require.False(t, survivor.AmbiguityQuarantined)
	require.Equal(t, 88202, survivor.DMMID)
	require.Equal(t, "Beta", survivor.FirstName)
	require.Equal(t, "Canonical", survivor.LastName)
	var deleted int64
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", verified.ID).Count(&deleted).Error)
	require.Zero(t, deleted)
	var alias models.ActressAlias
	require.NoError(t, db.Where("alias_name = ?", "Reported Alpha").First(&alias).Error)
	require.Equal(t, "Canonical Beta", alias.CanonicalName)

	counts, err := database.NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{first.ContentID, second.ContentID, unrelated.ContentID})
	require.NoError(t, err)
	for _, before := range []*models.Movie{first, second} {
		movie, findErr := movies.FindByContentID(ctx, before.ContentID)
		require.NoError(t, findErr)
		require.Len(t, movie.Actresses, 1)
		require.Equal(t, candidateID, movie.Actresses[0].ID)
		require.Equal(t, before.RenderGeneration+1, movie.RenderGeneration)
		require.Zero(t, counts[before.ContentID])
		require.NoError(t, movies.WithApplyArtifactPublicationFence(ctx, movie.ContentID, movie.RenderGeneration, func(*models.Movie) error { return nil }))
	}
	unrelatedAfter, err := movies.FindByContentID(ctx, unrelated.ContentID)
	require.NoError(t, err)
	require.Equal(t, unrelatedGeneration, unrelatedAfter.RenderGeneration)
	require.EqualValues(t, 1, counts[unrelated.ContentID])
	require.ErrorIs(t, movies.WithApplyArtifactPublicationFence(ctx, unrelated.ContentID, unrelatedAfter.RenderGeneration, func(*models.Movie) error { return nil }), database.ErrApplyArtifactPublicationBlocked)
}

func TestRegisteredVerifiedMergeIdentityClosureFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "merge-identity-rollback.db")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))

	verified := models.Actress{FirstName: "Beta", LastName: "Canonical", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Reported Alpha", CanonicalName: "Canonical Beta"}).Error)
	movies := database.NewMovieRepository(db)
	movie, err := movies.Upsert(ctx, &models.Movie{ContentID: "merge-rollback", ID: "merge-rollback", Title: "rollback", Credits: []models.MovieCredit{{
		MovieContentID: "merge-rollback", CreditedName: "Reported Alpha", Origin: string(models.CreditOriginScrape),
		Scraped: models.Actress{DMMID: 88204, FirstName: "Alpha", LastName: "Reported"},
	}}})
	require.NoError(t, err)
	candidateID := movie.Credits[0].ActressID
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_verified_identity_close
		BEFORE UPDATE OF status ON credit_collisions
		WHEN OLD.status = 'open' AND NEW.status = 'resolved'
		BEGIN SELECT RAISE(FAIL, 'identity close failure'); END`).Error)

	res := postRegisteredMerge(t, database.NewActressRepository(db), verified.ID, candidateID, nil)
	require.Equal(t, http.StatusInternalServerError, res.Code, res.Body.String())

	var verifiedAfter, candidateAfter models.Actress
	require.NoError(t, db.First(&verifiedAfter, verified.ID).Error)
	require.NoError(t, db.First(&candidateAfter, candidateID).Error)
	require.True(t, verifiedAfter.Verified)
	require.Zero(t, verifiedAfter.DMMID)
	require.False(t, candidateAfter.Verified)
	require.True(t, candidateAfter.AmbiguityQuarantined)
	require.Equal(t, 88204, candidateAfter.DMMID)
	var alias models.ActressAlias
	require.NoError(t, db.Where("alias_name = ?", "Reported Alpha").First(&alias).Error)
	require.Equal(t, "Canonical Beta", alias.CanonicalName)

	persisted, err := movies.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, movie.RenderGeneration, persisted.RenderGeneration)
	require.Empty(t, persisted.Actresses)
	require.Len(t, persisted.Credits, 1)
	require.Equal(t, candidateID, persisted.Credits[0].ActressID)
	counts, err := database.NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{movie.ContentID})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[movie.ContentID])
	require.ErrorIs(t, movies.WithApplyArtifactPublicationFence(ctx, movie.ContentID, persisted.RenderGeneration, func(*models.Movie) error { return nil }), database.ErrApplyArtifactPublicationBlocked)
}
