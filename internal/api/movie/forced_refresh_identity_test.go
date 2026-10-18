package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type forcedAPISource struct{ mockScraperWithResults }

func (s *forcedAPISource) Search(context.Context, string) (*models.ScraperResult, error) {
	copy := *s.result
	return &copy, nil
}

func TestForcedRefreshAPIConflictLeavesStoredMovieReadable(t *testing.T) {
	for _, tc := range []struct{ name, id, content string }{
		{"same_display", "REFRESH-101", "different00101"}, {"changed_display", "OTHER-101", "different00101"}, {"blank_display", "", "different00101"}, {"changed_display_missing_content", "OTHER-101", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx := context.Background()
			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Priority = []string{"r18dev"}
			deps := createTestDeps(t, cfg, "")
			old, err := deps.Repos.MovieRepo.Upsert(ctx, &models.Movie{ID: "REFRESH-101", ContentID: "refresh00101", Title: "Saved title"})
			require.NoError(t, err)
			other, err := deps.Repos.MovieRepo.Upsert(ctx, &models.Movie{ID: "OTHER-101", ContentID: "different00101", Title: "Saved other"})
			require.NoError(t, err)
			source := &forcedAPISource{mockScraperWithResults{name: "r18dev", enabled: true, result: &models.ScraperResult{Source: "r18dev", ID: tc.id, ContentID: tc.content, Title: "Different content"}}}
			deps.CoreDeps.GetRegistry().RegisterInstance(source)
			movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))
			router := gin.New()
			router.POST("/movies/:id/rescrape", rescrapeMovie(movieDeps))
			router.GET("/movies/:id", getMovie(movieDeps))
			req := httptest.NewRequest(http.MethodPost, "/movies/REFRESH-101/rescrape", bytes.NewBufferString(`{"selected_scrapers":["r18dev"],"force":true}`))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			assert.Equal(t, http.StatusInternalServerError, response.Code)
			var rejected map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rejected))
			assert.Contains(t, string(rejected["error"]), database.ErrMovieContentIDConflict.Error())
			assert.NotContains(t, rejected, "movie", "failed refresh must not expose a saveable conflicting candidate")
			get := httptest.NewRecorder()
			router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/movies/REFRESH-101", nil))
			require.Equal(t, http.StatusOK, get.Code)
			var read contracts.MovieResponse
			require.NoError(t, json.Unmarshal(get.Body.Bytes(), &read))
			require.NotNil(t, read.Movie)
			assert.Equal(t, old.ContentID, read.Movie.Code)
			assert.Equal(t, old.Title, read.Movie.Title)
			assert.NotContains(t, get.Body.String(), "RequireExactContentID")
			assert.NotContains(t, get.Body.String(), "require_exact_content_id")
			after, err := deps.Repos.MovieRepo.FindByID(ctx, old.ID)
			require.NoError(t, err)
			assert.Equal(t, old, after)
			otherAfter, err := deps.Repos.MovieRepo.FindByContentID(ctx, other.ContentID)
			require.NoError(t, err)
			assert.Equal(t, other, otherAfter)
		})
	}
}
