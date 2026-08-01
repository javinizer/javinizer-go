package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// twoToneCoverJPEG builds a 1000x600 landscape cover whose left half is red
// and right half is blue, so a crop's provenance is pixel-verifiable:
// manual left-side crop → red, default right-side auto-crop → blue.
func twoToneCoverJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			if x < 500 {
				img.Set(x, y, color.RGBA{R: 220, G: 30, B: 30, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 30, G: 30, B: 220, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}))
	return buf.Bytes()
}

// requireManualCropImage asserts the image at path is exactly the manual
// left-side crop: 400x600 portrait and red-dominant. A full uncropped cover
// (1000x600) or the default right-side auto-crop (blue) fails the test.
func requireManualCropImage(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	require.NoError(t, err)
	b := img.Bounds()
	require.Equal(t, 400, b.Dx(), "poster width should be the manual crop width (not full cover or default crop)")
	require.Equal(t, 600, b.Dy(), "poster height should be the manual crop height")
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	require.Greater(t, r, bl, "poster must come from the manual (left/red) crop region, not the default (right/blue) auto-crop")
}

func findMoviePosterFile(t *testing.T, root string) string {
	t.Helper()
	var found string
	require.NoError(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(info.Name()) == ".jpg" && bytes.HasSuffix([]byte(info.Name()), []byte("-poster.jpg")) {
			found = p
		}
		return nil
	}))
	require.NotEmpty(t, found, "no *-poster.jpg found under %s", root)
	return found
}

// TestPosterCropSurvivesOrganize_E2E is the end-to-end regression guard for
// the review-page manual poster crop: after re-cropping in the UI, Organize
// must write the USER's crop to disk — not the uncropped cover and not the
// scraper's default auto-crop.
func TestPosterCropSurvivesOrganize_E2E(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	// data/temp is relative to CWD for DefaultConfig — isolate in temp dir.
	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	coverBytes := twoToneCoverJPEG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(coverBytes)
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	tmpRoot := t.TempDir()
	ctx := context.Background()

	setupScrapedJob := func(t *testing.T, movieID string, idx int) (*worker.BatchJob, string) {
		t.Helper()
		srcDir := filepath.Join(tmpRoot, fmt.Sprintf("src%d", idx))
		require.NoError(t, os.MkdirAll(srcDir, 0o755))
		filePath := filepath.Join(srcDir, movieID+".mp4")
		require.NoError(t, os.WriteFile(filePath, []byte("dummy video"), 0o644))

		job := createJobWithWF(deps, cfg, []string{filePath})
		setJobResult(job, filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{
				ID:    movieID,
				Title: "E2E " + movieID,
				Poster: models.PosterState{
					PosterURL:        srv.URL + "/cover.jpg",
					CoverURL:         srv.URL + "/cover.jpg",
					ShouldCropPoster: true,
				},
			},
		})

		// Scrape-time temp poster artifacts: the full downloaded cover.
		posterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(posterDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(posterDir, movieID+"-full.jpg"), coverBytes, 0o644))
		return job, filePath
	}

	postManualCrop := func(t *testing.T, job *worker.BatchJob, movieID string) {
		t.Helper()
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
		// Manual crop: LEFT (red) half — disjoint from the default right-side crop.
		body, err := json.Marshal(contracts.PosterCropRequest{X: 0, Y: 0, Width: 400, Height: 600})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-crop", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp contracts.PosterCropResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.CroppedPosterURL)
	}

	organize := func(t *testing.T, job *worker.BatchJob, dest string) {
		t.Helper()
		require.NoError(t, testStartOrganizeApply(ctx, job, deps.JobStore, dest, false, "", true, false,
			deps.CoreDeps.DB, cfg, deps.CoreDeps.ScraperRegistry, nil))
		for fp, r := range job.GetStatus().Results {
			require.Equal(t, models.JobStatusCompleted, r.Status, "file %s should be organized (err=%s)", fp, r.Error)
		}
	}

	t.Run("manual crop is written to disk at organize", func(t *testing.T) {
		job, _ := setupScrapedJob(t, "E2EA-001", 1)
		postManualCrop(t, job, "E2EA-001")
		dest := t.TempDir()
		organize(t, job, dest)
		requireManualCropImage(t, findMoviePosterFile(t, dest))
	})

	t.Run("manual crop survives a metadata save round-trip before organize", func(t *testing.T) {
		job, _ := setupScrapedJob(t, "E2EB-002", 2)
		postManualCrop(t, job, "E2EB-002")

		// The review page saves pending metadata edits right before organizing
		// (organize-controller saveAllEdits). The client PATCHes back the movie
		// it was shown — the crop state must survive that round-trip.
		var current *models.Movie
		for _, r := range job.GetStatus().Results {
			current = r.Movie
		}
		require.NotNil(t, current)
		view := contracts.MovieViewFromModel(current)
		view.Title = "E2E edited title"
		body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
		require.NoError(t, err)

		router := gin.New()
		router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/E2EB-002", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		dest := t.TempDir()
		organize(t, job, dest)
		requireManualCropImage(t, findMoviePosterFile(t, dest))
	})
}
