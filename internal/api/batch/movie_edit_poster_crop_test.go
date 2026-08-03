package batch

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cropJobFixture wires a batch job with one completed movie result and a
// 1000x600 test poster stored at the full-source temp path the crop endpoint
// reads. Returns the job and a router exposing the crop and save endpoints.
func cropJobFixture(t *testing.T, movieID string) (*core.APIDeps, *worker.BatchJob, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// PosterManager resolves temp posters relative to the working directory.
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	cfg := &config.Config{System: config.SystemConfig{TempDir: "data/temp"}}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/" + movieID + ".mp4"})
	setJobResult(job, "/path/to/"+movieID+".mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/" + movieID + ".mp4", MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:    movieID,
			Title: "Crop Subject",
			Poster: models.PosterState{
				PosterURL:        "https://cdn.example/poster.jpg",
				CoverURL:         "https://cdn.example/cover.jpg",
				ShouldCropPoster: true,
			},
		},
		StartedAt: time.Now(),
	})

	writeTestPoster(t, filepath.Join("data/temp/posters", job.GetID(), movieID+"-full.jpg"), 1000, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	return deps, job, router
}

func writeTestPoster(t *testing.T, path string, w, h int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 120, G: 80, B: 40, A: 255})
		}
	}
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, jpeg.Encode(f, img, &jpeg.Options{Quality: 90}))
}

func postCrop(t *testing.T, router *gin.Engine, job *worker.BatchJob, resultID string, body contracts.PosterCropRequest) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/poster-crop", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func patchMovie(t *testing.T, router *gin.Engine, job *worker.BatchJob, resultID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PATCH", "/batch/"+job.GetID()+"/results/"+resultID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func storedMovie(t *testing.T, deps *core.APIDeps, job *worker.BatchJob, filePath string) *models.Movie {
	t.Helper()
	ej, ok := deps.JobStore.GetBatchJob(job.GetID())
	require.True(t, ok)
	res, err := ej.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, res.Movie)
	return res.Movie
}

// A crop against the full-size source stores normalized geometry on the job
// result and echoes it (plus the resulting crop intent) in the response.
func TestPosterCrop_StoresNormalizedGeometry(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPGEO-001")

	w := postCrop(t, router, job, "CROPGEO-001", contracts.PosterCropRequest{X: 100, Y: 60, Width: 400, Height: 540})
	require.Equal(t, 200, w.Code, "body: %s", w.Body.String())

	var resp contracts.PosterCropResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.PosterCropBounds, "response must echo applyable geometry")
	assert.InDelta(t, 0.1, resp.PosterCropBounds.X, 1e-9)
	assert.InDelta(t, 0.1, resp.PosterCropBounds.Y, 1e-9)
	assert.InDelta(t, 0.4, resp.PosterCropBounds.Width, 1e-9)
	assert.InDelta(t, 0.9, resp.PosterCropBounds.Height, 1e-9)
	assert.InDelta(t, 1000.0/600.0, resp.PosterCropBounds.SourceAspect, 1e-9)
	assert.False(t, resp.ShouldCropPoster, "manual crop turns the scraper crop intent off")
	assert.NotEmpty(t, resp.CroppedPosterURL)

	stored := storedMovie(t, deps, job, "/path/to/CROPGEO-001.mp4")
	require.NotNil(t, stored.Poster.PosterCropBounds, "job result must carry the geometry for organize")
	assert.Equal(t, *resp.PosterCropBounds, *stored.Poster.PosterCropBounds)
	assert.True(t, stored.Poster.PosterCropSourceFull)
	assert.False(t, stored.Poster.ShouldCropPoster)
}

// Pixel-space containment violations against the measured source must be
// rejected with 400 — no geometry is stored beyond the image edge.
func TestPosterCrop_ContainmentViolationRejected(t *testing.T) {
	for _, body := range []contracts.PosterCropRequest{
		{X: 950, Y: 0, Width: 100, Height: 100}, // x+w=1050 > 1000
		{X: 0, Y: 500, Width: 100, Height: 200}, // y+h=700 > 600
	} {
		deps, job, router := cropJobFixture(t, "CROPGEO-002")
		w := postCrop(t, router, job, "CROPGEO-002", body)
		assert.Equal(t, 400, w.Code, "body %+v must be rejected: %s", body, w.Body.String())

		stored := storedMovie(t, deps, job, "/path/to/CROPGEO-002.mp4")
		assert.Nil(t, stored.Poster.PosterCropBounds)
	}
}

// Legacy jobs (full-size source already cleaned up) keep pre-change behavior:
// the crop still refreshes the preview, but no applyable geometry is stored
// and the response reports poster_crop_bounds: null.
func TestPosterCrop_LegacyFallbackStoresNothing(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPGEO-003")
	// Remove the full source, keep only the already-cropped preview.
	require.NoError(t, os.Remove(filepath.Join("data/temp/posters", job.GetID(), "CROPGEO-003-full.jpg")))
	writeTestPoster(t, filepath.Join("data/temp/posters", job.GetID(), "CROPGEO-003.jpg"), 400, 600)

	w := postCrop(t, router, job, "CROPGEO-003", contracts.PosterCropRequest{X: 0, Y: 0, Width: 200, Height: 300})
	require.Equal(t, 200, w.Code, "body: %s", w.Body.String())

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.JSONEq(t, "null", string(raw["poster_crop_bounds"]), "legacy fallback must echo null bounds")

	stored := storedMovie(t, deps, job, "/path/to/CROPGEO-003.mp4")
	assert.Nil(t, stored.Poster.PosterCropBounds)
	assert.False(t, stored.Poster.PosterCropSourceFull)
	assert.False(t, stored.Poster.ShouldCropPoster, "crop intent behavior unchanged from pre-change")
}

