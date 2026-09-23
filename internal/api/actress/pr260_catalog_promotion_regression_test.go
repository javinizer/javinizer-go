package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type catalogUpdateFixture struct {
	db         *database.DB
	repos      database.Repositories
	router     *gin.Engine
	actress    models.Actress
	movie      models.Movie
	alias      models.ActressAlias
	collision  models.CreditCollision
	pinned     models.CreditCollision
	unrelated  models.CreditCollision
	unrelatedA models.ActressAlias
}

func newCatalogUpdateFixture(t *testing.T) catalogUpdateFixture {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()

	actress := models.Actress{DMMID: 101, FirstName: "Truth", LastName: "Original", JapaneseName: "旧名", ThumbURL: "old-thumb", Aliases: "Old Profile", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "catalog-update", ID: "catalog-update", RenderGeneration: 7}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Renamed Person", Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&credit).Error)
	alias := models.ActressAlias{AliasName: "Legacy Alias", CanonicalName: actress.JapaneseName}
	require.NoError(t, db.Create(&alias).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Renamed Person", CanonicalValue: actress.JapaneseName, Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)
	pinned := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: "新名", CanonicalValue: actress.JapaneseName, Status: models.CollisionStatusOpen, UserPinned: true}
	require.NoError(t, db.Create(&pinned).Error)

	other := models.Actress{FirstName: "Other", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&other).Error)
	otherMovie := models.Movie{ContentID: "catalog-unrelated", ID: "catalog-unrelated", RenderGeneration: 11}
	require.NoError(t, db.Create(&otherMovie).Error)
	otherCredit := models.MovieCredit{MovieContentID: otherMovie.ContentID, ActressID: other.ID, CreditedName: "Different Person"}
	require.NoError(t, db.Create(&otherCredit).Error)
	unrelated := models.CreditCollision{CreditID: otherCredit.ID, MovieContentID: otherMovie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Different Person", CanonicalValue: other.FullName(), Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&unrelated).Error)
	unrelatedAlias := models.ActressAlias{AliasName: "Unrelated Alias", CanonicalName: other.FullName()}
	require.NoError(t, db.Create(&unrelatedAlias).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
	return catalogUpdateFixture{db: db, repos: repos, router: router, actress: actress, movie: movie, alias: alias, collision: collision, pinned: pinned, unrelated: unrelated, unrelatedA: unrelatedAlias}
}

func catalogUpdateBody() []byte {
	body, _ := json.Marshal(map[string]any{
		"dmm_id": 202, "first_name": "Person", "last_name": "Renamed", "japanese_name": "新名",
		"thumb_url": "new-thumb", "aliases": "New Profile|Second Profile",
	})
	return body
}

func putCatalogActress(f catalogUpdateFixture) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/actresses/"+itoa(f.actress.ID), bytes.NewReader(catalogUpdateBody()))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func TestCatalogPUTAppliesIdentityLifecycleTransactionally(t *testing.T) {
	f := newCatalogUpdateFixture(t)
	response := putCatalogActress(f)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var returned models.Actress
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &returned))
	require.Equal(t, f.actress.ID, returned.ID)
	require.Equal(t, 202, returned.DMMID)
	require.Equal(t, "Person", returned.FirstName)
	require.Equal(t, "Renamed", returned.LastName)
	require.Equal(t, "新名", returned.JapaneseName)
	require.Equal(t, "new-thumb", returned.ThumbURL)
	require.Equal(t, "New Profile|Second Profile", returned.Aliases)
	require.True(t, returned.Verified)
	require.Equal(t, database.ActressOriginUser, returned.Origin)

	stored, err := f.repos.ActressRepo.FindByID(t.Context(), f.actress.ID)
	require.NoError(t, err)
	require.Equal(t, returned.DMMID, stored.DMMID)
	require.Equal(t, returned.ThumbURL, stored.ThumbURL)
	require.Equal(t, returned.Aliases, stored.Aliases)

	for _, oldName := range []string{"旧名", "Original Truth"} {
		resolved, err := f.repos.ActressRepo.FindVerifiedByAlias(t.Context(), oldName)
		require.NoError(t, err)
		require.Equal(t, f.actress.ID, resolved.ID)
	}
	require.NoError(t, f.db.First(&f.alias, f.alias.ID).Error)
	require.Equal(t, "新名", f.alias.CanonicalName)
	require.NoError(t, f.db.First(&f.collision, f.collision.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, f.collision.Status)
	require.Equal(t, "新名", f.collision.CanonicalValue)
	require.NoError(t, f.db.First(&f.pinned, f.pinned.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, f.pinned.Status)
	require.Equal(t, "新名", f.pinned.CanonicalValue)
	require.NoError(t, f.db.First(&f.unrelated, f.unrelated.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, f.unrelated.Status)
	require.NoError(t, f.db.First(&f.unrelatedA, f.unrelatedA.ID).Error)
	require.Equal(t, "Person Other", f.unrelatedA.CanonicalName)

	require.NoError(t, f.db.First(&f.movie, "content_id = ?", f.movie.ContentID).Error)
	require.True(t, f.movie.RenderDirty)
	require.Equal(t, int64(8), f.movie.RenderGeneration)
	published := false
	err = f.repos.MovieRepo.(*database.MovieRepository).WithApplyPublicationFence(t.Context(), f.movie.ContentID, 7, func(*models.Movie) error {
		published = true
		return nil
	})
	require.ErrorIs(t, err, database.ErrApplyPublicationStale)
	require.False(t, published)

	response = putCatalogActress(f)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, f.db.First(&f.movie, "content_id = ?", f.movie.ContentID).Error)
	require.Equal(t, int64(8), f.movie.RenderGeneration)
}

func TestCatalogPUTLifecycleFailuresRollbackEverything(t *testing.T) {
	for _, test := range []struct {
		name    string
		trigger string
	}{
		{"alias", "CREATE TRIGGER fail_catalog_alias BEFORE UPDATE ON actress_aliases BEGIN SELECT RAISE(ABORT, 'alias failure'); END"},
		{"collision", "CREATE TRIGGER fail_catalog_collision BEFORE UPDATE ON credit_collisions BEGIN SELECT RAISE(ABORT, 'collision failure'); END"},
		{"dirty", "CREATE TRIGGER fail_catalog_dirty BEFORE UPDATE OF render_dirty ON movies BEGIN SELECT RAISE(ABORT, 'dirty failure'); END"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCatalogUpdateFixture(t)
			require.NoError(t, f.db.Exec(test.trigger).Error)
			response := putCatalogActress(f)
			require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())

			stored, err := f.repos.ActressRepo.FindByID(t.Context(), f.actress.ID)
			require.NoError(t, err)
			require.Equal(t, 101, stored.DMMID)
			require.Equal(t, "Truth", stored.FirstName)
			require.Equal(t, "Original", stored.LastName)
			require.Equal(t, "旧名", stored.JapaneseName)
			require.Equal(t, "old-thumb", stored.ThumbURL)
			require.Equal(t, "Old Profile", stored.Aliases)
			require.NoError(t, f.db.First(&f.alias, f.alias.ID).Error)
			require.Equal(t, "旧名", f.alias.CanonicalName)
			require.NoError(t, f.db.First(&f.collision, f.collision.ID).Error)
			require.Equal(t, models.CollisionStatusOpen, f.collision.Status)
			require.Equal(t, "旧名", f.collision.CanonicalValue)
			require.NoError(t, f.db.First(&f.movie, "content_id = ?", f.movie.ContentID).Error)
			require.False(t, f.movie.RenderDirty)
			require.Equal(t, int64(7), f.movie.RenderGeneration)
		})
	}
}

