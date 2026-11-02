package actress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func candidateDismissalEnv(t *testing.T) (*database.DB, *gin.Engine) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
	return db, router
}

func candidateRequest(router http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/actresses/"+path, nil))
	return response
}

func TestCandidateDismissalRoutesAndPagination(t *testing.T) {
	db, router := candidateDismissalEnv(t)
	timestamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	candidates := []models.Actress{
		{FirstName: "First", Origin: database.ActressOriginScrape, UpdatedAt: timestamp},
		{FirstName: "Second", Origin: database.ActressOriginScrape, UpdatedAt: timestamp},
		{FirstName: "Third", Origin: database.ActressOriginScrape, UpdatedAt: timestamp},
		{FirstName: "Verified", Verified: true, Origin: database.ActressOriginUser, UpdatedAt: timestamp},
	}
	require.NoError(t, db.Create(&candidates).Error)
	for _, candidate := range candidates[:2] {
		for range 2 {
			response := candidateRequest(router, http.MethodPost, "candidates/"+itoa(candidate.ID)+"/dismiss")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.JSONEq(t, `{"ok":true}`, response.Body.String())
		}
	}
	for _, tc := range []struct {
		query string
		total int64
		id    uint
	}{
		{"", 1, candidates[2].ID},
		{"?dismissed=false", 1, candidates[2].ID},
		{"?dismissed=true&limit=1&offset=0", 2, candidates[0].ID},
		{"?dismissed=true&limit=1&offset=1", 2, candidates[1].ID},
	} {
		response := candidateRequest(router, http.MethodGet, "candidates"+tc.query)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result candidateListResponse
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, tc.total, result.Total)
		require.Len(t, result.Candidates, 1)
		require.Equal(t, tc.id, result.Candidates[0].ID)
	}
	response := candidateRequest(router, http.MethodGet, "candidates?dismissed=true&limit=1&offset=2")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result candidateListResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, int64(2), result.Total)
	require.Empty(t, result.Candidates)
	response = candidateRequest(router, http.MethodPost, "candidates/"+itoa(candidates[0].ID)+"/promote")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	for range 2 {
		response = candidateRequest(router, http.MethodPost, "candidates/"+itoa(candidates[0].ID)+"/restore")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	response = candidateRequest(router, http.MethodPost, "candidates/"+itoa(candidates[0].ID)+"/promote")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = candidateRequest(router, http.MethodGet, "candidates?dismissed=true")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, int64(1), result.Total)
	require.Len(t, result.Candidates, 1)
	require.Equal(t, candidates[1].ID, result.Candidates[0].ID)
}

func TestCandidateDismissalRouteValidation(t *testing.T) {
	db, router := candidateDismissalEnv(t)
	verified := models.Actress{FirstName: "Verified", Verified: true}
	require.NoError(t, db.Create(&verified).Error)
	for _, action := range []string{"dismiss", "restore"} {
		for _, tc := range []struct {
			id   string
			code int
		}{
			{"0", http.StatusBadRequest}, {"-1", http.StatusBadRequest}, {"abc", http.StatusBadRequest},
			{"18446744073709551616", http.StatusBadRequest}, {"99999", http.StatusNotFound},
			{itoa(verified.ID), http.StatusConflict},
		} {
			response := candidateRequest(router, http.MethodPost, "candidates/"+tc.id+"/"+action)
			require.Equal(t, tc.code, response.Code, response.Body.String())
		}
	}
	for _, query := range []string{"?dismissed=oops", "?dismissed="} {
		response := candidateRequest(router, http.MethodGet, "candidates"+query)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
}

func TestCandidateDismissalRoutesMissingDatabase(t *testing.T) {
	router := gin.New()
	router.GET("/api/v1/actresses/candidates", ListCandidates(ActressDeps{}))
	response := candidateRequest(router, http.MethodGet, "candidates?dismissed=true")
	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	router.POST("/api/v1/actresses/candidates/:id/dismiss", DismissCandidate(ActressDeps{}))
	router.POST("/api/v1/actresses/candidates/:id/restore", RestoreCandidate(ActressDeps{}))
	for _, action := range []string{"dismiss", "restore"} {
		response := candidateRequest(router, http.MethodPost, "candidates/1/"+action)
		require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	}
}

type dismissAfterPromotionPrecheckRepo struct {
	database.ActressRepositoryInterface
	actual *database.ActressRepository
	once   sync.Once
}

func (r *dismissAfterPromotionPrecheckRepo) FindByID(ctx context.Context, id uint) (*models.Actress, error) {
	found, err := r.ActressRepositoryInterface.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	r.once.Do(func() { err = r.actual.SetCandidateDismissed(ctx, id, true) })
	return found, err
}

func TestCandidateDismissalWinsAfterPromotionHandlerPrecheck(t *testing.T) {
	db, _ := candidateDismissalEnv(t)
	repos := db.Repositories()
	candidate := models.Actress{FirstName: "Candidate", Origin: database.ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	content := repos.ContentRepos
	content.ActressRepo = &dismissAfterPromotionPrecheckRepo{ActressRepositoryInterface: repos.ActressRepo, actual: database.NewActressRepository(db)}
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewActressDeps(content, repos.TranslationRepos))
	response := candidateRequest(router, http.MethodPost, "candidates/"+itoa(candidate.ID)+"/promote")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "dismiss")
	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.False(t, stored.Verified)
	require.Equal(t, candidate.FirstName, stored.FirstName)
}