// PATCH presence semantics: a movie save that omits poster_crop_bounds
// (legacy client, unrelated metadata edit) preserves stored geometry.
func TestUpdateBatchMovie_OmittedCropBoundsPreserved(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPGEO-004")
	require.Equal(t, 200, postCrop(t, router, job, "CROPGEO-004",
		contracts.PosterCropRequest{X: 0, Y: 0, Width: 400, Height: 600}).Code)

	body := `{"movie": {"code": "CROPGEO-004", "id": "CROPGEO-004", "title": "Edited Title", "poster_url": "https://cdn.example/poster.jpg", "cover_url": "https://cdn.example/cover.jpg", "should_crop_poster": false}}`
	w := patchMovie(t, router, job, "CROPGEO-004", body)
	require.Equal(t, 200, w.Code, "body: %s", w.Body.String())

	stored := storedMovie(t, deps, job, "/path/to/CROPGEO-004.mp4")
	assert.Equal(t, "Edited Title", stored.Title)
	require.NotNil(t, stored.Poster.PosterCropBounds, "omitted poster_crop_bounds must preserve stored geometry")
	assert.InDelta(t, 0.4, stored.Poster.PosterCropBounds.Width, 1e-9)
}

// An explicit poster_crop_bounds: null in a movie save clears stored geometry.
func TestUpdateBatchMovie_ExplicitNullClearsCropBounds(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPGEO-005")
	require.Equal(t, 200, postCrop(t, router, job, "CROPGEO-005",
		contracts.PosterCropRequest{X: 0, Y: 0, Width: 400, Height: 600}).Code)

	body := `{"movie": {"code": "CROPGEO-005", "id": "CROPGEO-005", "title": "Crop Subject", "poster_url": "https://cdn.example/poster.jpg", "cover_url": "https://cdn.example/cover.jpg", "should_crop_poster": false, "poster_crop_bounds": null}}`
	w := patchMovie(t, router, job, "CROPGEO-005", body)
	require.Equal(t, 200, w.Code, "body: %s", w.Body.String())

	stored := storedMovie(t, deps, job, "/path/to/CROPGEO-005.mp4")
	assert.Nil(t, stored.Poster.PosterCropBounds, "explicit null must clear stored geometry")
	assert.False(t, stored.Poster.PosterCropSourceFull)
}

// A stale overlay that re-uploads the pre-crop crop intent alongside omitted
// geometry cannot resurrect stale geometry — the stored geometry is cleared,
// restoring pre-change behavior for out-of-sync clients.
func TestUpdateBatchMovie_IntentChangeClearsCropBounds(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPGEO-006")
	require.Equal(t, 200, postCrop(t, router, job, "CROPGEO-006",
		contracts.PosterCropRequest{X: 0, Y: 0, Width: 400, Height: 600}).Code)

	body := `{"movie": {"code": "CROPGEO-006", "id": "CROPGEO-006", "title": "Crop Subject", "poster_url": "https://cdn.example/poster.jpg", "cover_url": "https://cdn.example/cover.jpg", "should_crop_poster": true}}`
	w := patchMovie(t, router, job, "CROPGEO-006", body)
	require.Equal(t, 200, w.Code, "body: %s", w.Body.String())

	stored := storedMovie(t, deps, job, "/path/to/CROPGEO-006.mp4")
	assert.Nil(t, stored.Poster.PosterCropBounds, "intent change must clear stored geometry")
	assert.True(t, stored.Poster.ShouldCropPoster, "client intent is applied")
}

// A correctly synced client re-sends the geometry with the save: unchanged
// source + intent keeps it intact (the organize-amplifier server half).
func TestUpdateBatchMovie_SyncedOverlayKeepsCropBounds(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPGEO-007")
	require.Equal(t, 200, postCrop(t, router, job, "CROPGEO-007",
		contracts.PosterCropRequest{X: 100, Y: 60, Width: 400, Height: 540}).Code)

	body := `{"movie": {"code": "CROPGEO-007", "id": "CROPGEO-007", "title": "Synced Save", "poster_url": "https://cdn.example/poster.jpg", "cover_url": "https://cdn.example/cover.jpg", "should_crop_poster": false, "poster_crop_bounds": {"x": 0.1, "y": 0.1, "width": 0.4, "height": 0.9, "source_aspect": 1.6666666666666667}, "poster_crop_source_full": true}}`
	w := patchMovie(t, router, job, "CROPGEO-007", body)
	require.Equal(t, 200, w.Code, "body: %s", w.Body.String())

	stored := storedMovie(t, deps, job, "/path/to/CROPGEO-007.mp4")
	assert.Equal(t, "Synced Save", stored.Title)
	require.NotNil(t, stored.Poster.PosterCropBounds, "synced overlay must keep geometry through save")
	assert.InDelta(t, 0.4, stored.Poster.PosterCropBounds.Width, 1e-9)
	assert.True(t, stored.Poster.PosterCropSourceFull)
}
