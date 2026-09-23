package batch

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFinalAuthoritativeRefreshRejectsDeletedLiveJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	movie := &models.Movie{ID: "PR260-refresh-vanished", ContentID: "PR260-refresh-vanished", Title: "database"}
	_, err := deps.Repos.MovieRepo.Upsert(nil, movie)
	require.NoError(t, err)
	job := deps.JobStore.CreateJobBatch([]string{"vanished.mp4"})
	snapshot := &models.Movie{ID: movie.ID, ContentID: movie.ContentID, Title: "snapshot"}
	result := &resultstore.MovieResult{Movie: snapshot, FileMatchInfo: models.FileMatchInfo{MovieID: movie.ID}, ResultID: "vanished-result", Revision: 3}
	setJobResult(job, "vanished.mp4", result)
	status := job.GetStatus()
	require.Equal(t, "snapshot", status.Results["vanished.mp4"].Movie.Title)
	require.NoError(t, deps.JobStore.DeleteJob(job.GetID()))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/batch/vanished?include_data=true", nil)
	err = refreshBatchJobMovies(c, deps, status)
	require.ErrorContains(t, err, "vanished during authoritative movie refresh")
	require.Equal(t, "snapshot", status.Results["vanished.mp4"].Movie.Title)
	require.Same(t, snapshot, result.Movie)
}
