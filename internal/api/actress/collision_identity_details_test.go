package actress

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Equal display names are not proof of equal identities. The response must
// describe the currently linked identity without resolving the collision.
func TestListCollisionsExplainsIdenticalIdentityNames(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	candidate := models.Actress{DMMID: 1109243, FirstName: "Nagisa", LastName: "Hazuki", JapaneseName: "葉月なぎさ", Origin: "scrape"}
	require.NoError(t, db.Create(&candidate).Error)
	other := models.Actress{DMMID: 1034095, FirstName: "Nagisa", LastName: "Hazuki", JapaneseName: "葉月渚", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&other).Error)
	movie := models.Movie{ContentID: "identical-names", ID: "identical-names"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Hazuki Nagisa"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: "Hazuki Nagisa", CanonicalValue: "Hazuki Nagisa", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)
	repos := db.Repositories()
	router := gin.New()
	RegisterRoutes(router.Group("/"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
	for _, linked := range []models.Actress{candidate, other} {
		require.NoError(t, db.Model(&credit).Update("actress_id", linked.ID).Error)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/actresses/collisions?movie_id="+movie.ContentID, nil))
		require.Equal(t, http.StatusOK, response.Code)
		var body struct {
			Collisions []struct {
				models.CreditCollision
				CurrentActressID uint            `json:"current_actress_id"`
				CurrentActress   *models.Actress `json:"current_actress"`
			} `json:"collisions"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Len(t, body.Collisions, 1)
		got := body.Collisions[0]
		require.Equal(t, got.ReportedValue, got.CanonicalValue)
		require.Equal(t, models.CollisionStatusOpen, got.Status)
		require.NotNil(t, got.CurrentActress, "identical names need current identity details, not a misleading text-only comparison")
		require.Equal(t, linked.ID, got.CurrentActressID)
		require.Equal(t, linked.ID, got.CurrentActress.ID)
		require.Equal(t, linked.DMMID, got.CurrentActress.DMMID)
		require.Equal(t, linked.JapaneseName, got.CurrentActress.JapaneseName)
		require.Equal(t, linked.FirstName, got.CurrentActress.FirstName)
		require.Equal(t, linked.LastName, got.CurrentActress.LastName)
		require.Equal(t, linked.Verified, got.CurrentActress.Verified)
	}
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
}
