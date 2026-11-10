package batch

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCancelBatchJobCleanupWaitsForCompletionOrDeadline(t *testing.T) {
	for _, completes := range []bool{true, false} {
		name := "deadline"
		if completes {
			name = "completion"
		}
		t.Run(name, func(t *testing.T) {
			deps := createTestDeps(t, &config.Config{}, "")
			fs := afero.NewMemMapFs()
			deps.Fs = fs
			rt := testkit.GetTestRuntime(deps)
			synctest.Test(t, func(t *testing.T) {
				const jobID = "12345678-1234-1234-1234-123456789abc"
				root := filepath.Join(string(filepath.Separator), "cancel-test")
				posterDir := filepath.Join(root, "posters", jobID)
				otherPoster := filepath.Join(root, "posters", "other-job", "poster.jpg")
				require.NoError(t, fs.MkdirAll(posterDir, 0755))
				require.NoError(t, afero.WriteFile(fs, filepath.Join(posterDir, "poster.jpg"), []byte("poster"), 0644))
				require.NoError(t, fs.MkdirAll(filepath.Dir(otherPoster), 0755))
				require.NoError(t, afero.WriteFile(fs, otherPoster, []byte("preserve"), 0644))
				done := make(chan struct{})
				job := workermocks.NewMockBatchJobInterface(t)
				job.EXPECT().GetStatus().Return(&worker.BatchJobStatus{Status: models.JobStatusRunning, TempDir: root}).Twice()
				job.EXPECT().Cancel().Once()
				job.EXPECT().Done().Return((<-chan struct{})(done)).Once()
				job.EXPECT().GetID().Return(jobID)
				deps.JobStore = &excludeEdgeStore{job: job}
				router := gin.New()
				router.POST("/batch/:id/cancel", cancelBatchJob(rt))
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/cancel", nil))
				require.Equal(t, http.StatusOK, response.Code)
				synctest.Wait()
				time.Sleep(4 * time.Second)
				synctest.Wait()
				exists, err := afero.DirExists(fs, posterDir)
				require.NoError(t, err)
				require.True(t, exists, "posters must remain while the cancelled worker has not stopped")
				if completes {
					close(done)
				} else {
					time.Sleep(time.Second)
				}
				synctest.Wait()
				exists, err = afero.Exists(fs, posterDir)
				require.NoError(t, err)
				require.False(t, exists, "cleanup must run on completion or after the five-second deadline")
				data, err := afero.ReadFile(fs, otherPoster)
				require.NoError(t, err)
				require.Equal(t, "preserve", string(data))
			})
		})
	}
}
