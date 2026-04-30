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

func newTestWordDeps(t *testing.T) *core.ServerDependencies {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"},
		Logging:  config.LoggingConfig{Level: "error"},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate())
	t.Cleanup(func() { _ = db.Close() })
	return &core.ServerDependencies{
		GenreReplacementRepo: database.NewGenreReplacementRepository(db),
		WordReplacementRepo:  database.NewWordReplacementRepository(db),
	}
}

func TestWordReplacementList(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo

	require.NoError(t, repo.Create(&models.WordReplacement{Original: "F***", Replacement: "Fuck"}))
	require.NoError(t, repo.Create(&models.WordReplacement{Original: "R**e", Replacement: "Rape"}))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("GET", "/words/replacements", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp wordReplacementListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
	assert.Len(t, resp.Replacements, 2)
}

func TestWordReplacementListPagination(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(&models.WordReplacement{
			Original:    fmt.Sprintf("word%d", i),
			Replacement: fmt.Sprintf("Word%d", i),
		}))
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("GET", "/words/replacements?limit=2&offset=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp wordReplacementListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(5), resp.Total)
	assert.Len(t, resp.Replacements, 2)
}

func TestWordReplacementCreate(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	payload := map[string]string{"original": "F***", "replacement": "Fuck"}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/words/replacements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var created models.WordReplacement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "F***", created.Original)
	assert.Equal(t, "Fuck", created.Replacement)
}

func TestWordReplacementCreateEmptyOriginal(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	payload := map[string]string{"original": "", "replacement": "Test"}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/words/replacements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWordReplacementCreateIdempotent(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	payload := map[string]string{"original": "R**e", "replacement": "Rape"}
	body, _ := json.Marshal(payload)

	req1 := httptest.NewRequest("POST", "/words/replacements", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusCreated, w1.Code)

	body2, _ := json.Marshal(payload)
	req2 := httptest.NewRequest("POST", "/words/replacements", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

func TestWordReplacementUpdate(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo
	require.NoError(t, repo.Create(&models.WordReplacement{Original: "F***", Replacement: "Fuck"}))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	payload := map[string]string{"original": "F***", "replacement": "Forcing"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/words/replacements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updated models.WordReplacement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "Forcing", updated.Replacement)
}

func TestWordReplacementUpdateNotFound(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	payload := map[string]string{"original": "nonexistent", "replacement": "test"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/words/replacements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWordReplacementDeleteByID(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo
	wr := &models.WordReplacement{Original: "K**l", Replacement: "Kill"}
	require.NoError(t, repo.Create(wr))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/words/replacements?id=%d", wr.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWordReplacementDeleteByOriginal(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo
	require.NoError(t, repo.Create(&models.WordReplacement{Original: "B***d", Replacement: "Blood"}))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("DELETE", "/words/replacements?original=B***d", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWordReplacementDeleteMissingParam(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("DELETE", "/words/replacements", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWordReplacementDeleteNotFound(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("DELETE", "/words/replacements?id=9999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWordReplacementDeleteByOriginalNotFound(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("DELETE", "/words/replacements?original=nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWordReplacementExport(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo
	require.NoError(t, repo.Create(&models.WordReplacement{Original: "R**e", Replacement: "Rape"}))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	req := httptest.NewRequest("GET", "/words/replacements/export", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var replacements []models.WordReplacement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &replacements))
	assert.Len(t, replacements, 1)
	assert.Equal(t, "R**e", replacements[0].Original)
}

func TestWordReplacementImport(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	importPayload := map[string]interface{}{
		"replacements": []map[string]string{
			{"original": "CENSORED1", "replacement": "Uncensored1"},
			{"original": "CENSORED2", "replacement": "Uncensored2"},
		},
		"includeDefaults": false,
	}
	body, _ := json.Marshal(importPayload)

	req := httptest.NewRequest("POST", "/words/replacements/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	assert.Equal(t, 2, summary.Imported)
	assert.Equal(t, 0, summary.Skipped)
	assert.Equal(t, 0, summary.Errors)
}

func TestWordReplacementImport_SkipsDefaults(t *testing.T) {
	deps := newTestWordDeps(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	importPayload := map[string]interface{}{
		"replacements": []map[string]string{
			{"original": "R**e", "replacement": "Rape"},
			{"original": "", "replacement": "Empty"},
		},
		"includeDefaults": false,
	}
	body, _ := json.Marshal(importPayload)

	req := httptest.NewRequest("POST", "/words/replacements/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	assert.Equal(t, 1, summary.Skipped)
	assert.Equal(t, 1, summary.Errors)
}

func TestWordReplacementImport_ExistingUnchanged(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo
	require.NoError(t, repo.Create(&models.WordReplacement{Original: "CENSORED_X", Replacement: "UncensoredX"}))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	importPayload := map[string]interface{}{
		"replacements": []map[string]string{
			{"original": "CENSORED_X", "replacement": "UncensoredX"},
		},
		"includeDefaults": false,
	}
	body, _ := json.Marshal(importPayload)

	req := httptest.NewRequest("POST", "/words/replacements/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	assert.Equal(t, 0, summary.Imported)
	assert.Equal(t, 1, summary.Skipped)
}

func TestWordReplacementImport_ExistingUpdated(t *testing.T) {
	deps := newTestWordDeps(t)
	repo := deps.WordReplacementRepo
	require.NoError(t, repo.Create(&models.WordReplacement{Original: "CENSORED_Y", Replacement: "OldValue"}))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	RegisterRoutes(protected, deps)

	importPayload := map[string]interface{}{
		"replacements": []map[string]string{
			{"original": "CENSORED_Y", "replacement": "NewValue"},
		},
		"includeDefaults": false,
	}
	body, _ := json.Marshal(importPayload)

	req := httptest.NewRequest("POST", "/words/replacements/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	assert.Equal(t, 1, summary.Imported)
	assert.Equal(t, 0, summary.Skipped)
}
