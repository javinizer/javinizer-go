package batch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

type nilMovieReadRepo struct {
	database.MovieRepositoryInterface
}

func (nilMovieReadRepo) FindByID(context.Context, string) (*models.Movie, error) { return nil, nil }
func (nilMovieReadRepo) FindByContentID(context.Context, string) (*models.Movie, error) {
	return nil, nil
}

type markerlessLiveJob struct{ worker.BatchJobInterface }
type markerlessJobStore struct{ worker.JobStoreInterface }

func (s markerlessJobStore) GetBatchJob(id string) (worker.BatchJobInterface, bool) {
	live, ok := s.JobStoreInterface.GetBatchJob(id)
	return markerlessLiveJob{live}, ok
}

func TestPR260RefreshNegativeInterfaceContractsDoNotPublishSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name                string
		nilRead, markerless bool
	}{
		{name: "nil successful repository read", nilRead: true},
		{name: "live job without provenance marker", markerless: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := createTestDeps(t, &config.Config{}, "")
			movie := &models.Movie{ID: "PR260-testability-refresh", ContentID: "PR260-testability-refresh", Title: "persisted"}
			_, err := deps.Repos.MovieRepo.Upsert(context.Background(), movie)
			require.NoError(t, err)
			job := deps.JobStore.CreateJobBatch([]string{"snapshot.mp4"})
			snapshot := &models.Movie{ID: movie.ID, ContentID: movie.ContentID, Title: "snapshot", Credits: []models.MovieCredit{{CreditedName: "unpublished"}}}
			result := &resultstore.MovieResult{Movie: snapshot, FileMatchInfo: models.FileMatchInfo{MovieID: movie.ID}, ResultID: "snapshot-result", Revision: 3}
			setJobResult(job, "snapshot.mp4", result)
			status := job.GetStatus()
			realRepo := deps.Repos.MovieRepo
			if tc.nilRead {
				deps.Repos.MovieRepo = nilMovieReadRepo{realRepo}
			}
			if tc.markerless {
				deps.JobStore = markerlessJobStore{deps.JobStore}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/batch/"+job.GetID(), nil)
			err = refreshBatchJobMovies(c, deps, status)
			if tc.markerless {
				require.ErrorContains(t, err, "cannot record authoritative movie refresh")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, "snapshot", status.Results["snapshot.mp4"].Movie.Title)
			require.Equal(t, "unpublished", status.Results["snapshot.mp4"].Movie.Credits[0].CreditedName)
			require.Same(t, snapshot, result.Movie)
			saved, readErr := deps.Repos.MovieRepo.FindByID(context.Background(), movie.ID)
			if tc.nilRead {
				saved, readErr = realRepo.FindByID(context.Background(), movie.ID)
			}
			require.NoError(t, readErr)
			require.Equal(t, "persisted", saved.Title)
			require.Empty(t, saved.Credits)
		})
	}
}
