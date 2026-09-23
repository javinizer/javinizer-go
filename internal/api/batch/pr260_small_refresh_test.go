package batch

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPR260BatchFullRefreshAuthoritativeEmptyCreditsAndSlimSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	persisted := &models.Movie{ID: "PR260-refresh-authority", ContentID: "PR260-refresh-authority", Title: "database"}
	_, err := deps.Repos.MovieRepo.Upsert(nil, persisted)
	require.NoError(t, err)
	stored, err := deps.Repos.MovieRepo.FindByID(nil, persisted.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Empty(t, stored.Credits)
	job := deps.JobStore.CreateJobBatch([]string{"authority.mp4"})
	snapshot := &models.Movie{ID: persisted.ID, ContentID: persisted.ContentID, Title: "snapshot", Credits: []models.MovieCredit{{CreditedName: "snapshot credit"}}}
	setJobResult(job, "authority.mp4", &resultstore.MovieResult{Movie: snapshot, FileMatchInfo: models.FileMatchInfo{MovieID: persisted.ID}, ResultID: "authority-result", Revision: 3})

	slim := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(slim)
	c.Request = httptest.NewRequest(http.MethodGet, "/batch/"+job.GetID(), nil)
	getBatchJobSlim(lifecycleDepsFromCore(deps), c, job.GetID())
	require.Equal(t, http.StatusOK, slim.Code)
	require.Contains(t, slim.Body.String(), persisted.ID)
	require.NotContains(t, slim.Body.String(), "snapshot credit")

	full := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(full)
	c.Request = httptest.NewRequest(http.MethodGet, "/batch/"+job.GetID()+"?include_data=true", nil)
	getBatchJobFull(lifecycleDepsFromCore(deps), c, job.GetID())
	require.Equal(t, http.StatusOK, full.Code, full.Body.String())
	require.NotContains(t, full.Body.String(), "snapshot credit")
	require.Equal(t, "snapshot credit", snapshot.Credits[0].CreditedName, "refresh must not mutate the original snapshot")
}

func TestPR260BatchRefreshMissingPrerequisitesAreNoop(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/batch/noop", nil)
	require.NoError(t, refreshBatchJobMovies(c, nil, nil))
	deps := createTestDeps(t, &config.Config{}, "")
	require.NoError(t, refreshBatchJobMovies(c, deps, nil))
}
