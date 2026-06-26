package genre

import (
	"context"
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
)

// --- exportGenreReplacements: repo.List error branch ---

func TestExportGenreReplacements_RepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := mocks.NewMockGenreReplacementRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return(nil, errors.New("db down"))

	deps := NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.GET("/export", exportGenreReplacements(deps))

	req := httptest.NewRequest("GET", "/export", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- updateWordReplacement: FindByOriginal non-NotFound error ---

func TestUpdateWordReplacement_FindByOriginalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "test").Return(nil, errors.New("conn refused"))

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/update", updateWordReplacement(deps, func() {}))

	req := httptest.NewRequest("PUT", "/update", strings.NewReader(`{"original":"test","replacement":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- updateWordReplacement: Upsert error ---

func TestUpdateWordReplacement_UpsertError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "word").Return(
		&models.WordReplacement{Original: "word", Replacement: "old"}, nil)
	mockRepo.EXPECT().Upsert(context.Background(), &models.WordReplacement{Original: "word", Replacement: "new"}).Return(errors.New("write fail"))

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/update", updateWordReplacement(deps, func() {}))

	req := httptest.NewRequest("PUT", "/update", strings.NewReader(`{"original":"word","replacement":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

// --- exportWordReplacements: repo.List error branch ---

func TestExportWordReplacements_RepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)
	mockRepo.EXPECT().List(context.Background()).Return(nil, errors.New("db error"))

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.GET("/export", exportWordReplacements(deps))

	req := httptest.NewRequest("GET", "/export", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}