func TestPromoteCandidateOptionalBodyHTTPContract(t *testing.T) {
	bodies := []struct {
		name        string
		body        io.Reader
		contentType string
	}{
		{"nil body", nil, ""},
		{"content length zero", http.NoBody, "application/json"},
		{"empty object", strings.NewReader(`{}`), "application/json"},
		{"whitespace", strings.NewReader("  \n\t"), "application/json"},
		{"null", strings.NewReader("null"), "application/json"},
	}
	for _, test := range bodies {
		t.Run(test.name, func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repos := db.Repositories()
			candidate := models.Actress{FirstName: "Candidate", LastName: "Person", JapaneseName: "候補", ThumbURL: "candidate-thumb", Verified: false, Origin: database.ActressOriginScrape}
			require.NoError(t, db.Create(&candidate).Error)
			movie := models.Movie{ContentID: "promote-" + strings.ReplaceAll(test.name, " ", "-"), ID: "promote-" + strings.ReplaceAll(test.name, " ", "-"), RenderGeneration: 3}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Candidate Person"}
			require.NoError(t, db.Create(&credit).Error)
			collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}
			require.NoError(t, db.Create(&collision).Error)
			unrelated := models.ActressAlias{AliasName: "Do Not Touch", CanonicalName: "Other Person"}
			require.NoError(t, db.Create(&unrelated).Error)

			router := gin.New()
			RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/candidates/"+itoa(candidate.ID)+"/promote", test.body)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())

			var returned models.Actress
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &returned))
			require.True(t, returned.Verified)
			require.Equal(t, database.ActressOriginUser, returned.Origin)
			require.Equal(t, candidate.FirstName, returned.FirstName)
			require.Equal(t, candidate.LastName, returned.LastName)
			require.Equal(t, candidate.JapaneseName, returned.JapaneseName)
			require.Equal(t, candidate.ThumbURL, returned.ThumbURL)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, models.CollisionStatusResolved, collision.Status)
			require.Equal(t, models.CollisionResolutionKeepIdentity, collision.Resolution)
			require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
			require.True(t, movie.RenderDirty)
			require.Equal(t, int64(4), movie.RenderGeneration)
			var projectionCount int64
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, candidate.ID).Count(&projectionCount).Error)
			require.Equal(t, int64(1), projectionCount)
			require.NoError(t, db.First(&unrelated, unrelated.ID).Error)
			require.Equal(t, "Other Person", unrelated.CanonicalName)
		})
	}
}

func TestPromoteCandidateOptionalBodyRejectsInvalidJSON(t *testing.T) {
	for _, body := range []string{`{`, `{"first_name":1}`} {
		t.Run(body, func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
			repos := db.Repositories()
			candidate := models.Actress{FirstName: "Candidate", Verified: false, Origin: database.ActressOriginScrape}
			require.NoError(t, db.Create(&candidate).Error)
			router := gin.New()
			RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/candidates/"+itoa(candidate.ID)+"/promote", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
			stored, err := repos.ActressRepo.FindByID(t.Context(), candidate.ID)
			require.NoError(t, err)
			require.False(t, stored.Verified)
		})
	}
}

func TestPromoteCandidateEmptyBodyStatusContracts(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	verified := models.Actress{FirstName: "Verified", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))

	for _, test := range []struct {
		name string
		id   uint
		want int
	}{
		{"missing", verified.ID + 1000, http.StatusNotFound},
		{"already verified", verified.ID, http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/candidates/"+itoa(test.id)+"/promote", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, test.want, response.Code, response.Body.String())
		})
	}
}
