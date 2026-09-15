package actress

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCreditEndpointValidation(t *testing.T) {
	for _, build := range []struct {
		name    string
		handler func(ActressDeps) gin.HandlerFunc
	}{
		{"promote", PromoteCandidate}, {"resolve", ResolveCollision}, {"override", UpdateCreditOverride}, {"suppress", SuppressCredit},
	} {
		t.Run(build.name, func(t *testing.T) {
			for _, tc := range []struct {
				id, body string
				status   int
			}{{"bad", "{}", 400}, {"1", "{", 400}} {
				r := gin.New()
				r.POST("/:id", build.handler(ActressDeps{}))
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/"+tc.id, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				require.Equal(t, tc.status, w.Code, w.Body.String())
			}
			if build.name != "promote" {
				r := gin.New()
				r.POST("/:id", build.handler(ActressDeps{}))
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/1", strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				require.Equal(t, 500, w.Code)
				require.Contains(t, w.Body.String(), "database not configured")
			}
		})
	}
}

func TestCreditEndpointLifecycle(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	actress := models.Actress{FirstName: "Candidate", LastName: "Name", ThumbURL: "original", Origin: "scrape"}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "api-credit", ID: "api-credit", Title: "Test"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, Origin: "scrape"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{MovieContentID: movie.ContentID, CreditID: credit.ID, Field: models.CreditFieldCreditedName, ReportedValue: "Reported", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)
	r := gin.New()
	RegisterRoutes(r.Group("/"), deps)
	request := func(method, path, body string, status int) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.String()
	}
	request("GET", "/actresses/collisions", "", 400)
	require.Contains(t, request("GET", "/actresses/collisions?movie_id=api-credit", "", 200), "Reported")
	request("POST", "/actresses/candidates/999/promote", "{}", 404)
	request("POST", "/actresses/candidates/"+itoa(actress.ID)+"/promote", "{}", 200)
	require.NoError(t, db.First(&actress, actress.ID).Error)
	require.Equal(t, "Candidate", actress.FirstName)
	require.Equal(t, "original", actress.ThumbURL)
	request("POST", "/actresses/candidates/"+itoa(actress.ID)+"/promote", "{}", 409)
	request("POST", "/actresses/collisions/"+itoa(collision.ID)+"/resolve", `{"resolution":"invalid"}`, 400)
	for _, action := range []string{"override", "suppress"} {
		request("POST", "/actresses/credits/999/"+action, "{}", 404)
	}
	request("POST", "/actresses/credits/"+itoa(credit.ID)+"/override", `{"override_name":"Per movie","user_override":true}`, 200)
	require.NoError(t, db.First(&credit, credit.ID).Error)
	require.True(t, credit.UserOverride)
	require.Equal(t, "Per movie", credit.OverrideName)
	request("POST", "/actresses/credits/"+itoa(credit.ID)+"/suppress", `{"suppressed":true}`, 200)
	require.NoError(t, db.First(&credit, credit.ID).Error)
	require.True(t, credit.Suppressed)
	request("POST", "/actresses/collisions/"+itoa(collision.ID)+"/resolve", `{"resolution":"keep_identity"}`, 409)
	require.NoError(t, db.Close())
	request("GET", "/actresses/candidates", "", 500)
	request("GET", "/actresses/collisions?movie_id=api-credit", "", 500)
	request("POST", "/actresses/credits/"+itoa(credit.ID)+"/override", "{}", 500)
	request("POST", "/actresses/credits/"+itoa(credit.ID)+"/suppress", "{}", 500)
}
