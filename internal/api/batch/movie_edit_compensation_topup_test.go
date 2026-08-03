package batch

// Patch-coverage top-up for updateBatchMovie's compensation annotations and
// A13's 410 leg on the whole-movie PATCH:
//   - the FAILING part had no readable pre-update snapshot (nil prior) — the
//     annotation must surface instead of a silent skip
//   - reverting the FAILING part itself failed — the failure must ride the
//     500 message
//   - the envelope persist answered ErrJobGone — 410, with the edit
//     compensated

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestUpdateBatchMovie_UpdateFailureFailingPartCompensationSurfaces drives the
// multipart whole-movie PATCH through a mid-loop UpdateMovie failure with
// (a) the failing part's pre-update snapshot UNREADABLE (nil prior) and
// (b) the failing part's own revert erroring — both compensation corner
// legs must annotate the 500 instead of being swallowed.
func TestUpdateBatchMovie_UpdateFailureFailingPartCompensationSurfaces(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)

	oldURL := srv.URL + "/old.jpg"
	newURL := srv.URL + "/new.jpg"

	cases := []struct {
		name                 string
		lookupFailFailing    bool // GetMovieResult for the FAILING part errors
		revertFailingFails   bool // RestoreMovieResult for the FAILING part errors
		wantUpdateCalls      []string
		wantErrContains      []string
		wantRestoreCallCount int
	}{
		{
			name:              "failing part snapshot unreadable: annotation surfaces, sibling restored",
			lookupFailFailing: true,
			// Part 2 failed with no prior to restore to; only part 1 reverts.
			wantUpdateCalls: []string{"part1:new", "part2:new", "part1:orig"},
			wantErrContains: []string{
				"Failed to update movie",
				"no pre-update snapshot for failing part /path/to/FPT-001-cd2.mp4",
			},
			wantRestoreCallCount: 1,
		},
		{
			name:               "failing part revert fails: failure surfaces",
			revertFailingFails: true,
			wantUpdateCalls:    []string{"part1:new", "part2:new", "part2:orig-attempted", "part1:orig"},
			wantErrContains: []string{
				"Failed to update movie",
				"revert of failing part /path/to/FPT-001-cd2.mp4 failed",
			},
			wantRestoreCallCount: 2,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			chdirWorkDir(t)
			hitsBefore := srv.newHits

			cfg := config.DefaultConfig(nil, nil)
			deps := createTestDeps(t, cfg, "")

			const movieID = "FPT-001"
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
			restoreTag := func(prior *resultstore.MovieResult, failed bool) string {
				for i, orig := range originals {
					if prior != nil && prior.Movie == orig {
						if failed {
							return fmt.Sprintf("part%d:orig-attempted", i+1)
						}
						return fmt.Sprintf("part%d:orig", i+1)
					}
				}
				return "unknown"
			}

			mockJob := workermocks.NewMockBatchJobInterface(t)
			mockJob.EXPECT().GetFileResultByResultID(movieID).Return(resultFor(0), partPaths[0], true)
			mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return(partPaths)
			mockJob.EXPECT().GetProvenance(mock.Anything).Return(nil)
			mockJob.EXPECT().GetMovieResult(partPaths[0]).Return(resultFor(0), nil)
			if tt.lookupFailFailing {
				// The FAILING part's snapshot lookup fails at capture time —
				// updateFailPrior is nil when its UpdateMovie errors.
				mockJob.EXPECT().GetMovieResult(partPaths[1]).Return(nil, assert.AnError)
			} else {
				mockJob.EXPECT().GetMovieResult(partPaths[1]).Return(resultFor(1), nil)
			}
			mockJob.EXPECT().UpdateMovie(mock.Anything, partPaths[0], mock.Anything).
				Run(func(_ context.Context, _ string, m *models.Movie) {
					calls = append(calls, tag(0, m))
				}).Return(nil)
			mockJob.EXPECT().UpdateMovie(mock.Anything, partPaths[1], mock.Anything).
				Run(func(_ context.Context, _ string, m *models.Movie) {
					calls = append(calls, tag(1, m))
				}).Return(assert.AnError)
			restoreCalls := 0
			if !tt.lookupFailFailing {
				// The failing part is restored FIRST (its forward UpdateMovie
				// may have committed DB side effects).
				revertErr := error(nil)
				if tt.revertFailingFails {
					revertErr = assert.AnError
				}
				mockJob.EXPECT().RestoreMovieResult(mock.Anything, partPaths[1], mock.Anything).
					Run(func(_ context.Context, _ string, prior *resultstore.MovieResult) {
						restoreCalls++
						calls = append(calls, restoreTag(prior, revertErr != nil))
					}).Return(revertErr)
			}
			mockJob.EXPECT().RestoreMovieResult(mock.Anything, partPaths[0], mock.Anything).
				Run(func(_ context.Context, _ string, prior *resultstore.MovieResult) {
					restoreCalls++
					calls = append(calls, restoreTag(prior, false))
				}).Return(nil)

			deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

			// Seed the pre-refresh cache the rollback restores.
			tempPosterDir := filepath.Join("data", "temp", "posters", "job-any")
			require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
			fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
			oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
			require.NoError(t, os.WriteFile(fullPath, srv.oldJPEG, 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(tempPosterDir, movieID+".jpg"), oldPreview, 0o644))

			router := gin.New()
			router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
			body := fmt.Sprintf(`{"movie":{"id":"%s","title":"Multipart","poster_url":%q}}`, movieID, newURL)
			req := httptest.NewRequest(http.MethodPatch, "/batch/job-any/results/"+movieID, bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
			for _, frag := range tt.wantErrContains {
				assert.Contains(t, rec.Body.String(), frag)
			}
			assert.NotContains(t, rec.Body.String(), "poster rollback failed",
				"the cache restore succeeds in both legs")
			assert.Equal(t, tt.wantUpdateCalls, calls)
			assert.Equal(t, tt.wantRestoreCallCount, restoreCalls)
			assert.Equal(t, hitsBefore+1, srv.newHits,
				"the source refresh ran before the update failed; its rollback restored the cache")

			full, err := os.ReadFile(fullPath)
			require.NoError(t, err)
			assert.Equal(t, srv.oldJPEG, full)
		})
	}
}

// TestUpdateBatchMovie_JobGonePersistReturns410 pins A13 for the whole-movie
// PATCH: the job vanishes between the handler's lookup and its envelope
// persist. The in-memory edit is compensated (the pre-request movie is
// re-seated) and the client gets 410 instead of a false 200 ack of state a
// restart could never reconstruct.
func TestUpdateBatchMovie_JobGonePersistReturns410(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "GPCH-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Old Title"},
	})

	deps.JobStore = &jobGoneStore{JobStoreInterface: deps.JobStore}

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":%q,"title":"New Title"}}`, movieID)
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusGone, rec.Code, rec.Body.String())

	// compensateEdit re-seated the pre-request movie — the rejected edit must
	// not linger in memory against a vanished envelope.
	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, "Old Title", current.Movie.Title)
}
