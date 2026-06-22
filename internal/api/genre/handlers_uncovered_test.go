package genre

import (
	"context"
	"encoding/json"
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

func newFullGenreDeps(t *testing.T) (GenreDeps, *database.DB) {
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
	deps := NewGenreDeps(repos.ReplacementRepos, repos.TranslationRepos)
	return deps, db
}

func TestListGenres_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, _ := newFullGenreDeps(t)

	// Seed genre via repo
	_, err := deps.GenreRepo.FindOrCreate(context.Background(), "Action")
	require.NoError(t, err)
	_, err = deps.GenreRepo.FindOrCreate(context.Background(), "Drama")
	require.NoError(t, err)

	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.GET("/genres", listGenres(deps))

	req := httptest.NewRequest("GET", "/api/v1/genres", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp genreListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Count)
	assert.Len(t, resp.Genres, 2)
}

func TestListGenres_WithTranslations_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, _ := newFullGenreDeps(t)

	_, err := deps.GenreRepo.FindOrCreate(context.Background(), "Action")
	require.NoError(t, err)

	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.GET("/genres", listGenres(deps))

	req := httptest.NewRequest("GET", "/api/v1/genres?include_translations=en", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp genreListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Count)
}

func TestListGenres_RepoError_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps := NewGenreDeps(
		database.ReplacementRepos{GenreRepo: &errorGenreRepo{}},
		database.TranslationRepos{},
	)

	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.GET("/genres", listGenres(deps))

	req := httptest.NewRequest("GET", "/api/v1/genres", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSafeFindTranslationsByIDsAndLanguage_NilRepo(t *testing.T) {
	deps := GenreDeps{} // No translation repo
	result, err := deps.safeFindTranslationsByIDsAndLanguage(context.Background(), []uint{1, 2}, "en")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestNewGenreDeps_Uncovered(t *testing.T) {
	replacement := database.ReplacementRepos{}
	translation := database.TranslationRepos{}
	deps := NewGenreDeps(replacement, translation)
	assert.NotNil(t, deps)
}

// errorGenreRepo always returns an error
type errorGenreRepo struct{}

func (e *errorGenreRepo) FindOrCreate(ctx context.Context, name string) (*models.Genre, error) {
	return nil, assert.AnError
}
func (e *errorGenreRepo) List(ctx context.Context) ([]models.Genre, error) {
	return nil, assert.AnError
}
