package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type verifyAfterPromotionPrecheckRepo struct {
	database.ActressRepositoryInterface
	actual *database.ActressRepository
	once   sync.Once
}

func (r *verifyAfterPromotionPrecheckRepo) FindByID(ctx context.Context, id uint) (*models.Actress, error) {
	found, err := r.ActressRepositoryInterface.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	r.once.Do(func() {
		err = r.actual.UpdateCanonicalFields(ctx, id, "Catalog", "Winner", "", "winner-thumb")
	})
	return found, err
}

func TestPromoteCandidateRejectsVerificationAfterHandlerPrecheck(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	candidate := models.Actress{FirstName: "Candidate", LastName: "Before", Origin: database.ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "promotion-precheck-race", ID: "promotion-precheck-race", RenderGeneration: 4}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}).Error)

	actual := repos.ActressRepo.(*database.ActressRepository)
	wrapped := &verifyAfterPromotionPrecheckRepo{ActressRepositoryInterface: repos.ActressRepo, actual: actual}
	content := repos.ContentRepos
	content.ActressRepo = wrapped
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewActressDeps(content, repos.TranslationRepos))
	body := bytes.NewBufferString(`{"first_name":"Stale","last_name":"Request","thumb_url":"stale-thumb"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/candidates/"+itoa(candidate.ID)+"/promote", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.JSONEq(t, `{"error":"identity is already verified"}`, response.Body.String())

	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.Equal(t, "Catalog", stored.FirstName)
	require.Equal(t, "Winner", stored.LastName)
	require.Equal(t, "winner-thumb", stored.ThumbURL)
	require.True(t, stored.Verified)
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
	require.Equal(t, int64(5), movie.RenderGeneration)
	var staleAliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Where("alias_name = ?", "Request Stale").Count(&staleAliases).Error)
	require.Zero(t, staleAliases)
}

func TestMergeSourceCanonicalReplacesStaleTargetTranslationForPublicGet(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	target := models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}
	source := models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	targetTranslation := models.ActressTranslation{ActressID: target.ID, Language: "en", DisplayName: "Stale Target Translation", SourceName: target.FullName()}
	sourceTranslation := models.ActressTranslation{ActressID: source.ID, Language: "en", DisplayName: "Fresh Source Translation", SourceName: source.FullName()}
	require.NoError(t, db.Create(&targetTranslation).Error)
	require.NoError(t, db.Create(&sourceTranslation).Error)

	returned := mergeAndGetActressWithTranslations(t, repos, target.ID, source.ID, map[string]string{"first_name": "source", "last_name": "source"})
	require.Len(t, returned.Translations, 1)
	require.Equal(t, sourceTranslation.ID, returned.Translations[0].ID)
	require.Equal(t, "Fresh Source Translation", returned.Translations[0].DisplayName)

	var rows []models.ActressTranslation
	require.NoError(t, db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, sourceTranslation.ID, rows[0].ID)
	require.Equal(t, target.ID, rows[0].ActressID)
}

func TestMergeSourceCanonicalDeletesTargetOnlyStaleTranslationForPublicGet(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	target := models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}
	source := models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	targetTranslation := models.ActressTranslation{ActressID: target.ID, Language: "en", DisplayName: "Stale Target Translation", SourceName: target.FullName()}
	require.NoError(t, db.Create(&targetTranslation).Error)

	returned := mergeAndGetActressWithTranslations(t, repos, target.ID, source.ID, map[string]string{"first_name": "source", "last_name": "source"})
	require.Empty(t, returned.Translations)
	var rows int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Count(&rows).Error)
	require.Zero(t, rows)
}

func mergeAndGetActressWithTranslations(t *testing.T, repos database.Repositories, targetID, sourceID uint, resolutions map[string]string) models.Actress {
	t.Helper()
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
	mergeBody, err := json.Marshal(map[string]any{"target_id": targetID, "source_id": sourceID, "resolutions": resolutions})
	require.NoError(t, err)
	mergeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/merge", bytes.NewReader(mergeBody))
	mergeRequest.Header.Set("Content-Type", "application/json")
	mergeResponse := httptest.NewRecorder()
	router.ServeHTTP(mergeResponse, mergeRequest)
	require.Equal(t, http.StatusOK, mergeResponse.Code, mergeResponse.Body.String())

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/actresses/"+itoa(targetID)+"?include_translations=en", nil)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
	var returned models.Actress
	require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &returned))
	return returned
}

func TestMergeTargetCanonicalDoesNotExposeSourceTranslation(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	target := models.Actress{FirstName: "Target", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}
	source := models.Actress{FirstName: "Source", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: "en", DisplayName: "Stale Source Translation", SourceName: source.FullName()}).Error)

	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
	mergeBody, err := json.Marshal(map[string]any{"target_id": target.ID, "source_id": source.ID})
	require.NoError(t, err)
	mergeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/merge", bytes.NewReader(mergeBody))
	mergeRequest.Header.Set("Content-Type", "application/json")
	mergeResponse := httptest.NewRecorder()
	router.ServeHTTP(mergeResponse, mergeRequest)
	require.Equal(t, http.StatusOK, mergeResponse.Code, mergeResponse.Body.String())

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/actresses/"+itoa(target.ID)+"?include_translations=en", nil)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
	var returned models.Actress
	require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &returned))
	require.Empty(t, returned.Translations)
	var rows int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Count(&rows).Error)
	require.Zero(t, rows)
}
