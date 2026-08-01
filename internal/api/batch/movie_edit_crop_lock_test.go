package batch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// noisyJPEG renders a high-entropy grayscale JPEG of the given size. Unlike
// the solid-color posterRefreshJPEG fixtures, noise defeats JPEG block
// deduplication, so decoding takes a human-timescale-long time — used to
// widen a request's source-read window in race tests.
func noisyJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(0x5eed))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(rng.Intn(256))
			img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 0xff})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}))
	return buf.Bytes()
}

// assertPreviewDerivedFromNewImage proves the review preview ({movieID}.jpg)
// was produced from the solid-blue /new.jpg fixture image: JPEG encoding
// keeps solid colors within a few units, while a preview cut from the noisy
// gray /old.jpg (the pre-edit source) is obviously non-uniform. This is the
// observable that pins Finding A's interleave — in a racy crop, the preview
// is cut from the pre-edit bytes yet gets attached to the movie at the
// post-edit URL (recorded bound dimensions alone cannot show it:
// CropWithBounds re-reads the source size AFTER the refresh swapped the
// file, which masks the stale measurement).
func assertPreviewDerivedFromNewImage(t *testing.T, previewPath string) {
	t.Helper()
	f, err := os.Open(previewPath)
	require.NoError(t, err, "the final preview must exist in every serialized order")
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	require.NoError(t, err)
	b := img.Bounds()
	samples := [][2]int{{b.Min.X, b.Min.Y}, {b.Min.X + b.Dx()/2, b.Min.Y + b.Dy()/2}, {b.Max.X - 1, b.Max.Y - 1}}
	for _, pt := range samples {
		r, g, bl, _ := img.At(pt[0], pt[1]).RGBA()
		// /new.jpg is solid RGB(0x20, 0x40, 0xcc).
		assert.InDelta(t, 0x20, r>>8, 12, "preview pixel %v must come from the post-edit image (not the pre-edit crop source)", pt)
		assert.InDelta(t, 0x40, g>>8, 12, "preview pixel %v must come from the post-edit image", pt)
		assert.InDelta(t, 0xcc, bl>>8, 12, "preview pixel %v must come from the post-edit image", pt)
	}
}

