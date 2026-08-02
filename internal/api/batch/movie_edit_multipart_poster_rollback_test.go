package batch

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
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
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestUpdateBatchMovie_MultipartPartialFailure pins the multipart atomicity
// invariant for a source-changing whole-movie PATCH: whatever part fails, job
// state and the cached -full.jpg must never diverge. When a later part's
// UpdateMovie fails after an earlier part succeeded, the handler reverts the
// earlier part by re-persisting its pre-request movie BEFORE restoring the
// pre-refresh poster cache — otherwise a retry routed through the successful
// part's resultID would see the persisted (new) URL as unchanged, skip the
// asset refresh, and a manual crop would be measured against the restored old
// image while Organize downloads the new one.
//
// Each recorded UpdateMovie call is tagged "new" (the PATCHed movie carrying
// the new source URL) or "orig" (pointer-identical to the pre-request stored
// movie the handler must have fetched via GetMovieResult). The mock job keeps
// no state of its own, so the call trace is the asserted persistence trace.
func TestUpdateBatchMovie_MultipartPartialFailure(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)

	oldURL := srv.URL + "/old.jpg"
	newURL := srv.URL + "/new.jpg"

	cases := []struct {
		name              string
		failAt            int  // 0-indexed part whose UpdateMovie fails
		lookupFailPart1   bool // GetMovieResult for part 1 errors (no original snapshot)
		revertPart1Fails  bool // the revert UpdateMovie of part 1 also errors
		wantUpdateCalls   []string
		wantErrContains   []string
		wantErrNotContain []string
	}{
		{
			name:            "part 2 fails after part 1 succeeds: part 1 reverted, cache restored",
			failAt:          1,
			wantUpdateCalls: []string{"part1:new", "part2:new", "part1:orig"},
			// Persistence, revert, and poster rollback all succeeded: the
			// job is fully back at the old URLs AND the old cached image.
			wantErrContains:   []string{"Failed to update movie"},
			wantErrNotContain: []string{"revert of part", "poster rollback failed"},
		},
		{
			name:              "part 1 fails immediately: nothing updated, cache restored",
			failAt:            0,
			wantUpdateCalls:   []string{"part1:new"},
			wantErrContains:   []string{"Failed to update movie"},
			wantErrNotContain: []string{"revert of part", "poster rollback failed"},
		},
		{
			name:              "original snapshot of part 1 unavailable: revert skipped, cache still restored",
			failAt:            1,
			lookupFailPart1:   true,
			wantUpdateCalls:   []string{"part1:new", "part2:new"},
			wantErrContains:   []string{"Failed to update movie"},
			wantErrNotContain: []string{"revert of part", "poster rollback failed"},
		},
		{
			name:              "failed part revert is surfaced alongside the persist failure",
			failAt:            1,
			revertPart1Fails:  true,
			wantUpdateCalls:   []string{"part1:new", "part2:new", "part1:orig"},
			wantErrContains:   []string{"Failed to update movie", "revert of part"},
			wantErrNotContain: []string{"poster rollback failed"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			chdirWorkDir(t)
			hitsBefore := srv.newHits

			cfg := config.DefaultConfig(nil, nil)
			deps := createTestDeps(t, cfg, "")

			const movieID = "MPRT-001"
			partPaths := []string{"/path/to/" + movieID + "-cd1.mp4", "/path/to/" + movieID + "-cd2.mp4"}
			originals := []*models.Movie{
				{ID: movieID, Title: "Multipart", Poster: models.PosterState{PosterURL: oldURL}},
				{ID: movieID, Title: "Multipart", Poster: models.PosterState{PosterURL: oldURL}},
			}
			resultFor := func(i int) *resultstore.MovieResult {
				return &resultstore.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: partPaths[i], MovieID: movieID},
					Status:        models.JobStatusCompleted,
					Movie:         originals[i],
				}
			}

			var calls []string
			tag := func(i int, m *models.Movie) string {
				kind := "new"
				if m == originals[i] {
					kind = "orig"
				}
				return fmt.Sprintf("part%d:%s", i+1, kind)
			}

			mockJob := workermocks.NewMockBatchJobInterface(t)
			mockJob.EXPECT().GetFileResultByResultID(movieID).Return(resultFor(0), partPaths[0], true)
			mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return(partPaths)
			// The source-changing PATCH looks up recorded provenance for the
			// crop-intent sync; these parts contribute none.
			mockJob.EXPECT().GetProvenance(mock.Anything).Return(nil)
			if tt.lookupFailPart1 {
				mockJob.EXPECT().GetMovieResult(partPaths[0]).Return(nil, assert.AnError)
			} else {
				mockJob.EXPECT().GetMovieResult(partPaths[0]).Return(resultFor(0), nil)
			}
			if tt.failAt == 0 {
				// Part 1 fails immediately: part 2 and the revert path are untouched.
				mockJob.EXPECT().GetMovieResult(partPaths[1]).
					Return(resultFor(1), nil).Maybe()
				mockJob.EXPECT().UpdateMovie(mock.Anything, partPaths[0], mock.Anything).
					Run(func(_ context.Context, _ string, m *models.Movie) {
						calls = append(calls, tag(0, m))
					}).Return(assert.AnError)
			} else {
				// Part 1 update succeeds (and is what the revert re-persists);
				// part 2 fails.
				if tt.revertPart1Fails {
					// The revert passes the exact stored movie fetched via
					// GetMovieResult — match that pointer to fail only it.
					mockJob.EXPECT().UpdateMovie(mock.Anything, partPaths[0], originals[0]).
						Run(func(_ context.Context, _ string, m *models.Movie) {
							calls = append(calls, tag(0, m))
						}).Return(assert.AnError)
				}
				mockJob.EXPECT().UpdateMovie(mock.Anything, partPaths[0], mock.Anything).
					Run(func(_ context.Context, _ string, m *models.Movie) {
						calls = append(calls, tag(0, m))
					}).Return(nil)
				mockJob.EXPECT().GetMovieResult(partPaths[1]).Return(resultFor(1), nil)
				mockJob.EXPECT().UpdateMovie(mock.Anything, partPaths[1], mock.Anything).
					Run(func(_ context.Context, _ string, m *models.Movie) {
						calls = append(calls, tag(1, m))
					}).Return(assert.AnError)
			}
			deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

			// Seed the pre-refresh cached assets the snapshot/rollback must restore.
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
			body := fmt.Sprintf(`{"movie":{"id":"%s","title":"Multipart","poster_url":%q}}`, movieID, newURL)
			req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/"+movieID, bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
			for _, frag := range tt.wantErrContains {
				assert.Contains(t, rec.Body.String(), frag)
			}
			for _, frag := range tt.wantErrNotContain {
				assert.NotContains(t, rec.Body.String(), frag)
			}

			assert.Equal(t, hitsBefore+1, srv.newHits, "the refresh downloads the new image exactly once before persistence fails")
			assert.Equal(t, tt.wantUpdateCalls, calls,
				"UpdateMovie trace must show the successful part(s) re-persisted with their original movies before the handler returns")

			// The key invariant: no persisted part holds the new source URL
			// against the restored old image. Reverts re-persist the old URLs,
			// so the restored -full.jpg matches job state again.
			full, err := os.ReadFile(fullPath)
			require.NoError(t, err)
			assert.True(t, bytes.Equal(srv.oldJPEG, full),
				"-full.jpg must be restored when the multipart update fails, got %x want %x", full, srv.oldJPEG)
			preview, err := os.ReadFile(previewPath)
			require.NoError(t, err)
			assert.True(t, bytes.Equal(oldPreview, preview),
				"preview must be restored when the multipart update fails, got %x want %x", preview, oldPreview)
		})
	}
}

