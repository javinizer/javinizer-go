package batch

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// storedMovieResult reads back the result for movieID from a real BatchJob
// (the interface getter for resultID lookups is not exposed on *BatchJob).
func storedMovieResult(t *testing.T, job *worker.BatchJob, movieID string) *resultstore.MovieResult {
	t.Helper()
	for _, r := range job.GetStatus().Results {
		if r != nil && r.FileMatchInfo.MovieID == movieID {
			return r
		}
	}
	t.Fatalf("no movie result for %s", movieID)
	return nil
}

// posterRefreshJPEG renders a solid-color JPEG of the given size. Distinct
// colors keep the pre-PATCH and post-PATCH fixtures byte-distinguishable.
func posterRefreshJPEG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

// patchPosterSourceServer serves a stable old and new poster image (and a
// broken endpoint) for the whole-movie PATCH refresh tests. newHits counts
// /new.jpg fetches so no-op cases can prove no refresh happened.
type patchPosterSourceServer struct {
	*httptest.Server
	oldJPEG []byte
	newJPEG []byte
	newHits int
}

func newPatchPosterSourceServer(t *testing.T) *patchPosterSourceServer {
	t.Helper()
	s := &patchPosterSourceServer{
		oldJPEG: posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0xcc, A: 0xff}),
		newJPEG: posterRefreshJPEG(t, 800, 500, color.RGBA{B: 0xaa, A: 0xff}),
	}
	require.NotEqual(t, s.oldJPEG, s.newJPEG, "fixture images must be distinguishable")
	mux := http.NewServeMux()
	mux.HandleFunc("/old.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(s.oldJPEG)
	})
	mux.HandleFunc("/new.jpg", func(w http.ResponseWriter, _ *http.Request) {
		s.newHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(s.newJPEG)
	})
	mux.HandleFunc("/broken.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Server.Close)
	return s
}

// allowTestHTTPServerURL makes the SSRF URL check pass for the httptest
// server (loopback) — the same pattern the poster-from-url tests use.
func allowTestHTTPServerURL(t *testing.T) {
	t.Helper()
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)
}

// chdirWorkDir isolates the relative default TempDir ("data/temp") per test,
// matching the other movie_edit poster tests.
func chdirWorkDir(t *testing.T) string {
	t.Helper()
	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	return workDir
}

