package genre

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- listGenres: repo.List error ---

func TestListGenres_Miss2_RepoListError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return(nil, errors.New("db offline"))

	mockGenreReplRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(
		database.ReplacementRepos{GenreRepo: mockRepo, GenreReplacementRepo: mockGenreReplRepo},
		database.TranslationRepos{},
	)
	router := gin.New()
	router.GET("/genres", listGenres(deps))

	req := httptest.NewRequest("GET", "/genres", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- listGenres: with translations ---

func TestListGenres_Miss2_WithTranslations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return([]models.Genre{
		{ID: 1, Name: "HD"},
		{ID: 2, Name: "4K"},
	}, nil)

	mockTransRepo := mocks.NewMockGenreTranslationRepositoryInterface(t)
	mockTransRepo.EXPECT().FindByGenreIDsAndLanguage(context.Background(), []uint{1, 2}, "en").Return(
		map[uint][]models.GenreTranslation{
			1: {{GenreID: 1, Language: "en", Name: "High Definition"}},
		}, nil)

	mockGenreReplRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(
		database.ReplacementRepos{GenreRepo: mockRepo, GenreReplacementRepo: mockGenreReplRepo},
		database.TranslationRepos{GenreTranslationRepo: mockTransRepo},
	)
	router := gin.New()
	router.GET("/genres", listGenres(deps))

	req := httptest.NewRequest("GET", "/genres?include_translations=en", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- listGenres: translation repo nil (best-effort skip) ---

func TestListGenres_Miss2_TranslationNilSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return([]models.Genre{
		{ID: 1, Name: "HD"},
	}, nil)

	mockGenreReplRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(
		database.ReplacementRepos{GenreRepo: mockRepo, GenreReplacementRepo: mockGenreReplRepo},
		database.TranslationRepos{}, // nil GenreTranslationRepo
	)
	router := gin.New()
	router.GET("/genres", listGenres(deps))

	req := httptest.NewRequest("GET", "/genres?include_translations=en", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- updateGenreReplacement: not found returns 404 ---

func TestUpdateGenreReplacement_Miss2_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(nil, database.ErrNotFound)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/replacements", updateGenreReplacement(deps, func() {}))

	body := `{"original":"HD","replacement":"HD Video"}`
	req := httptest.NewRequest("PUT", "/replacements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- updateGenreReplacement: empty original returns 400 ---

func TestUpdateGenreReplacement_Miss2_EmptyOriginal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/replacements", updateGenreReplacement(deps, func() {}))

	body := `{"original":"","replacement":"Value"}`
	req := httptest.NewRequest("PUT", "/replacements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "original is required")
}

// --- deleteGenreReplacement: no id or original returns 400 ---

func TestDeleteGenreReplacement_Miss2_NoParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "id or original")
}

// --- deleteGenreReplacement: by ID, not found ---

func TestDeleteGenreReplacement_Miss2_NotFoundByID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByID(context.Background(), uint(999)).Return(nil, database.ErrNotFound)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements?id=999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- deleteGenreReplacement: by original, not found ---

func TestDeleteGenreReplacement_Miss2_NotFoundByOriginal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "NONEXISTENT").Return(nil, database.ErrNotFound)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements?original=NONEXISTENT", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- createGenreReplacement: already exists returns 200 ---

func TestCreateGenreReplacement_Miss2_AlreadyExists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(
		&models.GenreReplacement{Original: "HD", Replacement: "High Definition"}, nil)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/replacements", createGenreReplacement(deps, func() {}))

	body := `{"original":"HD","replacement":"HD Video"}`
	req := httptest.NewRequest("POST", "/replacements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- exportGenreReplacements: repo error ---

func TestExportGenreReplacements_Miss2_RepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return(nil, errors.New("db offline"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.GET("/export", exportGenreReplacements(deps))

	req := httptest.NewRequest("GET", "/export", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- importGenreReplacements: skipped (existing same replacement) ---

func TestImportGenreReplacements_Miss2_SkippedExisting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(
		&models.GenreReplacement{Original: "HD", Replacement: "High Definition"}, nil)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/import", importGenreReplacements(deps, func() {}))

	importPayload := map[string]interface{}{
		"replacements": []map[string]string{
			{"original": "HD", "replacement": "High Definition"},
		},
	}
	body, _ := json.Marshal(importPayload)
	req := httptest.NewRequest("POST", "/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	assert.Equal(t, 1, summary.Skipped)
	assert.Equal(t, 0, summary.Imported)
}
