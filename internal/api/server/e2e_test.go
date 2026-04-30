package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type genreReplacement struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

type wordReplacement struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

type importSummary struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors,omitempty"`
}

func setupDBAndServer(t *testing.T) (*gin.Engine, *ServerDependencies) {
	t.Helper()
	db := setupTestDB(t)

	cfg := &config.Config{
		Server:   config.ServerConfig{Host: "localhost", Port: 8080},
		Logging:  config.LoggingConfig{Level: "error"},
		Matching: config.MatchingConfig{RegexEnabled: false},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}},
		API: config.APIConfig{
			Security: config.SecurityConfig{AllowedOrigins: []string{"http://localhost:8080"}},
		},
	}

	registry := models.NewScraperRegistry()
	agg := aggregator.New(cfg)
	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	deps := &ServerDependencies{
		ConfigFile:           "/tmp/config.yaml",
		Registry:             registry,
		DB:                   db,
		Aggregator:           agg,
		MovieRepo:            database.NewMovieRepository(db),
		ActressRepo:          database.NewActressRepository(db),
		GenreReplacementRepo: database.NewGenreReplacementRepository(db),
		WordReplacementRepo:  database.NewWordReplacementRepository(db),
		HistoryRepo:          database.NewHistoryRepository(db),
		JobRepo:              database.NewJobRepository(db),
		BatchFileOpRepo:      database.NewBatchFileOperationRepository(db),
		EventRepo:            database.NewEventRepository(db),
		Matcher:              mat,
		JobQueue:             worker.NewJobQueue(nil, "", nil),
	}
	deps.SetConfig(cfg)
	router := NewServer(deps)
	t.Cleanup(func() { cleanupServerHub(t, deps) })
	return router, deps
}

// ---- Genre Replacement E2E ----

func TestE2E_GenreReplacementCRUD(t *testing.T) {
	router, _ := setupDBAndServer(t)

	createBody, _ := json.Marshal(genreReplacement{Original: "Blow", Replacement: "Blowjob"})
	req := httptest.NewRequest("POST", "/api/v1/genres/replacements", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 201, w.Code)

	respBody, _ := io.ReadAll(w.Result().Body)
	var created genreReplacement
	json.Unmarshal(respBody, &created)
	assert.Equal(t, "Blow", created.Original)
	assert.Equal(t, "Blowjob", created.Replacement)

	listReq := httptest.NewRequest("GET", "/api/v1/genres/replacements", nil)
	listReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, listReq)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "\"original\":\"Blow\"")

	updateBody, _ := json.Marshal(genreReplacement{Original: "Blow", Replacement: "Oral"})
	updateReq := httptest.NewRequest("PUT", "/api/v1/genres/replacements", bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, updateReq)
	assert.Equal(t, 200, w.Code)

	delReq := httptest.NewRequest("DELETE", "/api/v1/genres/replacements?original=Blow", nil)
	delReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, delReq)
	assert.Equal(t, 200, w.Code)
}

func TestE2E_GenreReplacementImportExport(t *testing.T) {
	router, _ := setupDBAndServer(t)

	importBody, _ := json.Marshal(map[string]any{
		"replacements": []genreReplacement{
			{Original: "Action", Replacement: "アクション"},
			{Original: "Drama", Replacement: "ドラマ"},
			{Original: "Romance", Replacement: "ロマンス"},
		},
	})
	importReq := httptest.NewRequest("POST", "/api/v1/genres/replacements/import", bytes.NewReader(importBody))
	importReq.Header.Set("Content-Type", "application/json")
	importReq.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, importReq)
	assert.Equal(t, 200, w.Code)

	var summary importSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	assert.Equal(t, 3, summary.Imported)
	assert.Equal(t, 0, summary.Skipped)

	exportReq := httptest.NewRequest("GET", "/api/v1/genres/replacements/export", nil)
	exportReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, exportReq)
	assert.Equal(t, 200, w.Code)

	var exported []genreReplacement
	json.Unmarshal(w.Body.Bytes(), &exported)
	assert.Len(t, exported, 3)

	names := make(map[string]string)
	for _, r := range exported {
		names[r.Original] = r.Replacement
	}
	assert.Equal(t, "アクション", names["Action"])
	assert.Equal(t, "ドラマ", names["Drama"])
}

