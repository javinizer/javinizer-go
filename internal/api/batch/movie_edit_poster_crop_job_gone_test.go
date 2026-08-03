package batch

import (
	"bytes"
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// jobGoneStore wraps the real job store but fails every PersistJobByID with
// worker.ErrJobGone — the job-vanished race (delete between the handler's
// GetBatchJob lookup and the persist) that A13 maps to 410.
type jobGoneStore struct {
	worker.JobStoreInterface
}

func (s *jobGoneStore) PersistJobByID(string) error { return worker.ErrJobGone }

// TestUpdateBatchMoviePosterCrop_JobGoneReturns410 pins A13: when the job
// vanishes between the crop handler's lookup and its envelope persist, the
// handler must answer 410 (never a false 200) and the compensation must
// leave the poster cache untouched (the pre-crop preview bytes restored).
func TestUpdateBatchMoviePosterCrop_JobGoneReturns410(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "GONE-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Gone", Poster: models.PosterState{
			PosterURL:        "https://old.example/poster.jpg",
			ShouldCropPoster: true,
		}},
	})

	mem := afero.NewMemMapFs()
	tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, mem.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"),
		posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0xcc, A: 0xff}), 0o644))
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"), oldPreview, 0o644))
	deps.Fs = mem

	// The vanish lands between the handler's GetBatchJob lookup and its
	// PersistJobByID — modelled as the store answering ErrJobGone.
	deps.JobStore = &jobGoneStore{JobStoreInterface: deps.JobStore}

	body, _ := json.Marshal(contracts.PosterCropRequest{X: 10, Y: 20, Width: 300, Height: 400})
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-crop", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusGone, rec.Code, rec.Body.String())

	gotPreview, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldPreview, gotPreview, "the vanished-job leg must restore the pre-crop cache (A13)")
}
