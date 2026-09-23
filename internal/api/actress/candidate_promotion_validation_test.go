package actress

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPromoteCandidateErrorStatusMapping(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*mocks.MockActressRepositoryInterface)
		wantCode int
	}{
		{name: "missing", setup: func(repo *mocks.MockActressRepositoryInterface) {
			repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(nil, database.ErrNotFound)
		}, wantCode: http.StatusNotFound},
		{name: "lookup database error", setup: func(repo *mocks.MockActressRepositoryInterface) {
			repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(nil, errors.New("lookup failed"))
		}, wantCode: http.StatusInternalServerError},
		{name: "repository validation", setup: func(repo *mocks.MockActressRepositoryInterface) {
			repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(&models.Actress{ID: 1, FirstName: "Valid"}, nil)
			repo.EXPECT().PromoteCandidate(mock.Anything, uint(1), "Valid", "", "", "").Return(database.ErrInvalidLookup)
		}, wantCode: http.StatusBadRequest},
		{name: "already verified race", setup: func(repo *mocks.MockActressRepositoryInterface) {
			repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(&models.Actress{ID: 1, FirstName: "Valid"}, nil)
			repo.EXPECT().PromoteCandidate(mock.Anything, uint(1), "Valid", "", "", "").Return(database.ErrCandidateAlreadyVerified)
		}, wantCode: http.StatusConflict},
		{name: "promotion database error", setup: func(repo *mocks.MockActressRepositoryInterface) {
			repo.EXPECT().FindByID(mock.Anything, uint(1)).Return(&models.Actress{ID: 1, FirstName: "Valid"}, nil)
			repo.EXPECT().PromoteCandidate(mock.Anything, uint(1), "Valid", "", "", "").Return(errors.New("promotion failed"))
		}, wantCode: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := mocks.NewMockActressRepositoryInterface(t)
			test.setup(repo)
			router := gin.New()
			router.POST("/candidates/:id/promote", PromoteCandidate(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/candidates/1/promote", nil))
			require.Equal(t, test.wantCode, response.Code, response.Body.String())
		})
	}
}

