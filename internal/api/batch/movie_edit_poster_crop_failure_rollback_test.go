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

// truncateThenFailWriteFs lets Create through and fails the FIRST Write on
// the created file, driving CropWithBounds' encode leg
// (imageutil.cropAndWritePoster: fs.Create then jpeg.Encode) into an error.
// Post-C6 the crop is staged through {posterID}.jpg.tmp, so the poisoned
// file is the STAGING temp — the live preview is never truncated — and the
// pre-existing preview must be intact even before the handler's restore
// runs. Exactly one file is poisoned (armed=false after the first hit) so
// the handler's RestoreAssets rollback writes succeed.
type truncateThenFailWriteFs struct {
	afero.Fs
	failSuffix string
	writeErr   error
	armed      bool
}

func (f *truncateThenFailWriteFs) Create(name string) (afero.File, error) {
	file, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	if f.armed && strings.HasSuffix(filepath.ToSlash(name), f.failSuffix) {
		f.armed = false
		return &failFirstWriteFile{File: file, err: f.writeErr}, nil
	}
	return file, nil
}

type failFirstWriteFile struct {
	afero.File
	err   error
	fired bool
}

func (w *failFirstWriteFile) Write(p []byte) (int, error) {
	if !w.fired {
		w.fired = true
		return 0, w.err
	}
	return w.File.Write(p)
}

// TestUpdateBatchMoviePosterCrop_CropFailureRestoresPreview pins the asset
// rollback on the NON-legacy CropWithBounds error leg. Post-C6 the crop
// writes to a staging temp and installs by rename, so the encode failure
// poisons only {posterID}.jpg.tmp and the pre-existing preview survives the
// crop itself; the handler's RestoreAssets rollback then re-asserts the
// pre-crop snapshot as the belt-and-braces leg (parity with the
// DownloadFromURL failure leg and the UpdatePosterCrop/PersistJobByID
// compensate legs).
func TestUpdateBatchMoviePosterCrop_CropFailureRestoresPreview(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "CRP-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Crop Restore", Poster: models.PosterState{
			PosterURL:        "https://old.example/poster.jpg",
			ShouldCropPoster: true,
			CropBounds:       &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
		}},
	})

	// Seed the pre-existing cached assets the stored state references: the
	// full-size source (so the crop is NOT on the legacy-preview path) and
	// the previous preview the failed crop must not leave destroyed.
	oldFull := posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0xcc, A: 0xff})
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	mem := afero.NewMemMapFs()
	tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, mem.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"), oldFull, 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"), oldPreview, 0o644))
	deps.Fs = &truncateThenFailWriteFs{
		Fs:         mem,
		failSuffix: "/" + movieID + ".jpg.tmp",
		writeErr:   errors.New("injected encode write failure"),
		armed:      true,
	}

	body, _ := json.Marshal(contracts.PosterCropRequest{X: 10, Y: 20, Width: 300, Height: 400})
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-crop", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The injected write error reaches the client as a rejected crop
	// (non-legacy crop errors map to 400 with the underlying cause).
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "crop failed")

	// The preview the failed crop truncated must be byte-for-byte restored…
	gotPreview, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"))
	require.NoError(t, readErr, "the pre-existing preview must be restored after the failed crop")
	assert.Equal(t, oldPreview, gotPreview, "the failed crop must not leave a partial preview behind")
	// …and the full-size source must be untouched.
	gotFull, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldFull, gotFull)

	// Job state must be untouched: no crop bounds replace, no intent flip.
	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.True(t, current.Movie.Poster.ShouldCropPoster,
		"a rejected crop must not flip the persisted crop intent")
	assert.Equal(t, &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4}, current.Movie.Poster.CropBounds,
		"a rejected crop must not replace the persisted crop bounds")
}
