package actress

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestListCandidatesCountErrorPR260(t *testing.T) {
	repo := mocks.NewMockActressRepositoryInterface(t)
	repo.EXPECT().ListCandidates(mock.Anything, 7, 3).Return([]models.Actress{}, nil)
	repo.EXPECT().CountCandidates(mock.Anything).Return(int64(0), errors.New("count failed"))
	router := gin.New()
	router.GET("/candidates", ListCandidates(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	request := httptest.NewRequest(http.MethodGet, "/candidates?limit=7&offset=3", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
}

func TestPromoteCandidateRetainsScrapedFieldsPR260(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repos := db.Repositories()
	candidate := &models.Actress{FirstName: "First", LastName: "Last", JapaneseName: "日本名", ThumbURL: "thumb.jpg", Origin: "scrape"}
	require.NoError(t, repos.ActressRepo.Create(context.Background(), candidate))
	router := gin.New()
	router.POST("/candidates/:id/promote", PromoteCandidate(NewActressDeps(repos.ContentRepos, repos.TranslationRepos)))
	request := httptest.NewRequest(http.MethodPost, "/candidates/"+itoa(candidate.ID)+"/promote", bytes.NewBufferString(`{"first_name":"Corrected"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	updated, err := repos.ActressRepo.FindByID(context.Background(), candidate.ID)
	require.NoError(t, err)
	assert.Equal(t, "Corrected", updated.FirstName)
	assert.Equal(t, "Last", updated.LastName)
	assert.Equal(t, "日本名", updated.JapaneseName)
	assert.Equal(t, "thumb.jpg", updated.ThumbURL)
}

func TestResolveCollisionRejectsUnknownResolutionPR260(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repos := db.Repositories()
	router := gin.New()
	router.POST("/collisions/:id/resolve", ResolveCollision(NewActressDeps(repos.ContentRepos, repos.TranslationRepos)))
	request := httptest.NewRequest(http.MethodPost, "/collisions/1/resolve", bytes.NewBufferString(`{"resolution":"unknown"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestPromoteCandidateRepositoryFailuresPR260(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*mocks.MockActressRepositoryInterface)
	}{
		{
			name: "promote",
			setup: func(repo *mocks.MockActressRepositoryInterface) {
				repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(&models.Actress{ID: 1, JapaneseName: "name"}, nil).Once()
				repo.EXPECT().PromoteCandidate(mock.Anything, uint(1), "", "", "name", "").Return(errors.New("promote failed"))
			},
		},
		{
			name: "refetch",
			setup: func(repo *mocks.MockActressRepositoryInterface) {
				repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(&models.Actress{ID: 1, JapaneseName: "name"}, nil).Once()
				repo.EXPECT().PromoteCandidate(mock.Anything, uint(1), "", "", "name", "").Return(nil)
				repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(nil, errors.New("refetch failed")).Once()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := mocks.NewMockActressRepositoryInterface(t)
			test.setup(repo)
			router := gin.New()
			router.POST("/candidates/:id/promote", PromoteCandidate(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
			request := httptest.NewRequest(http.MethodPost, "/candidates/1/promote", bytes.NewBufferString(`{}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, http.StatusInternalServerError, response.Code)
		})
	}
}

func TestImportActresses_ImportUpsertError(t *testing.T) {
	repo := mocks.NewMockActressRepositoryInterface(t)
	repo.EXPECT().FindByJapaneseNameAndDMMID(mock.Anything, "", 1).Return(nil, database.ErrNotFound)
	repo.EXPECT().ImportUpsert(mock.Anything, mock.AnythingOfType("*models.Actress")).Return(errors.New("import failed"))

	router := gin.New()
	router.POST("/actresses/import", importActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	request := httptest.NewRequest(http.MethodPost, "/actresses/import", bytes.NewBufferString(`{"actresses":[{"dmm_id":1,"first_name":"Name"}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"errors":1`)
}

func TestResolveCollisionMissingCollisionPR260(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repos := db.Repositories()
	collision := &models.CreditCollision{CreditID: 999, MovieContentID: "missing-credit", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(collision).Error)
	router := gin.New()
	router.POST("/collisions/:id/resolve", ResolveCollision(NewActressDeps(repos.ContentRepos, repos.TranslationRepos)))
	request := httptest.NewRequest(http.MethodPost, "/collisions/"+itoa(collision.ID)+"/resolve", bytes.NewBufferString(`{"resolution":"keep_identity"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusNotFound, response.Code)
}