// TestUpdateBatchMovie_MultipartSuccessRefreshesCacheOnce covers the success
// path of a source-changing whole-movie PATCH on a multipart movie against a
// real job: every part is updated with the new source URL, the cached
// -full.jpg is refreshed exactly once for the movie (not once per part), and
// afterwards the persisted URLs and the cached image agree.
func TestUpdateBatchMovie_MultipartSuccessRefreshesCacheOnce(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "MPRT-200"
	partPaths := []string{"/path/to/" + movieID + "-cd1.mp4", "/path/to/" + movieID + "-cd2.mp4"}
	job := createJobWithWF(deps, cfg, partPaths)
	for _, path := range partPaths {
		setJobResult(job, path, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: movieID, Title: "Multipart OK", Poster: models.PosterState{
				PosterURL: srv.URL + "/old.jpg",
			}},
		})
	}

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	require.NoError(t, os.WriteFile(fullPath, srv.oldJPEG, 0o644))

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":"%s","title":"Multipart OK","poster_url":%q}}`, movieID, srv.URL+"/new.jpg")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, srv.newHits, "one refresh for the movie, regardless of part count")

	content, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(srv.newJPEG, content),
		"-full.jpg must hold the new image, got %x want %x", content, srv.newJPEG)

	for _, path := range partPaths {
		result := job.GetStatus().Results[path]
		require.NotNil(t, result, "result for part %s", path)
		require.NotNil(t, result.Movie)
		assert.Equal(t, srv.URL+"/new.jpg", result.Movie.Poster.PosterURL,
			"every part persists the new source URL so job state and cache stay in sync")
	}
}

