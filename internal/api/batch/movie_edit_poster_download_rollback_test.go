package batch

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/color"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// renameFailFs fails Rename calls whose destination ends in failSuffix,
// forcing PosterManager.DownloadFromURL down its
// "remove old -full.jpg → rename temp download fails" leg with the cache's
// pre-existing full image already deleted.
type renameFailFs struct {
	afero.Fs
	failSuffix string
	renameErr  error
}

func (f *renameFailFs) Rename(oldname, newname string) error {
	if f.failSuffix != "" && strings.HasSuffix(filepath.ToSlash(newname), f.failSuffix) {
		return f.renameErr
	}
	return f.Fs.Rename(oldname, newname)
}

// TestUpdateBatchMoviePosterFromURL_DownloadFailureRestoresPreexistingAssets
// pins the poster rollback on the DownloadFromURL error leg: the download's
// finalize step REMOVES the cached {posterID}-full.jpg before its rename, so
// when the rename fails the pre-existing cache is already partially deleted
// while the (untouched) job state still references it. The handler snapshotted
// the assets up front; the failure branch must restore that snapshot instead
// of leaving the movie pointing at missing files.
func TestUpdateBatchMoviePosterFromURL_DownloadFailureRestoresPreexistingAssets(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "DLR-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Restore", Poster: models.PosterState{
			PosterURL: "https://old.example/poster.jpg",
		}},
	})

	// Seed the pre-existing cached assets (the images the stored state
	// references), then route the poster manager through an fs whose rename
	// onto the -full.jpg target fails.
	oldFull := posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0xcc, A: 0xff})
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	mem := afero.NewMemMapFs()
	tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, mem.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"), oldFull, 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"), oldPreview, 0o644))
	deps.Fs = &renameFailFs{Fs: mem, failSuffix: movieID + "-full.jpg", renameErr: errors.New("injected rename failure")}

	newFull := posterRefreshJPEG(t, 800, 500, color.RGBA{B: 0xaa, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(newFull)
	}))
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/new.jpg"})
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// "failed to finalize image download: ..." matches the handler's
	// download-failure mapping (BadGateway); what matters is the rejection
	// and the surfaced cause.
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "failed to finalize image download")

	gotFull, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"))
	require.NoError(t, readErr, "the pre-existing -full.jpg must be restored after the failed download")
	assert.Equal(t, oldFull, gotFull)
	gotPreview, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"))
	require.NoError(t, readErr, "the pre-existing preview must be restored after the failed download")
	assert.Equal(t, oldPreview, gotPreview)

	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, "https://old.example/poster.jpg", current.Movie.Poster.PosterURL,
		"a rejected poster-from-URL must not touch the stored poster state")
}
