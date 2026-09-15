package actress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

func newCollisionTestEnv(t *testing.T) (database.Repositories, *gin.RouterGroup) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/")
	RegisterRoutes(group, deps)
	return repos, group
}

func TestListCandidatesExcludesVerified(t *testing.T) {
	db, _ := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	depsWithDB := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)

	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		JapaneseName: "候補", Verified: false, Origin: "scrape",
	}))
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		JapaneseName: "正規", Verified: true, Origin: "user",
	}))

	r2 := gin.New()
	group2 := r2.Group("/")
	RegisterRoutes(group2, depsWithDB)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/actresses/candidates", nil)
	r2.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Candidates []models.Actress `json:"candidates"`
		Total      int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
	require.Len(t, resp.Candidates, 1)
	assert.Equal(t, "候補", resp.Candidates[0].JapaneseName)
}

func TestPromoteCandidateEndpoint(t *testing.T) {
	db, _ := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		JapaneseName: "候補者", Verified: false, Origin: "scrape",
	}))
	candidates, err := repos.ActressRepo.ListCandidates(t.Context(), 10, 0)
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	r := gin.New()
	group := r.Group("/")
	RegisterRoutes(group, deps)

	body, _ := json.Marshal(map[string]string{"first_name": "Promoted", "last_name": "Candidate"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/actresses/candidates/"+itoa(uint(candidates[0].ID))+"/promote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	promoted, err := repos.ActressRepo.FindByID(t.Context(), candidates[0].ID)
	require.NoError(t, err)
	assert.True(t, promoted.Verified)
	assert.Equal(t, "user", promoted.Origin)
}

func TestResolveCollisionKeepIdentity(t *testing.T) {
	db, _ := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()

	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{
		DMMID: 42, JapaneseName: "正規アイドル", Verified: true, Origin: "user",
	}))
	movie := &models.Movie{ContentID: "mov-1", ID: "mov-1", Credits: []models.MovieCredit{{
		CreditedName: "まったく違う名前", Source: "dmm", Origin: "scrape",
		Scraped: models.Actress{DMMID: 42},
	}}}
	movie.Credits[0].MovieContentID = "mov-1"
	_, err := repos.MovieRepo.UpsertWithTranslations(t.Context(), movie, nil, nil)
	require.NoError(t, err)

	open, err := repos.CreditCollisionRepo.ListOpenByMovie(t.Context(), "mov-1")
	require.NoError(t, err)
	require.Len(t, open, 1)

	deps := NewActressDeps(repos.ContentRepos, repos.TranslationRepos)
	r := gin.New()
	group := r.Group("/")
	RegisterRoutes(group, deps)

	body, _ := json.Marshal(map[string]string{"resolution": "keep_identity"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/actresses/collisions/"+itoa(uint(open[0].ID))+"/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		RemainingOpen int `json:"remaining_open"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.RemainingOpen)

	stillOpen, err := repos.CreditCollisionRepo.HasOpenForMovie(t.Context(), "mov-1")
	require.NoError(t, err)
	assert.False(t, stillOpen)
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}