// postPosterCropBounds issues one manual-crop request with a small region that
// is valid for every fixture image in this file and returns the HTTP status.
func postPosterCropBounds(router *gin.Engine, jobID, resultID string) int {
	req := httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/results/"+resultID+"/poster-crop",
		bytes.NewBufferString(`{"x":10,"y":10,"width":200,"height":200}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// TestUpdateBatchMoviePosterCrop_SerializesWithPosterSourcePATCH is the race
// from Finding A: a manual crop and a whole-movie PATCH that swaps the poster
// source run concurrently against the same job/movie. The crop endpoint now
// holds the shared per-(jobID, movieID) lock across cached-source resolution,
// CropWithBounds, the state update, and persistence — so the refresh+persist
// sequences cannot interleave. The invariant pinned here: after both requests
// finish, the persisted poster URL, the cached -full.jpg, and any recorded
// crop bounds ALL describe the same (post-edit) image — no bounds measured
// against the pre-edit image may survive on the post-edit source.
func TestUpdateBatchMoviePosterCrop_SerializesWithPosterSourcePATCH(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	// Distinct dimensions: bounds measured against /old.jpg (2000x3000) can
	// never be mistaken for bounds measured against /new.jpg (1000x1500). The
	// old image is a large NOISY jpeg (~100ms to decode) so the crop's
	// source-read → state-update window is far wider than the PATCH refresh's
	// 15ms download delay: without the shared lock, a crop that starts inside
	// that delay reads the pre-edit image yet persists after the PATCH — the
	// exact interleave from the finding.
	oldJPEG := noisyJPEG(t, 2000, 3000)
	newJPEG := posterRefreshJPEG(t, 1000, 1500, color.RGBA{R: 0x20, G: 0x40, B: 0xcc, A: 0xff})
	require.NotEqual(t, oldJPEG, newJPEG)
	mux := http.NewServeMux()
	serve := func(img []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(15 * time.Millisecond) // widen the refresh window so the two requests overlap
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(img)
		}
	}
	mux.HandleFunc("/old.jpg", serve(oldJPEG))
	mux.HandleFunc("/new.jpg", serve(newJPEG))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	urlNew := srv.URL + "/new.jpg"

	// Several rounds raise the chance the crop's source read actually
	// overlaps the PATCH refresh window; every round must still converge to
	// the invariant.
	for round := 0; round < 6; round++ {
		movieID := fmt.Sprintf("CRACE-%03d", round)
		filePath := "/path/to/" + movieID + ".mp4"

		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{filePath})
		setJobResult(job, filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: movieID, Title: "Race", Poster: models.PosterState{
				PosterURL: srv.URL + "/old.jpg",
			}},
		})
		tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
		fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
		require.NoError(t, os.WriteFile(fullPath, oldJPEG, 0o644))

		router := gin.New()
		router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
		router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

		var wg sync.WaitGroup
		cropCode := make(chan int, 1)
		patchCode := make(chan int, 1)
		wg.Add(2)
		// Start the PATCH first and the crop inside its refresh window (the
		// download delay): without the shared lock the crop reads the pre-edit
		// -full.jpg yet persists after the PATCH; with it, the crop blocks on
		// the lock until the PATCH has finished refreshing and persisting.
		go func() { defer wg.Done(); patchCode <- patchPosterURL(t, router, job.GetID(), movieID, urlNew) }()
		time.Sleep(3 * time.Millisecond)
		go func() { defer wg.Done(); cropCode <- postPosterCropBounds(router, job.GetID(), movieID) }()
		wg.Wait()

		require.Equal(t, http.StatusOK, <-cropCode, "round %d: the crop is valid against either image", round)
		require.Equal(t, http.StatusOK, <-patchCode, "round %d", round)

		// The PATCH is the last writer of the poster URL under every
		// serialized order (UpdatePosterCrop never touches PosterURL).
		assertCachedPosterMatchesStoredURL(t, job, movieID, fullPath, map[string][]byte{urlNew: newJPEG})

		current := storedMovieResult(t, job, movieID)
		require.NotNil(t, current.Movie)
		if b := current.Movie.Poster.CropBounds; b != nil {
			// The crop persisted after the PATCH: bounds must have been measured
			// against the post-edit image — never the pre-edit 2000x3000 one.
			assert.Equal(t, 1000, b.ImageWidth, "round %d: bounds measured against the pre-edit image survived on the post-edit source", round)
			assert.Equal(t, 1500, b.ImageHeight, "round %d", round)
			assert.False(t, b.SourceWasCover, "round %d: a poster-grade source records poster intent", round)
		}
		// In every serialized order the final preview is derived from the
		// post-edit image: either the crop measured and cut /new.jpg after
		// the PATCH completed, or the PATCH's own refresh rewrote the preview
		// from /new.jpg after clearing the crop's pre-edit output. A racy
		// crop instead leaves a preview cut from the pre-edit noisy image
		// attached to the post-edit source.
		assertPreviewDerivedFromNewImage(t, filepath.Join(tempPosterDir, movieID+".jpg"))
		// A nil CropBounds is the other legitimate order: the crop persisted
		// first and the PATCH's source-change invalidation cleared it.

		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	}
}

// TestUpdateBatchMoviePosterCrop_TakesSharedPosterSourceLock proves the crop
// endpoint contends on the SAME per-(jobID, movieID) lock the PATCH and
// field-override paths use: while the test goroutine holds it, a crop request
// cannot complete; once released, it proceeds. This is the deterministic
// complement to the race test above — it pins the lock acquisition itself,
// regardless of timing luck.
func TestUpdateBatchMoviePosterCrop_TakesSharedPosterSourceLock(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieID = "CBLK-001"
	job := createJobWithWF(deps, cfg, []string{"/path/to/CBLK-001.mp4"})
	setJobResult(job, "/path/to/CBLK-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/CBLK-001.mp4", MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Block"},
	})
	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 1000, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	release := worker.AcquirePosterSourceLock(job.GetID(), movieID)
	done := make(chan int, 1)
	go func() { done <- postPosterCropBounds(router, job.GetID(), movieID) }()

	select {
	case code := <-done:
		release()
		t.Fatalf("crop completed (%d) while the shared poster-source lock was held", code)
	case <-time.After(150 * time.Millisecond):
	}
	release()

	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("crop did not proceed after the lock was released")
	}
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterCrop_PosterSourceLockReleasedOnAllPaths is the
// ordering/deadlock-safety table for the crop endpoint: it takes ONLY the
// shared poster-source lock (no overrideMu, matching updateBatchMovie;
// ApplyFieldOverride's overrideMu is always taken before this lock and never
// after), so every outcome — success, legacy rejection, a failed state
// update, or the result vanishing mid-request — must leave the per-
// (jobID, movieID) lock free. A leaked release would deadlock every future
// poster edit for that movie.
func TestUpdateBatchMoviePosterCrop_PosterSourceLockReleasedOnAllPaths(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	newRouter := func(deps *core.APIDeps) *gin.Engine {
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
		return router
	}

	seedFullPoster := func(t *testing.T, jobID, movieID string) {
		t.Helper()
		posterDir := filepath.Join("data", "temp", "posters", jobID)
		require.NoError(t, os.MkdirAll(posterDir, 0o755))
		writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 1000, 600)
	}

	t.Run("successful crop releases the lock", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		const movieID = "COK-001"
		job := createJobWithWF(deps, cfg, []string{"/path/to/COK-001.mp4"})
		setJobResult(job, "/path/to/COK-001.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/COK-001.mp4", MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Title: "Crop OK"},
		})
		seedFullPoster(t, job.GetID(), movieID)

		require.Equal(t, http.StatusOK, postPosterCropBounds(newRouter(deps), job.GetID(), movieID))
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})

	t.Run("legacy job without full-size source releases the lock", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		const movieID = "CLG-001"
		job := createJobWithWF(deps, cfg, []string{"/path/to/CLG-001.mp4"})
		setJobResult(job, "/path/to/CLG-001.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/CLG-001.mp4", MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Title: "Legacy Crop"},
		})
		// Only the already-cropped preview exists: CropWithBounds rejects.
		posterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(posterDir, 0o755))
		writeJPEG(t, filepath.Join(posterDir, movieID+".jpg"), 900, 600)

		require.Equal(t, http.StatusBadRequest, postPosterCropBounds(newRouter(deps), job.GetID(), movieID))
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})

	t.Run("state update failure releases the lock", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		const jobID, movieID = "job-lock-fail", "CFL-001"
		result := &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/CFL-001.mp4", MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Title: "Fail"},
		}
		mockJob := workermocks.NewMockBatchJobInterface(t)
		// Exactly two lookups: the pre-lock one and the post-lock re-read.
		mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, "/path/to/CFL-001.mp4", true).Twice()
		mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/CFL-001.mp4"})
		mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(result, nil)
		mockJob.EXPECT().UpdatePosterCrop(movieID, mock.Anything, mock.Anything).Return(assert.AnError)
		deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}
		seedFullPoster(t, jobID, movieID)

		require.Equal(t, http.StatusInternalServerError, postPosterCropBounds(newRouter(deps), jobID, movieID))
		assertPosterSourceLockFreeAPI(t, jobID, movieID)
	})

	t.Run("result vanishing after the lock falls back to the pre-lock snapshot", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		const jobID, movieID = "job-lock-gone", "CVN-001"
		result := &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/CVN-001.mp4", MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: movieID, Title: "Vanish", Poster: models.PosterState{
				PosterURL:        "https://example.com/cover.jpg",
				ShouldCropPoster: true, // recorded as SourceWasCover from the pre-lock snapshot
			}},
		}
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, "/path/to/CVN-001.mp4", true).Once()
		// The source edit won the race AND the result went away before the
		// post-lock re-read: the endpoint proceeds with the pre-lock state.
		mockJob.EXPECT().GetFileResultByResultID(movieID).Return(nil, "", false).Once()
		mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/CVN-001.mp4"})
		mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(result, nil)
		mockJob.EXPECT().UpdatePosterCrop(movieID, mock.Anything, mock.MatchedBy(func(b *models.CropBounds) bool {
			return b != nil && b.SourceWasCover
		})).Return(nil)
		deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}
		seedFullPoster(t, jobID, movieID)

		require.Equal(t, http.StatusOK, postPosterCropBounds(newRouter(deps), jobID, movieID))
		assertPosterSourceLockFreeAPI(t, jobID, movieID)
	})
}

// TestUpdateBatchMovie_PosterSourceChangeSyncsCropIntent pins the intent sync
// in the whole-movie PATCH path: when a PATCH replaces the effective poster
// source, ShouldCropPoster follows the NEW image. A recorded scraper source
// whose own effective poster URL IS the adopted image contributes its raw
// decision (javdb/mgstage populate PosterURL from a landscape CoverURL with
// ShouldCropPoster=true); otherwise the URL-class fallback applies, so a
// stale cover intent never survives onto a poster-grade image (it would be
// recorded as CropBounds.SourceWasCover by a later manual crop, degrading the
// apply-time geometry fallback to the default cover crop), and a cleared
// poster URL regains cover-backed semantics. A PATCH that does NOT change the
// source leaves an explicitly sent flag untouched.
func TestUpdateBatchMovie_PosterSourceChangeSyncsCropIntent(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)
	oldURL, newURL := srv.URL+"/old.jpg", srv.URL+"/new.jpg"

	setup := func(t *testing.T, movieID string, poster models.PosterState) (*worker.BatchJob, *gin.Engine) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{"/path/to/" + movieID + ".mp4"})
		setJobResult(job, "/path/to/"+movieID+".mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/" + movieID + ".mp4", MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Title: "Intent", Poster: poster},
		})
		router := gin.New()
		router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
		return job, router
	}

	patch := func(t *testing.T, router *gin.Engine, jobID, movieID string, poster models.PosterState) {
		view := contracts.MovieViewFromModel(&models.Movie{ID: movieID, Title: "Intent", Poster: poster})
		body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/"+movieID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}

	t.Run("cover-backed movie saved with a poster URL drops the cover intent", func(t *testing.T) {
		const movieID = "SYN-001"
		job, router := setup(t, movieID, models.PosterState{
			CoverURL:         oldURL,
			ShouldCropPoster: true, // cover-backed at scrape time
			CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 400},
		})
		// The client resends the still-stale flag alongside the new source.
		patch(t, router, job.GetID(), movieID, models.PosterState{
			PosterURL: newURL, CoverURL: oldURL, ShouldCropPoster: true,
			CropBounds: &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 400},
		})

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.False(t, stored.Movie.Poster.ShouldCropPoster,
			"the effective source is now poster-grade — the cover intent must not survive")
		assert.Nil(t, stored.Movie.Poster.CropBounds, "source change still invalidates the old crop")
	})

	t.Run("cleared poster URL regains cover-backed intent", func(t *testing.T) {
		const movieID = "SYN-002"
		job, router := setup(t, movieID, models.PosterState{
			PosterURL:        oldURL,
			CoverURL:         newURL,
			ShouldCropPoster: false,
		})
		patch(t, router, job.GetID(), movieID, models.PosterState{
			PosterURL: "", CoverURL: newURL, ShouldCropPoster: false,
		})

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.True(t, stored.Movie.Poster.ShouldCropPoster,
			"the cover feeds the poster pipeline again — default cover-crop semantics return")
	})

	t.Run("poster URL swap with a stale flag re-derives poster-grade intent", func(t *testing.T) {
		const movieID = "SYN-003"
		job, router := setup(t, movieID, models.PosterState{
			PosterURL:        oldURL,
			ShouldCropPoster: false,
		})
		patch(t, router, job.GetID(), movieID, models.PosterState{
			PosterURL: newURL, ShouldCropPoster: true, // stale client flag
		})

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.False(t, stored.Movie.Poster.ShouldCropPoster)
	})

	t.Run("PATCH selecting a cover-derived poster URL adopts the recorded source intent", func(t *testing.T) {
		const movieID = "SYN-005"
		job, router := setup(t, movieID, models.PosterState{
			PosterURL:        oldURL,
			ShouldCropPoster: false, // poster-grade source currently feeds the pipeline
		})
		filePath := "/path/to/" + movieID + ".mp4"
		// javdb/mgstage output shape: PosterURL is populated FROM the
		// landscape CoverURL and flagged ShouldCropPoster=true (provenance
		// carries the scraper's raw decision for the review source viewer).
		job.ResultsWriter().SetProvenance(filePath, &resultstore.ProvenanceData{
			ScraperResults: []*models.ScraperResult{
				{Source: "javdb", PosterURL: newURL, CoverURL: newURL, ShouldCropPoster: true},
				{Source: "dmm", PosterURL: "https://dmm.example/pl.jpg", ShouldCropPoster: false},
			},
		})
		// The client resends a poster-grade flag alongside the selected source.
		patch(t, router, job.GetID(), movieID, models.PosterState{
			PosterURL: newURL, CoverURL: oldURL, ShouldCropPoster: false,
		})

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.True(t, stored.Movie.Poster.ShouldCropPoster,
			"the selected source's landscape poster keeps its crop intent — the temp preview is auto-cropped, so Organize must crop too")
		assert.Nil(t, stored.Movie.Poster.CropBounds, "the source change still invalidates the old crop")
	})

	t.Run("recorded source intent is not leaked onto a different adopted image", func(t *testing.T) {
		const movieID = "SYN-006"
		job, router := setup(t, movieID, models.PosterState{
			CoverURL:         oldURL,
			ShouldCropPoster: true,
		})
		filePath := "/path/to/" + movieID + ".mp4"
		// dmm's intent describes its OWN poster URL (pl.jpg); the PATCH adopts
		// only its cover, so the URL-class fallback applies to the new image.
		job.ResultsWriter().SetProvenance(filePath, &resultstore.ProvenanceData{
			ScraperResults: []*models.ScraperResult{
				{Source: "dmm", PosterURL: "https://dmm.example/pl.jpg", CoverURL: newURL, ShouldCropPoster: false},
			},
		})
		patch(t, router, job.GetID(), movieID, models.PosterState{
			PosterURL: "", CoverURL: newURL, ShouldCropPoster: false,
		})

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.True(t, stored.Movie.Poster.ShouldCropPoster,
			"the adopted cover feeds the poster pipeline — cover-backed fallback applies")
	})

	t.Run("cover_url PATCH after a manual crop re-establishes cover-backed intent", func(t *testing.T) {
		const movieID = "SYN-007"
		job, router := setup(t, movieID, models.PosterState{
			CoverURL:         oldURL,
			ShouldCropPoster: false, // the manual crop's decision on the OLD cover
			CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 400},
		})
		// No recorded provenance describes the new cover: the URL-class
		// fallback must still restore cover-crop semantics.
		patch(t, router, job.GetID(), movieID, models.PosterState{
			PosterURL: "", CoverURL: newURL, ShouldCropPoster: false,
		})

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.True(t, stored.Movie.Poster.ShouldCropPoster,
			"the new cover is the effective poster source again — Organize must crop it")
		assert.Nil(t, stored.Movie.Poster.CropBounds, "the source change invalidates the old manual crop")
	})

	t.Run("unchanged source keeps an explicit flag flip", func(t *testing.T) {
		const movieID = "SYN-004"
		job, router := setup(t, movieID, models.PosterState{
			PosterURL:        oldURL,
			ShouldCropPoster: false,
			CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 400},
		})
		patch(t, router, job.GetID(), movieID, models.PosterState{
			PosterURL:        oldURL,
			ShouldCropPoster: true, // deliberate decision on the SAME source
			CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 400},
		})

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.True(t, stored.Movie.Poster.ShouldCropPoster,
			"a source-preserving PATCH must not clobber a deliberate crop decision")
		assert.Nil(t, stored.Movie.Poster.CropBounds, "the crop-decision change still invalidates stored bounds")
	})
}
