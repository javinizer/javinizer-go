package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestAPIRoutesKeepStoredMoviesReadableWithoutWorkflowFactory(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = false
	cfg.System.TempDir = t.TempDir()
	deps := testkit.CreateTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	movie := &models.Movie{ContentID: "DEGRADED-001", ID: "DEGRADED-001", Title: "Previously stored movie"}
	_, err := deps.Repos.MovieRepo.Upsert(t.Context(), movie)
	require.NoError(t, err)

	// Simulate factory construction refusing the installed matcher settings.
	// The router must preserve repository-backed reads rather than panic or
	// build a replacement poster generator outside the workflow factory.
	broken := *cfg
	broken.Matching.RegexEnabled = true
	broken.Matching.RegexPattern = "["
	rt.SetConfig(&broken)
	require.Nil(t, rt.GetPosterGen())
	require.Nil(t, rt.GetWorkflow())
	router := gin.New()
	registerAPIV1Routes(router, rt)

	read := httptest.NewRecorder()
	router.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/v1/movies/DEGRADED-001", nil))
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), movie.Title)

	scrape := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/scrape", strings.NewReader(`{"id":"DEGRADED-001"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(scrape, request)
	require.Equal(t, http.StatusInternalServerError, scrape.Code, scrape.Body.String())
	require.JSONEq(t, `{"error":"workflow not available"}`, scrape.Body.String())
	stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, movie.Title, stored.Title, "refused scrape must preserve existing metadata")
	require.Nil(t, rt.GetPosterGen(), "route registration must leave the runtime poster generator unavailable")
}
