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

// --- listGenreReplacements: repo.List error ---

func TestListGenreReplacements_Miss_RepoListError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return(nil, errors.New("db offline"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.GET("/replacements", listGenreReplacements(deps))

	req := httptest.NewRequest("GET", "/replacements", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- createGenreReplacement: FindByOriginal non-NotFound error ---

func TestCreateGenreReplacement_Miss_FindByOriginalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(nil, errors.New("conn refused")) // non-NotFound error

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/replacements", createGenreReplacement(deps, func() {}))

	body := `{"original":"HD","replacement":"High Definition"}`
	req := httptest.NewRequest("POST", "/replacements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- createGenreReplacement: Create error ---

func TestCreateGenreReplacement_Miss_CreateError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(nil, database.ErrNotFound)
	mockRepo.EXPECT().Create(context.Background(), &models.GenreReplacement{Original: "HD", Replacement: "High Definition"}).Return(errors.New("write fail"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/replacements", createGenreReplacement(deps, func() {}))

	body := `{"original":"HD","replacement":"High Definition"}`
	req := httptest.NewRequest("POST", "/replacements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- updateGenreReplacement: FindByOriginal non-NotFound error ---

func TestUpdateGenreReplacement_Miss_FindByOriginalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(nil, errors.New("conn refused"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/replacements", updateGenreReplacement(deps, func() {}))

	body := `{"original":"HD","replacement":"HD Video"}`
	req := httptest.NewRequest("PUT", "/replacements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- updateGenreReplacement: Upsert error ---

func TestUpdateGenreReplacement_Miss_UpsertError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(
		&models.GenreReplacement{Original: "HD", Replacement: "Old"}, nil)
	mockRepo.EXPECT().Upsert(context.Background(), &models.GenreReplacement{Original: "HD", Replacement: "HD Video"}).Return(errors.New("write fail"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/replacements", updateGenreReplacement(deps, func() {}))

	body := `{"original":"HD","replacement":"HD Video"}`
	req := httptest.NewRequest("PUT", "/replacements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- deleteGenreReplacement: by ID, FindByID non-NotFound error ---

func TestDeleteGenreReplacement_Miss_FindByIDError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByID(context.Background(), uint(1)).Return(nil, errors.New("conn refused"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements?id=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- deleteGenreReplacement: by ID, DeleteByID error ---

func TestDeleteGenreReplacement_Miss_DeleteByIDError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByID(context.Background(), uint(1)).Return(
		&models.GenreReplacement{Original: "HD", Replacement: "High Definition"}, nil)
	mockRepo.EXPECT().DeleteByID(context.Background(), uint(1)).Return(errors.New("delete fail"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements?id=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- deleteGenreReplacement: by original, FindByOriginal non-NotFound error ---

func TestDeleteGenreReplacement_Miss_FindByOriginalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(nil, errors.New("conn refused"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements?original=HD", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- deleteGenreReplacement: by original, Delete error ---

func TestDeleteGenreReplacement_Miss_DeleteError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(
		&models.GenreReplacement{Original: "HD", Replacement: "High Definition"}, nil)
	mockRepo.EXPECT().Delete(context.Background(), "HD").Return(errors.New("delete fail"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements?original=HD", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- deleteGenreReplacement: invalid id format ---

func TestDeleteGenreReplacement_Miss_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.DELETE("/replacements", deleteGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("DELETE", "/replacements?id=not-a-number", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "id must be a number")
}

// --- importGenreReplacements: invalid JSON ---

func TestImportGenreReplacements_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/import", importGenreReplacements(deps, func() {}))

	req := httptest.NewRequest("POST", "/import", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- importGenreReplacements: FindByOriginal error during import ---

func TestImportGenreReplacements_Miss_FindByOriginalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(nil, errors.New("db error"))

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
	assert.Equal(t, 1, summary.Errors)
	assert.Equal(t, 0, summary.Imported)
}

// --- importGenreReplacements: Create error during import ---

func TestImportGenreReplacements_Miss_CreateError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(nil, database.ErrNotFound)
	mockRepo.EXPECT().Create(context.Background(), &models.GenreReplacement{Original: "HD", Replacement: "High Definition"}).Return(errors.New("write fail"))

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
	assert.Equal(t, 1, summary.Errors)
	assert.Equal(t, 0, summary.Imported)
}

// --- importGenreReplacements: Upsert error during import (existing with changed replacement) ---

func TestImportGenreReplacements_Miss_UpsertError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "HD").Return(
		&models.GenreReplacement{Original: "HD", Replacement: "Old"}, nil)
	mockRepo.EXPECT().Upsert(context.Background(), &models.GenreReplacement{Original: "HD", Replacement: "New"}).Return(errors.New("upsert fail"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/import", importGenreReplacements(deps, func() {}))

	importPayload := map[string]interface{}{
		"replacements": []map[string]string{
			{"original": "HD", "replacement": "New"},
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
	assert.Equal(t, 1, summary.Errors)
	assert.Equal(t, 0, summary.Imported)
}

// --- importGenreReplacements: empty original counts as error ---

func TestImportGenreReplacements_Miss_EmptyOriginal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/import", importGenreReplacements(deps, func() {}))

	importPayload := map[string]interface{}{
		"replacements": []map[string]string{
			{"original": "", "replacement": "Value"},
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
	assert.Equal(t, 1, summary.Errors)
}

// --- createGenreReplacement: bad JSON body ---

func TestCreateGenreReplacement_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/replacements", createGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("POST", "/replacements", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- updateGenreReplacement: bad JSON body ---

func TestUpdateGenreReplacement_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/replacements", updateGenreReplacement(deps, func() {}))

	req := httptest.NewRequest("PUT", "/replacements", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- listGenreReplacements: offset beyond list length ---

func TestListGenreReplacements_Miss_OffsetBeyondLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return([]models.GenreReplacement{
		{Original: "HD", Replacement: "High Definition"},
	}, nil)

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.GET("/replacements", listGenreReplacements(deps))

	req := httptest.NewRequest("GET", "/replacements?offset=100", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp genreReplacementListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
	assert.Empty(t, resp.Replacements)
}