// TestUpdateBatchMovie_MultipartPreservesPerPartOriginalFileName pins the
// PATCH-side sibling of the override fan-out fix: OriginalFileName is
// per-part (populated from each part's FileMatchInfo, read by template
// contexts for <FILENAME>/the NFO original path), so a whole-movie PATCH
// that merely round-trips the SELECTED part's file name must not relabel the
// sibling parts with it. A name that DIFFERS from the selection's stored
// value is a deliberate whole-movie rename and fans out to every part.
func TestUpdateBatchMovie_MultipartPreservesPerPartOriginalFileName(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	const movieID = "MPFN-001"
	cd1, cd2 := "/path/to/"+movieID+"-cd1.mp4", "/path/to/"+movieID+"-cd2.mp4"

	setup := func(t *testing.T) (*worker.BatchJob, *gin.Engine) {
		t.Helper()
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{cd1, cd2})
		for path, name := range map[string]string{cd1: movieID + "-cd1.mp4", cd2: movieID + "-cd2.mp4"} {
			resID := "res-cd1"
			if path == cd2 {
				resID = "res-cd2"
			}
			setJobResult(job, path, &resultstore.MovieResult{
				ResultID:      resID,
				FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movieID},
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: movieID, Title: "Multipart", OriginalFileName: name},
			})
		}
		router := gin.New()
		router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
		return job, router
	}
	// patch edits the movie THROUGH PART 1's result (res-cd1) — the review page
	// round-trips the displayed (selected) part's file name.
	patch := func(t *testing.T, router *gin.Engine, jobID, originalFileName string) {
		t.Helper()
		body := fmt.Sprintf(`{"movie":{"id":%q,"title":"Renamed","original_filename":%q}}`, movieID, originalFileName)
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/res-cd1", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	storedName := func(t *testing.T, job *worker.BatchJob, filePath string) string {
		t.Helper()
		result := job.GetStatus().Results[filePath]
		require.NotNil(t, result, "result for part %s", filePath)
		require.NotNil(t, result.Movie)
		return result.Movie.OriginalFileName
	}

	t.Run("round-tripped selection file name: each part keeps its own", func(t *testing.T) {
		job, router := setup(t)
		patch(t, router, job.GetID(), movieID+"-cd1.mp4") // the selected part's stored name
		assert.Equal(t, movieID+"-cd1.mp4", storedName(t, job, cd1))
		assert.Equal(t, movieID+"-cd2.mp4", storedName(t, job, cd2),
			"a wholesale fan-out would stamp CD1's file name onto CD2 and misrender its templates")
		assert.Equal(t, "Renamed", job.GetStatus().Results[cd2].Movie.Title,
			"whole-movie fields still converge on every part")
	})

	t.Run("changed file name: deliberate rename applied to every part", func(t *testing.T) {
		job, router := setup(t)
		patch(t, router, job.GetID(), "renamed.mp4") // differs from the selection's stored name
		assert.Equal(t, "renamed.mp4", storedName(t, job, cd1))
		assert.Equal(t, "renamed.mp4", storedName(t, job, cd2))
	})
}