// TestUpdateBatchMovie_PosterSourceChangeRefreshesCachedAssets pins Finding A:
// a whole-movie PATCH that changes the effective poster source (poster_url, or
// cover_url while no poster URL is set) regenerates the cached
// {jobID}/{movie.ID}-full.jpg before the new URLs are persisted — otherwise a
// later manual crop measures the stale pre-PATCH image while Organize
// downloads the persisted one. Unchanged sources (including a cover change
// behind an explicit poster URL) are a no-op, and a failed refresh rejects
// the edit without touching job state or the cache.
func TestUpdateBatchMovie_PosterSourceChangeRefreshesCachedAssets(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)

	const sentinel = "stale-cache-sentinel"

	cases := []struct {
		name            string
		storedPosterURL string // "OLD"/"NEW"/"BROKEN" resolve against the fixture server; "" = unset
		storedCoverURL  string
		patchPosterURL  string
		patchCoverURL   string
		wantRefreshed   bool   // the PATCH switches the effective source to /new.jpg
		seeded          []byte // bytes seeded into -full.jpg before the PATCH (nil = oldJPEG)
		wantCode        int    // 0 = http.StatusOK
		wantErrContains string // asserted against the response body when set
		wantNewHits     int
		wantBoundsKept  bool // whether the stored crop bounds must survive the PATCH
	}{
		{
			name:            "poster_url change refreshes the cached full image",
			storedPosterURL: "OLD",
			patchPosterURL:  "NEW",
			wantRefreshed:   true,
			wantNewHits:     1,
		},
		{
			name:           "cover_url change without a poster refreshes from the cover",
			storedCoverURL: "OLD",
			patchCoverURL:  "NEW",
			wantRefreshed:  true,
			wantNewHits:    1,
		},
		{
			name:            "identical poster_url leaves the cache untouched",
			storedPosterURL: "OLD",
			patchPosterURL:  "OLD",
			seeded:          []byte(sentinel),
			wantBoundsKept:  true,
		},
		{
			name:            "cover_url change behind an explicit poster leaves the cache untouched",
			storedPosterURL: "OLD",
			storedCoverURL:  "OLD",
			patchPosterURL:  "OLD",
			patchCoverURL:   "NEW",
			seeded:          []byte(sentinel),
			wantBoundsKept:  true,
		},
		{
			name:            "failed refresh rejects the edit and keeps job state and cache intact",
			storedPosterURL: "OLD",
			patchPosterURL:  "BROKEN",
			wantCode:        http.StatusInternalServerError,
			wantErrContains: "Failed to refresh poster source",
			wantBoundsKept:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			chdirWorkDir(t)

			resolveURL := func(marker string) string {
				switch marker {
				case "OLD":
					return srv.URL + "/old.jpg"
				case "NEW":
					return srv.URL + "/new.jpg"
				case "BROKEN":
					return srv.URL + "/broken.jpg"
				default:
					return ""
				}
			}
			storedPosterURL := resolveURL(tt.storedPosterURL)
			storedCoverURL := resolveURL(tt.storedCoverURL)
			patchPosterURL := resolveURL(tt.patchPosterURL)
			patchCoverURL := resolveURL(tt.patchCoverURL)

			cfg := config.DefaultConfig(nil, nil)
			deps := createTestDeps(t, cfg, "")
			const movieID = "PATCH-001"
			job := createJobWithWF(deps, cfg, []string{"/path/to/" + movieID + ".mp4"})
			storedBounds := &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4}
			setJobResult(job, "/path/to/"+movieID+".mp4", &resultstore.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: "/path/to/" + movieID + ".mp4", MovieID: movieID},
				Status:        models.JobStatusCompleted,
				Movie: &models.Movie{ID: movieID, Title: "Patch", Poster: models.PosterState{
					PosterURL: storedPosterURL, CoverURL: storedCoverURL, CropBounds: storedBounds,
				}},
			})

			// Seed the scraped full-size source the crop endpoint would hit.
			tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
			require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
			fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
			seeded := tt.seeded
			if seeded == nil {
				seeded = srv.oldJPEG
			}
			require.NoError(t, os.WriteFile(fullPath, seeded, 0o644))

			hitsBefore := srv.newHits

			body := fmt.Sprintf(`{"movie":{"id":"%s","title":"Patch","poster_url":%q,"cover_url":%q}}`,
				movieID, patchPosterURL, patchCoverURL)
			router := gin.New()
			router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
			req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			wantCode := tt.wantCode
			if wantCode == 0 {
				wantCode = http.StatusOK
			}
			require.Equal(t, wantCode, rec.Code, rec.Body.String())
			if tt.wantErrContains != "" {
				assert.Contains(t, rec.Body.String(), tt.wantErrContains)
			}

			wantFull := seeded
			if tt.wantRefreshed {
				wantFull = srv.newJPEG
			}
			content, readErr := os.ReadFile(fullPath)
			require.NoError(t, readErr)
			assert.True(t, bytes.Equal(wantFull, content),
				"-full.jpg contents after PATCH = %x, want %x", content, wantFull)
			assert.Equal(t, hitsBefore+tt.wantNewHits, srv.newHits,
				"/new.jpg fetch count must match the refresh decision")

			current := storedMovieResult(t, job, movieID)
			require.NotNil(t, current.Movie)
			if wantCode == http.StatusOK {
				assert.Equal(t, patchPosterURL, current.Movie.Poster.PosterURL)
				assert.Equal(t, patchCoverURL, current.Movie.Poster.CoverURL)
			} else {
				// Rejected edit: everything stays at the stored values.
				assert.Equal(t, storedPosterURL, current.Movie.Poster.PosterURL)
				assert.Equal(t, storedCoverURL, current.Movie.Poster.CoverURL)
			}
			if tt.wantBoundsKept {
				require.NotNil(t, current.Movie.Poster.CropBounds, "the crop measured against the still-cached image must survive")
				assert.Equal(t, *storedBounds, *current.Movie.Poster.CropBounds)
			} else {
				assert.Nil(t, current.Movie.Poster.CropBounds, "a source change must invalidate bounds measured against the old image")
			}
		})
	}
}