func TestE2E_GenreReplacementImportIdempotent(t *testing.T) {
	router, _ := setupDBAndServer(t)

	replacements := []genreReplacement{
		{Original: "Action", Replacement: "アクション"},
		{Original: "Drama", Replacement: "ドラマ"},
	}

	importBody, _ := json.Marshal(map[string]any{"replacements": replacements})
	for i := 0; i < 3; i++ {
		importReq := httptest.NewRequest("POST", "/api/v1/genres/replacements/import", bytes.NewReader(importBody))
		importReq.Header.Set("Content-Type", "application/json")
		importReq.Header.Set("Origin", "http://localhost:8080")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, importReq)
		assert.Equal(t, 200, w.Code, "import %d should succeed", i)
	}

	listReq := httptest.NewRequest("GET", "/api/v1/genres/replacements", nil)
	listReq.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, listReq)
	var listResp struct {
		Replacements []genreReplacement `json:"replacements"`
	}
	json.Unmarshal(w.Body.Bytes(), &listResp)
	assert.Len(t, listResp.Replacements, 2, "should have exactly 2 unique replacements, not more")
}

func TestE2E_GenreReplacementImportInvalidJSON(t *testing.T) {
	router, _ := setupDBAndServer(t)
	req := httptest.NewRequest("POST", "/api/v1/genres/replacements/import", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

// ---- Word Replacement E2E ----

func TestE2E_WordReplacementCRUD(t *testing.T) {
	router, _ := setupDBAndServer(t)

	createBody, _ := json.Marshal(wordReplacement{Original: "censored", Replacement: "uncensored"})
	req := httptest.NewRequest("POST", "/api/v1/words/replacements", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 201, w.Code)

	listReq := httptest.NewRequest("GET", "/api/v1/words/replacements", nil)
	listReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, listReq)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "\"original\":\"censored\"")

	updateBody, _ := json.Marshal(wordReplacement{Original: "censored", Replacement: "nsfw"})
	updateReq := httptest.NewRequest("PUT", "/api/v1/words/replacements", bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, updateReq)
	assert.Equal(t, 200, w.Code)

	delReq := httptest.NewRequest("DELETE", "/api/v1/words/replacements?original=censored", nil)
	delReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, delReq)
	assert.Equal(t, 200, w.Code)
}

func TestE2E_WordReplacementImportExport(t *testing.T) {
	router, _ := setupDBAndServer(t)

	importBody, _ := json.Marshal(map[string]any{
		"replacements": []wordReplacement{
			{Original: "blur", Replacement: "clear"},
			{Original: "XXX", Replacement: "redacted"},
		},
	})
	importReq := httptest.NewRequest("POST", "/api/v1/words/replacements/import", bytes.NewReader(importBody))
	importReq.Header.Set("Content-Type", "application/json")
	importReq.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, importReq)
	assert.Equal(t, 200, w.Code)

	var summary importSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	assert.Equal(t, 2, summary.Imported)

	exportReq := httptest.NewRequest("GET", "/api/v1/words/replacements/export", nil)
	exportReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, exportReq)
	assert.Equal(t, 200, w.Code)

	var exported []wordReplacement
	json.Unmarshal(w.Body.Bytes(), &exported)
	require.Len(t, exported, 2)
	names := make(map[string]string)
	for _, r := range exported {
		names[r.Original] = r.Replacement
	}
	assert.Equal(t, "clear", names["blur"])
	assert.Equal(t, "redacted", names["XXX"])
}

func TestE2E_WordReplacementImportInvalidJSON(t *testing.T) {
	router, _ := setupDBAndServer(t)
	req := httptest.NewRequest("POST", "/api/v1/words/replacements/import", strings.NewReader("not json at all"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

// ---- Actress E2E ----

func TestE2E_ActressExportEmpty(t *testing.T) {
	router, _ := setupDBAndServer(t)
	req := httptest.NewRequest("GET", "/api/v1/actresses/export", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	var actresses []any
	json.Unmarshal(w.Body.Bytes(), &actresses)
	assert.Empty(t, actresses)
}

func TestE2E_ActressImport(t *testing.T) {
	router, deps := setupDBAndServer(t)

	importBody, _ := json.Marshal(map[string]any{
		"actresses": []map[string]any{
			{"dmm_id": 10001, "japanese_name": "テストA", "first_name": "TestA", "last_name": "Actress"},
			{"dmm_id": 10002, "japanese_name": "テストB", "first_name": "TestB", "last_name": "Actress"},
		},
	})
	importReq := httptest.NewRequest("POST", "/api/v1/actresses/import", bytes.NewReader(importBody))
	importReq.Header.Set("Content-Type", "application/json")
	importReq.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, importReq)
	assert.Equal(t, 200, w.Code)

	var summary importSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	assert.Equal(t, 2, summary.Imported)

	listReq := httptest.NewRequest("GET", "/api/v1/actresses", nil)
	listReq.Header.Set("Origin", "http://localhost:8080")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, listReq)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "\"japanese_name\":\"テストA\"")
	assert.Contains(t, w.Body.String(), "\"japanese_name\":\"テストB\"")

	deps.ActressRepo.List(10, 0)
}

func TestE2E_ActressImportInvalidJSON(t *testing.T) {
	router, _ := setupDBAndServer(t)
	req := httptest.NewRequest("POST", "/api/v1/actresses/import", strings.NewReader("{{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}
