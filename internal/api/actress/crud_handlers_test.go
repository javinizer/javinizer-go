package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupActressTestDB creates an in-memory DB with migrations for full handler tests.
func setupActressTestDB(t *testing.T) (*database.DB, database.ActressRepositoryInterface, database.ActressTranslationRepositoryInterface) {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"},
		Logging:  config.LoggingConfig{Level: "error"},
	}
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	repos := db.Repositories()
	return db, repos.ActressRepo, repos.ActressTranslationRepo
}

func TestListActresses_WithSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	// Seed data
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 1, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 2, FirstName: "Ai", LastName: "Uehara", JapaneseName: "上原亜衣"}))

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?q=Yui&limit=10&offset=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp actressesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
	require.Len(t, resp.Actresses, 1)
	assert.Equal(t, "Yui", resp.Actresses[0].FirstName)
}

func TestListActresses_NoSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 1, FirstName: "Yui", LastName: "Hatano"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 2, FirstName: "Ai", LastName: "Uehara"}))

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?limit=10&offset=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp actressesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
	assert.Len(t, resp.Actresses, 2)
}

func TestListActresses_WithSortBy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 30, FirstName: "A", LastName: "One"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 10, FirstName: "B", LastName: "Two"}))

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?sort_by=dmm_id&sort_order=desc&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp actressesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Actresses, 2)
	assert.Equal(t, 30, resp.Actresses[0].DMMID)
	assert.Equal(t, 10, resp.Actresses[1].DMMID)
}

func TestListActresses_InvalidSort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?sort_by=invalid_col&sort_order=asc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActresses_WithTranslations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, transRepo := setupActressTestDB(t)

	actress := &models.Actress{DMMID: 1, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}
	require.NoError(t, repo.Create(context.Background(), actress))

	// Add translation using Upsert
	require.NoError(t, transRepo.Upsert(context.Background(), &models.ActressTranslation{
		ActressID: actress.ID,
		Language:  "en",
		FirstName: "Yui EN",
		LastName:  "Hatano EN",
	}))

	deps := ActressDeps{
		ContentRepos:     database.ContentRepos{ActressRepo: repo},
		TranslationRepos: database.TranslationRepos{ActressTranslationRepo: transRepo},
	}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?include_translations=en&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp actressesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Actresses, 1)
	assert.Len(t, resp.Actresses[0].Translations, 1)
	assert.Equal(t, "Yui EN", resp.Actresses[0].Translations[0].FirstName)
}

func TestListActresses_TranslationRepoNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 1, FirstName: "Yui"}))

	// No translation repo configured
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?include_translations=en&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp actressesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Actresses, 1)
	assert.Empty(t, resp.Actresses[0].Translations, "No translations when repo is nil")
}

func TestGetActress_Found(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	actress := &models.Actress{DMMID: 1, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}
	require.NoError(t, repo.Create(context.Background(), actress))

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses/:id", getActress(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses/"+toString(actress.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var fetched models.Actress
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fetched))
	assert.Equal(t, actress.ID, fetched.ID)
	assert.Equal(t, "Yui", fetched.FirstName)
	assert.Equal(t, "波多野結衣", fetched.JapaneseName)
}

func TestGetActress_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses/:id", getActress(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses/99999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetActress_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses/:id", getActress(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetActress_WithTranslation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, transRepo := setupActressTestDB(t)

	actress := &models.Actress{DMMID: 1, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}
	require.NoError(t, repo.Create(context.Background(), actress))

	require.NoError(t, transRepo.Upsert(context.Background(), &models.ActressTranslation{
		ActressID: actress.ID,
		Language:  "en",
		FirstName: "Yui EN",
		LastName:  "Hatano EN",
	}))

	deps := ActressDeps{
		ContentRepos:     database.ContentRepos{ActressRepo: repo},
		TranslationRepos: database.TranslationRepos{ActressTranslationRepo: transRepo},
	}
	router := gin.New()
	router.GET("/actresses/:id", getActress(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses/"+toString(actress.ID)+"?include_translations=en", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var fetched models.Actress
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fetched))
	assert.Equal(t, actress.ID, fetched.ID)
	assert.Len(t, fetched.Translations, 1)
	assert.Equal(t, "Yui EN", fetched.Translations[0].FirstName)
}

func TestGetActress_TranslationBestEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	actress := &models.Actress{DMMID: 1, FirstName: "Yui"}
	require.NoError(t, repo.Create(context.Background(), actress))

	// No translation repo configured — should still succeed
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses/:id", getActress(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses/"+toString(actress.ID)+"?include_translations=en", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var fetched models.Actress
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fetched))
	assert.Empty(t, fetched.Translations, "No translations when repo is nil")
}

func TestCreateActress_Handler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.POST("/actresses", createActress(deps))

	payload := map[string]interface{}{
		"dmm_id":        100,
		"first_name":    "Test",
		"last_name":     "models.Actress",
		"japanese_name": "テスト",
		"thumb_url":     "https://example.com/img.jpg",
		"aliases":       "Alias1",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/actresses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var created models.Actress
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "Test", created.FirstName)
	assert.Equal(t, "テスト", created.JapaneseName)
	assert.NotZero(t, created.ID)
}

func TestCreateActress_InvalidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.POST("/actresses", createActress(deps))

	// Missing required fields
	payload := map[string]interface{}{
		"dmm_id": 100,
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/actresses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateActress_Handler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	existing := &models.Actress{DMMID: 200, FirstName: "Old", LastName: "Name"}
	require.NoError(t, repo.Create(context.Background(), existing))

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(deps))

	payload := map[string]interface{}{
		"dmm_id":        200,
		"first_name":    "Updated",
		"last_name":     "Name",
		"japanese_name": "更新",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/actresses/"+toString(existing.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updated models.Actress
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "Updated", updated.FirstName)
	assert.Equal(t, "更新", updated.JapaneseName)
}

func TestDeleteActress_Handler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	existing := &models.Actress{DMMID: 300, FirstName: "ToDelete"}
	require.NoError(t, repo.Create(context.Background(), existing))

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.DELETE("/actresses/:id", deleteActress(deps))

	req := httptest.NewRequest(http.MethodDelete, "/actresses/"+toString(existing.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify gone
	_, err := repo.FindByID(context.Background(), existing.ID)
	assert.Error(t, err, "models.Actress should be deleted")
}

func TestListActresses_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp actressesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(0), resp.Total)
	assert.Empty(t, resp.Actresses)
}

func TestListActresses_PaginationWithSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, repo, _ := setupActressTestDB(t)

	// Create 5 actresses with similar names
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.Actress{
			DMMID:     600 + i,
			FirstName: fmt.Sprintf("SearchTest%d", i),
		}))
	}

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?q=SearchTest&limit=2&offset=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp actressesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(5), resp.Total)
	assert.Len(t, resp.Actresses, 2)
}
