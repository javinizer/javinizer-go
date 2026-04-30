package genre

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGenreRepo(t *testing.T) *database.GenreReplacementRepository {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"},
		Logging:  config.LoggingConfig{Level: "error"},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate())
	t.Cleanup(func() { _ = db.Close() })
	return database.NewGenreReplacementRepository(db)
}

func newTestDeps(t *testing.T) *core.ServerDependencies {
	t.Helper()
	repo := newTestGenreRepo(t)
	deps := &core.ServerDependencies{
		GenreReplacementRepo: repo,
	}
	return deps
}

func setupRouter(deps *core.ServerDependencies) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)
	return router
}

func TestGenreReplacementList(t *testing.T) {
	deps := newTestDeps(t)
	repo := deps.GenreReplacementRepo

	require.NoError(t, repo.Create(&models.GenreReplacement{Original: "HD", Replacement: "High Definition"}))
	require.NoError(t, repo.Create(&models.GenreReplacement{Original: "VR", Replacement: "Virtual Reality"}))

	router := setupRouter(deps)

	req := httptest.NewRequest("GET", "/genres/replacements", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp genreReplacementListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
	assert.Len(t, resp.Replacements, 2)
}

func TestGenreReplacementCreate(t *testing.T) {
	deps := newTestDeps(t)
	router := setupRouter(deps)

	payload := map[string]string{"original": "HD", "replacement": "High Definition"}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/genres/replacements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var created models.GenreReplacement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "HD", created.Original)
	assert.Equal(t, "High Definition", created.Replacement)
}

func TestGenreReplacementCreateIdempotent(t *testing.T) {
	deps := newTestDeps(t)
	router := setupRouter(deps)

	payload := map[string]string{"original": "HD", "replacement": "High Definition"}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req1 := httptest.NewRequest("POST", "/genres/replacements", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusCreated, w1.Code)

	body2, _ := json.Marshal(payload)
	req2 := httptest.NewRequest("POST", "/genres/replacements", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)

	var existing models.GenreReplacement
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &existing))
	assert.Equal(t, "HD", existing.Original)
}

func TestGenreReplacementDelete(t *testing.T) {
	deps := newTestDeps(t)
	repo := deps.GenreReplacementRepo
	entity := models.GenreReplacement{Original: "HD", Replacement: "High Definition"}
	require.NoError(t, repo.Create(&entity))

	router := setupRouter(deps)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/genres/replacements?id=%d", entity.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "genre replacement deleted", resp["message"])
	assert.Equal(t, "HD", resp["original"])
}

func TestGenreReplacementDeleteNotFound(t *testing.T) {
	deps := newTestDeps(t)
	router := setupRouter(deps)

	req := httptest.NewRequest("DELETE", "/genres/replacements?id=9999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGenreReplacementDeleteWithSpecialCharacters(t *testing.T) {
	deps := newTestDeps(t)
	repo := deps.GenreReplacementRepo
	entity := models.GenreReplacement{Original: "Threesome / Foursome", Replacement: "Group"}
	require.NoError(t, repo.Create(&entity))

	router := setupRouter(deps)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/genres/replacements?id=%d", entity.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "genre replacement deleted", resp["message"])
	assert.Equal(t, "Threesome / Foursome", resp["original"])
}

func TestGenreReplacementDeleteMissingOriginal(t *testing.T) {
	deps := newTestDeps(t)
	router := setupRouter(deps)

	req := httptest.NewRequest("DELETE", "/genres/replacements", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGenreReplacementDeleteByOriginal(t *testing.T) {
	deps := newTestDeps(t)
	repo := deps.GenreReplacementRepo
	require.NoError(t, repo.Create(&models.GenreReplacement{Original: "HD", Replacement: "High Definition"}))

	router := setupRouter(deps)

	req := httptest.NewRequest("DELETE", "/genres/replacements?original=HD", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "genre replacement deleted", resp["message"])
	assert.Equal(t, "HD", resp["original"])
}

func TestGenreReplacementDeleteByOriginalNotFound(t *testing.T) {
	deps := newTestDeps(t)
	router := setupRouter(deps)

	req := httptest.NewRequest("DELETE", "/genres/replacements?original=nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGenreReplacementCreateEmptyOriginal(t *testing.T) {
	deps := newTestDeps(t)
	router := setupRouter(deps)

	payload := map[string]string{"original": "", "replacement": "High Definition"}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/genres/replacements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGenreReplacementListPagination(t *testing.T) {
	deps := newTestDeps(t)
	repo := deps.GenreReplacementRepo

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(&models.GenreReplacement{
			Original:    "genre" + string(rune('A'+i)),
			Replacement: "Genre " + string(rune('A'+i)),
		}))
	}

	router := setupRouter(deps)

	req := httptest.NewRequest("GET", "/genres/replacements?limit=2&offset=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp genreReplacementListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(5), resp.Total)
	assert.Len(t, resp.Replacements, 2)
	assert.Equal(t, 2, resp.Limit)
	assert.Equal(t, 0, resp.Offset)
}
