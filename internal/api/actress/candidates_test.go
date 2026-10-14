package actress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

func newCollisionTestEnv(t *testing.T) (database.Repositories, *gin.RouterGroup) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/")
	RegisterRoutes(group, deps)
	return repos, group
}

func TestListCandidatesExcludesVerified(t *testing.T) {
	db, _ := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	depsWithDB := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)

	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		JapaneseName: "候補", Verified: false, Origin: "scrape",
	}))
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		JapaneseName: "正規", Verified: true, Origin: "user",
	}))

	r2 := gin.New()
	group2 := r2.Group("/")
	RegisterRoutes(group2, depsWithDB)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/actresses/candidates", nil)
	r2.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Candidates []models.Actress `json:"candidates"`
		Total      int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
	require.Len(t, resp.Candidates, 1)
	assert.Equal(t, "候補", resp.Candidates[0].JapaneseName)
}

func TestPromoteCandidateEndpoint(t *testing.T) {
	db, _ := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		JapaneseName: "候補者", Verified: false, Origin: "scrape",
	}))
	candidates, err := repos.ActressRepo.ListCandidates(t.Context(), 10, 0)
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	r := gin.New()
	group := r.Group("/")
	RegisterRoutes(group, deps)

	body, _ := json.Marshal(map[string]string{"first_name": "Promoted", "last_name": "Candidate"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/actresses/candidates/"+itoa(uint(candidates[0].ID))+"/promote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	promoted, err := repos.ActressRepo.FindByID(t.Context(), candidates[0].ID)
	require.NoError(t, err)
	assert.True(t, promoted.Verified)
	assert.Equal(t, "user", promoted.Origin)
}

func TestResolveCollisionKeepIdentity(t *testing.T) {
	db, _ := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()

	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		DMMID: 42, JapaneseName: "正規アイドル", Verified: true, Origin: "user",
	}))
	movie := &models.Movie{ContentID: "mov-1", ID: "mov-1", Credits: []models.MovieCredit{{
		CreditedName: "まったく違う名前", Source: "dmm", Origin: "scrape",
		Scraped: models.Actress{DMMID: 42},
	}}}
	movie.Credits[0].MovieContentID = "mov-1"
	_, err := repos.MovieRepo.UpsertWithTranslations(t.Context(), movie, nil, nil)
	require.NoError(t, err)

	open, err := repos.CreditCollisionRepo.ListOpenByMovie(t.Context(), "mov-1")
	require.NoError(t, err)
	require.Len(t, open, 1)

	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	r := gin.New()
	group := r.Group("/")
	RegisterRoutes(group, deps)

	body, _ := json.Marshal(map[string]string{"resolution": "keep_identity"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/actresses/collisions/"+itoa(uint(open[0].ID))+"/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		RemainingOpen int `json:"remaining_open"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.RemainingOpen)

	stillOpen, err := repos.CreditCollisionRepo.HasOpenForMovie(t.Context(), "mov-1")
	require.NoError(t, err)
	assert.False(t, stillOpen)
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}

func TestCollisionEndpointsExposeAndEnforceAllowedResolutions(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	candidate := models.Actress{FirstName: "Candidate", LastName: "Identity", Verified: false, Origin: "scrape"}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "allowed-actions", ID: "allowed-actions"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Identity Candidate"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: "Identity Candidate", CanonicalValue: "Identity Candidate", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)

	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	router := gin.New()
	RegisterRoutes(router.Group("/"), deps)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/actresses/collisions?movie_id=allowed-actions", nil))
	require.Equal(t, http.StatusOK, list.Code)
	var response struct {
		Collisions []struct {
			ID               uint     `json:"id"`
			Allowed          []string `json:"allowed_resolutions"`
			CurrentActressID uint     `json:"current_actress_id"`
		} `json:"collisions"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &response))
	require.Len(t, response.Collisions, 1)
	require.ElementsMatch(t, []string{models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign}, response.Collisions[0].Allowed)
	require.Equal(t, candidate.ID, response.Collisions[0].CurrentActressID)

	body, err := json.Marshal(map[string]string{"resolution": models.CollisionResolutionKeepIdentity})
	require.NoError(t, err)
	rejected := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/actresses/collisions/"+itoa(collision.ID)+"/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rejected, req)
	require.Equal(t, http.StatusBadRequest, rejected.Code)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
}

func TestListCollisionsResolutionPolicyFailures(t *testing.T) {
	t.Run("database dependency missing", func(t *testing.T) {
		db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		repos := db.Repositories()
		deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
		deps.DB = nil
		router := gin.New()
		RegisterRoutes(router.Group("/"), deps)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/actresses/collisions?movie_id=missing-db", nil))
		require.Equal(t, http.StatusInternalServerError, response.Code)
		require.Contains(t, response.Body.String(), databaseNotConfiguredError)
	})

	t.Run("orphaned collision credit", func(t *testing.T) {
		db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		repos := db.Repositories()
		orphan := models.CreditCollision{CreditID: 999, MovieContentID: "orphan-policy", Field: models.CreditFieldCreditedName, Status: models.CollisionStatusOpen}
		require.NoError(t, db.Create(&orphan).Error)
		deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
		router := gin.New()
		RegisterRoutes(router.Group("/"), deps)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/actresses/collisions?movie_id=orphan-policy", nil))
		require.Equal(t, http.StatusInternalServerError, response.Code)
		require.Contains(t, response.Body.String(), "load collision credit 999")
	})
}

func TestResolveCollisionAdoptAliasOwnershipConflictReturns409(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	ownerA := models.Actress{JapaneseName: "Owner A", Verified: true, Origin: database.ActressOriginUser}
	ownerB := models.Actress{JapaneseName: "Owner B", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&ownerA).Error)
	require.NoError(t, db.Create(&ownerB).Error)
	require.NoError(t, repos.ActressAliasRepo.Create(t.Context(), &models.ActressAlias{AliasName: "Stage Name", CanonicalName: ownerA.JapaneseName}))
	movie := models.Movie{ContentID: "alias-owner-conflict", ID: "alias-owner-conflict", RenderGeneration: 9}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: ownerB.ID, CreditedName: "Ｓｔａｇｅ　Ｎａｍｅ"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: credit.CreditedName, CanonicalValue: ownerB.JapaneseName, Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)

	router := gin.New()
	RegisterRoutes(router.Group("/"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
	body, err := json.Marshal(map[string]string{"resolution": models.CollisionResolutionAdoptAlias})
	require.NoError(t, err)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/actresses/collisions/"+itoa(collision.ID)+"/resolve", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())

	found, err := repos.ActressAliasRepo.FindByAliasName(t.Context(), "stage name")
	require.NoError(t, err)
	require.Equal(t, ownerA.JapaneseName, found.CanonicalName)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
	require.EqualValues(t, 9, movie.RenderGeneration)
}