// TestUpdateBatchMovie_PosterRefreshRollbackOnPersistFailure guards the
// filesystem/job-state invariant for the whole-movie PATCH path: if the
// poster refresh succeeded but the UpdateMovie persistence then fails, the
// cached -full.jpg and preview are restored to the pre-refresh bytes so a
// subsequent crop does not measure the new image against the job's old
// source URL (parity with the field-override rollback).
func TestUpdateBatchMovie_PosterRefreshRollbackOnPersistFailure(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "RBK-001"
	filePath := "/path/to/" + movieID + ".mp4"
	result := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Rollback", Poster: models.PosterState{
			PosterURL:  srv.URL + "/old.jpg",
			CropBounds: &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
		}},
	}

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, filePath, true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{filePath})
	mockJob.EXPECT().UpdateMovie(mock.Anything, filePath, mock.Anything).Return(assert.AnError)
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	const jobID = "job-any"
	tempPosterDir := filepath.Join("data", "temp", "posters", jobID)
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	previewPath := filepath.Join(tempPosterDir, movieID+".jpg")
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(fullPath, srv.oldJPEG, 0o644))
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":"%s","title":"Rollback","poster_url":%q}}`, movieID, srv.URL+"/new.jpg")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Failed to update movie")
	assert.NotContains(t, rec.Body.String(), "poster rollback failed")
	assert.Equal(t, 1, srv.newHits, "sanity: the refresh really downloaded the new image before persistence failed")

	full, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(srv.oldJPEG, full),
		"-full.jpg must be restored when persistence fails, got %x want %x", full, srv.oldJPEG)
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(oldPreview, preview),
		"preview must be restored when persistence fails, got %x want %x", preview, oldPreview)
}

// TestUpdateBatchMovie_PosterRefreshRollbackFailureSurfaced ensures a failed
// cache rollback is not swallowed on the whole-movie PATCH path either: the
// persist error stays primary and the rollback failure is surfaced alongside
// it, so the cache/job-state desync is observable (parity with the
// field-override path).
func TestUpdateBatchMovie_PosterRefreshRollbackFailureSurfaced(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "RBK-002"
	filePath := "/path/to/" + movieID + ".mp4"
	result := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "RollbackFail", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	}

	const jobID = "job-any"
	tempPosterDir := filepath.Join("data", "temp", "posters", jobID)

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, filePath, true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{filePath})
	mockJob.EXPECT().UpdateMovie(mock.Anything, filePath, mock.Anything).
		Run(func(_ context.Context, _ string, _ *models.Movie) {
			// Break the cache directory between refresh and rollback so
			// RestoreAssets cannot recreate it: a file sits where MkdirAll
			// needs a directory.
			require.NoError(t, os.RemoveAll(tempPosterDir))
			require.NoError(t, os.WriteFile(tempPosterDir, []byte("blocker"), 0o644))
		}).Return(assert.AnError)
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempPosterDir, movieID+"-full.jpg"), srv.oldJPEG, 0o644))

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":"%s","title":"RollbackFail","poster_url":%q}}`, movieID, srv.URL+"/new.jpg")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Failed to update movie")
	assert.Contains(t, rec.Body.String(), "poster rollback failed")
	assert.Equal(t, 1, srv.newHits, "sanity: the refresh really downloaded the new image")
}

// TestUpdateBatchMovie_PosterRefreshSkippedWithoutPosterInfra covers the
// degradation path: when the workflow factory (and therefore the shared
// PosterGenerator) is unavailable, a source-changing whole-movie PATCH is
// still accepted without refreshing the cache — matching the field-override
// path's nil-generator skip. The bad matcher regex makes the factory
// unbuildable, which also exercises the display-title re-derive's
// factory-unavailable fallback.
func TestUpdateBatchMovie_PosterRefreshSkippedWithoutPosterInfra(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "(unclosed["
	deps := createTestDeps(t, cfg, "")

	const movieID = "NOGEN-001"
	filePath := "/path/to/" + movieID + ".mp4"
	// No workflow factory can be built from this config, so createJobWithWF
	// (which builds one directly) would panic; the handler path under test
	// does not need a job workflow.
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "NoGen", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	const sentinel = "stale-cache-sentinel"
	require.NoError(t, os.WriteFile(fullPath, []byte(sentinel), 0o644))

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":"%s","title":"NoGen","poster_url":%q}}`, movieID, srv.URL+"/new.jpg")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 0, srv.newHits, "no poster generator wired: no refresh may happen")

	content, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, sentinel, string(content), "the cache is left untouched when no generator is available")

	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, srv.URL+"/new.jpg", current.Movie.Poster.PosterURL, "the edit itself still persists")
}