func TestPromoteCandidateRejectsZeroID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := mocks.NewMockActressRepositoryInterface(t)
	router := gin.New()
	router.POST("/candidates/:id/promote", PromoteCandidate(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/candidates/0/promote", bytes.NewBufferString(`{"first_name":"Zero"}`)))
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	repo.AssertNotCalled(t, "FindByID", mock.Anything, uint(0))
}

func TestPromoteCandidateNormalizesResolvedCanonicalFields(t *testing.T) {
	tests := []struct {
		name      string
		candidate models.Actress
		body      string
		wantCode  int
		wantFirst string
		wantLast  string
		wantJP    string
		wantThumb string
	}{
		{name: "present whitespace clears all fields and rejects missing canonical name", candidate: models.Actress{FirstName: "Scraped", LastName: "Person", JapaneseName: "候補", ThumbURL: "https://example.test/scraped.jpg"}, body: `{"first_name":" ","last_name":"\t","japanese_name":"  ","thumb_url":"\n"}`, wantCode: http.StatusBadRequest},
		{name: "omitted fields keep scraped values", candidate: models.Actress{FirstName: "Scraped", LastName: "Person", JapaneseName: "候補", ThumbURL: "https://example.test/scraped.jpg"}, body: `{"first_name":"  Override  "}`, wantCode: http.StatusOK, wantFirst: "Override", wantLast: "Person", wantJP: "候補", wantThumb: "https://example.test/scraped.jpg"},
		{name: "present empty optional fields clear", candidate: models.Actress{FirstName: "Scraped", LastName: "Person", JapaneseName: "候補", ThumbURL: "https://example.test/scraped.jpg"}, body: `{"last_name":"","japanese_name":"","thumb_url":""}`, wantCode: http.StatusOK, wantFirst: "Scraped"},
		{name: "present empty first clears while omitted japanese remains", candidate: models.Actress{FirstName: "Scraped", LastName: "Person", JapaneseName: "候補", ThumbURL: "https://example.test/scraped.jpg"}, body: `{"first_name":""}`, wantCode: http.StatusOK, wantLast: "Person", wantJP: "候補", wantThumb: "https://example.test/scraped.jpg"},
		{name: "present empty first rejects when japanese is empty", candidate: models.Actress{FirstName: "Scraped", LastName: "Person", ThumbURL: "https://example.test/scraped.jpg"}, body: `{"first_name":""}`, wantCode: http.StatusBadRequest},
		{name: "malformed thumbnail URL", candidate: models.Actress{FirstName: "Scraped", ThumbURL: "https://example.test/scraped.jpg"}, body: `{"thumb_url":"not a url"}`, wantCode: http.StatusBadRequest},
		{name: "non HTTP thumbnail URL", candidate: models.Actress{FirstName: "Scraped", ThumbURL: "https://example.test/scraped.jpg"}, body: `{"thumb_url":"ftp://example.test/image.jpg"}`, wantCode: http.StatusBadRequest},
		{name: "valid HTTP thumbnail URL", candidate: models.Actress{FirstName: "Scraped"}, body: `{"thumb_url":"http://example.test/image.jpg"}`, wantCode: http.StatusOK, wantFirst: "Scraped", wantThumb: "http://example.test/image.jpg"},
		{name: "trim overrides", candidate: models.Actress{FirstName: "Scraped"}, body: `{"first_name":"  Override  ","last_name":"  Name  ","thumb_url":"  https://example.test/override.jpg  "}`, wantCode: http.StatusOK, wantFirst: "Override", wantLast: "Name", wantThumb: "https://example.test/override.jpg"},
		{name: "id only empty object", candidate: models.Actress{DMMID: 701}, body: `{}`, wantCode: http.StatusBadRequest},
		{name: "id only whitespace body", candidate: models.Actress{DMMID: 702}, body: "  \n\t", wantCode: http.StatusBadRequest},
		{name: "whitespace existing and body", candidate: models.Actress{DMMID: 702, FirstName: " ", JapaneseName: "\t"}, body: `{"first_name":" ","japanese_name":"\n"}`, wantCode: http.StatusBadRequest},
		{name: "last only", candidate: models.Actress{LastName: "Only"}, body: `{}`, wantCode: http.StatusBadRequest},
		{name: "japanese only", candidate: models.Actress{}, body: `{"japanese_name":"  日本名  "}`, wantCode: http.StatusOK, wantJP: "日本名"},
		{name: "first only", candidate: models.Actress{}, body: `{"first_name":"  First  "}`, wantCode: http.StatusOK, wantFirst: "First"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			test.candidate.Origin = database.ActressOriginScrape
			test.candidate.AmbiguityQuarantined = true
			require.NoError(t, db.Create(&test.candidate).Error)
			movie := models.Movie{ContentID: "promotion-validation", ID: "promotion-validation", RenderGeneration: 9}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: test.candidate.ID, CreditedName: "reported"}
			require.NoError(t, db.Create(&credit).Error)
			collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}
			require.NoError(t, db.Create(&collision).Error)
			translation := models.ActressTranslation{ActressID: test.candidate.ID, Language: "en", DisplayName: "untouched"}
			require.NoError(t, db.Create(&translation).Error)

			router := gin.New()
			repos := db.Repositories()
			RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/candidates/"+itoa(test.candidate.ID)+"/promote", bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, test.wantCode, response.Code, response.Body.String())

			var stored models.Actress
			require.NoError(t, db.First(&stored, test.candidate.ID).Error)
			if test.wantCode == http.StatusOK {
				require.True(t, stored.Verified)
				require.Equal(t, test.wantFirst, stored.FirstName)
				require.Equal(t, test.wantLast, stored.LastName)
				require.Equal(t, test.wantJP, stored.JapaneseName)
				require.Equal(t, test.wantThumb, stored.ThumbURL)
				var returned models.Actress
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &returned))
				require.Equal(t, stored.FirstName, returned.FirstName)
				return
			}
			require.False(t, stored.Verified)
			require.Equal(t, test.candidate.FirstName, stored.FirstName)
			require.Equal(t, test.candidate.LastName, stored.LastName)
			require.Equal(t, test.candidate.JapaneseName, stored.JapaneseName)
			require.Equal(t, test.candidate.ThumbURL, stored.ThumbURL)
			require.True(t, stored.AmbiguityQuarantined)
			require.NoError(t, db.First(&translation, translation.ID).Error)
			require.NoError(t, db.First(&credit, credit.ID).Error)
			require.Equal(t, test.candidate.ID, credit.ActressID)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, models.CollisionStatusOpen, collision.Status)
			require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
			require.False(t, movie.RenderDirty)
			require.Equal(t, int64(9), movie.RenderGeneration)
		})
	}
}
