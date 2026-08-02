package batch

import (
	"bytes"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patchRenameMovie issues one whole-movie PATCH that RENAMES the movie ID,
// carrying the old-key temp preview URLs the review client would round-trip,
// and returns the HTTP status code plus response body.
func patchRenameMovie(router *gin.Engine, jobID, resultID string, movie *models.Movie) (int, string) {
	body := fmt.Sprintf(
		`{"movie":{"id":%q,"title":%q,"poster_url":%q,"cover_url":"","cropped_poster_url":%q,"original_cropped_poster_url":%q}}`,
		movie.ID, movie.Title, movie.Poster.PosterURL, movie.Poster.CroppedPosterURL, movie.Poster.OriginalCroppedPosterURL)
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/"+resultID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// seedRenameCache writes the old key's cached poster assets (full-size source
// + preview) and returns the preview bytes for byte-level assertions.
func seedRenameCache(t *testing.T, posterDir, movieID string, fullBytes []byte) []byte {
	t.Helper()
	previewBytes := posterRefreshJPEG(t, 80, 120, color.RGBA{R: 0x10, G: 0x80, B: 0x10, A: 0xff})
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, movieID+"-full.jpg"), fullBytes, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, movieID+".jpg"), previewBytes, 0o644))
	return previewBytes
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "%s must not exist", path)
}

// TestUpdateBatchMovie_RenameMigratesCacheUnderLockPair pins the residual-A
// fix: a whole-movie PATCH that RENAMES the movie ID migrates the cached
// poster assets old→new (instead of leaving the old key orphaned) while
// holding BOTH keys' poster-source locks in lexical order, and re-points the
// persisted preview URLs (CroppedPosterURL AND OriginalCroppedPosterURL,
// which the poster reset flow reads). The destination key's lock is held by
// the test across the request start, proving the handler serializes on it.
func TestUpdateBatchMovie_RenameMigratesCacheUnderLockPair(t *testing.T) {
	t.Run("destination sorts after the origin (stack-on-top)", func(t *testing.T) {
		runRenamePairTest(t, "REN-ORIG", "REN-ZDEST", 1)
	})
	t.Run("destination sorts before the origin (release re-acquire), multipart", func(t *testing.T) {
		runRenamePairTest(t, "REN2-ZORIG", "REN2-ADEST", 2)
	})
}

func runRenamePairTest(t *testing.T, oldID, newID string, parts int) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	files := []string{"/path/to/" + oldID + "-cd1.mp4"}
	if parts > 1 {
		files = append(files, "/path/to/"+oldID+"-cd2.mp4")
	}
	job := createJobWithWF(deps, cfg, files)
	for _, fp := range files {
		setJobResult(job, fp, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: oldID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: oldID, Title: "Old Title", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
		})
	}
	jobID := job.GetID()
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	previewBytes := seedRenameCache(t, posterDir, oldID, srv.oldJPEG)
	oldFull := filepath.Join(posterDir, oldID+"-full.jpg")
	oldPreview := filepath.Join(posterDir, oldID+".jpg")

	tempURL := func(key, query string) string {
		return "/api/v1/temp/posters/" + jobID + "/" + key + ".jpg?" + query
	}
	renamed := &models.Movie{
		ID:    newID,
		Title: "Renamed Title",
		Poster: models.PosterState{
			PosterURL:                srv.URL + "/old.jpg", // source unchanged: no refresh, pure migration
			CroppedPosterURL:         tempURL(oldID, "v=111"),
			OriginalCroppedPosterURL: tempURL(oldID, "v=orig7"),
		},
	}

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	// Hold the DESTINATION key's lock across the request start: the handler
	// must block on it — the migration serializes with the new key's
	// crop/edit writers, whichever lexical side it sorts on.
	releaseDest := worker.AcquirePosterSourceLock(jobID, newID)
	destHeld := true
	t.Cleanup(func() {
		if destHeld {
			releaseDest()
		}
	})
	type patchResult struct {
		code int
		body string
	}
	done := make(chan patchResult, 1)
	go func() {
		code, body := patchRenameMovie(router, jobID, oldID, renamed)
		done <- patchResult{code, body}
	}()

	select {
	case out := <-done:
		t.Fatalf("PATCH completed (%d) without waiting for the destination key %s — the rename migration must hold BOTH keys' locks", out.code, newID)
	case <-time.After(150 * time.Millisecond):
	}
	releaseDest()
	destHeld = false
	select {
	case out := <-done:
		require.Equal(t, http.StatusOK, out.code, out.body)
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH did not proceed after the destination lock was released")
	}

	// Every part re-keyed, with BOTH preview URLs re-pointed at the new key.
	for _, fp := range files {
		res := storedMovieResultByPath(t, job, fp)
		require.NotNil(t, res.Movie)
		assert.Equal(t, newID, res.Movie.ID, "part %s adopts the new movie ID", fp)
		assert.Equal(t, tempURL(newID, "v=111"), res.Movie.Poster.CroppedPosterURL,
			"part %s: the preview URL follows the new key", fp)
		assert.Equal(t, tempURL(newID, "v=orig7"), res.Movie.Poster.OriginalCroppedPosterURL,
			"part %s: the reset-flow original preview URL follows the new key too", fp)
	}

	// The old key is emptied; the new key holds the migrated bytes.
	newFull, err := os.ReadFile(filepath.Join(posterDir, newID+"-full.jpg"))
	require.NoError(t, err, "the full-size asset must exist at the new key")
	assert.Equal(t, srv.oldJPEG, newFull)
	newPreview, err := os.ReadFile(filepath.Join(posterDir, newID+".jpg"))
	require.NoError(t, err)
	assert.Equal(t, previewBytes, newPreview)
	assertPathAbsent(t, oldFull)
	assertPathAbsent(t, oldPreview)
	assert.Equal(t, 0, srv.newHits, "an unchanged source must not re-download — the migration replaces regeneration")

	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenamePersistFailureMovesAssetsBack pins the
// compensation half: the envelope persist already failed after the parts
// were persisted under the new key, so the handler must revert the parts to
// their pre-request movies AND move the migrated assets back to the old key
// — a restart must never resurrect pre-rename state against a cache that
// lives at the new key.
func TestUpdateBatchMovie_RenamePersistFailureMovesAssetsBack(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const oldID, newID = "RPF-OLD1", "RPF-NEW1"
	file1 := "/path/to/" + oldID + "-cd1.mp4"
	file2 := "/path/to/" + oldID + "-cd2.mp4"
	job := createJobWithWF(deps, cfg, []string{file1, file2})
	jobID := job.GetID()
	tempURL := func(key string) string { return "/api/v1/temp/posters/" + jobID + "/" + key + ".jpg?v=111" }
	for _, fp := range []string{file1, file2} {
		setJobResult(job, fp, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: oldID},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: oldID, Title: "Old Title", Poster: models.PosterState{
				PosterURL: srv.URL + "/old.jpg", CroppedPosterURL: tempURL(oldID),
			}},
		})
	}
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	previewBytes := seedRenameCache(t, posterDir, oldID, srv.oldJPEG)
	oldFull := filepath.Join(posterDir, oldID+"-full.jpg")
	oldPreview := filepath.Join(posterDir, oldID+".jpg")

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	renamed := &models.Movie{
		ID: newID, Title: "Renamed",
		Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg", CroppedPosterURL: tempURL(oldID)},
	}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusInternalServerError, code, body)
	assert.Contains(t, body, "persist")

	// BOTH parts reverted to the pre-request movies (old key)…
	for _, fp := range []string{file1, file2} {
		res := storedMovieResultByPath(t, job, fp)
		require.NotNil(t, res.Movie)
		assert.Equal(t, oldID, res.Movie.ID, "part %s must revert to the old key", fp)
		assert.Equal(t, "Old Title", res.Movie.Title, "part %s must revert fully", fp)
		assert.Equal(t, tempURL(oldID), res.Movie.Poster.CroppedPosterURL, "part %s preview URL reverts", fp)
	}
	// …and the migrated assets moved BACK to the old key, new key emptied.
	full, err := os.ReadFile(oldFull)
	require.NoError(t, err, "the full-size asset must be back at the old key")
	assert.Equal(t, srv.oldJPEG, full)
	preview, err := os.ReadFile(oldPreview)
	require.NoError(t, err)
	assert.Equal(t, previewBytes, preview)
	assertPathAbsent(t, filepath.Join(posterDir, newID+"-full.jpg"))
	assertPathAbsent(t, filepath.Join(posterDir, newID+".jpg"))

	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenamePersistFailureMoveBackFailureSurfaced pins the
// degraded leg of the rename compensation: when the move-back ITSELF fails
// (the poster directory is destroyed between the failed persist and the
// compensation), the failure must ride along on the 500 instead of being
// swallowed.
func TestUpdateBatchMovie_RenamePersistFailureMoveBackFailureSurfaced(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	var corrupt func()
	deps.JobStore = newFailingPersistJobStoreWithHook(t, cfg, func() {
		if corrupt != nil {
			corrupt()
		}
	})

	const oldID, newID = "RMB-OLD1", "RMB-NEW1"
	filePath := "/path/to/" + oldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	jobID := job.GetID()
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Old", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	tempPosterDir := filepath.Join("data", "temp", "posters", jobID)
	seedRenameCache(t, tempPosterDir, oldID, srv.oldJPEG)
	corrupt = corruptPosterDir(tempPosterDir)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	renamed := &models.Movie{ID: newID, Title: "Renamed", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusInternalServerError, code, body)
	assert.Contains(t, body, "persist")
	assert.Contains(t, body, "poster asset move-back failed",
		"a failed move-back must surface alongside the persist error, not be swallowed")

	res := storedMovieResultByPath(t, job, filePath)
	assert.Equal(t, oldID, res.Movie.ID, "the in-memory movie stays at the old key")
	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenameRefreshFailureReversesMove pins the other
// post-migration failure: the PATCH renamed the movie AND switched the
// poster source, but the refresh download failed — the already-completed
// A→B migration is reversed before the edit is rejected with 500, so the
// old key is not left empty while the persisted state stays at A.
func TestUpdateBatchMovie_RenameRefreshFailureReversesMove(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID, newID = "RRF-OLD1", "RRF-NEW1"
	filePath := "/path/to/" + oldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Old", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	jobID := job.GetID()
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	previewBytes := seedRenameCache(t, posterDir, oldID, srv.oldJPEG)
	oldFull := filepath.Join(posterDir, oldID+"-full.jpg")
	oldPreview := filepath.Join(posterDir, oldID+".jpg")

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	renamed := &models.Movie{ID: newID, Title: "Renamed", Poster: models.PosterState{PosterURL: srv.URL + "/broken.jpg"}}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusInternalServerError, code, body)
	assert.Contains(t, body, "Failed to refresh poster source")

	// The completed migration was reversed: old key holds the bytes again,
	// the new key is empty, and the persisted state never moved.
	full, err := os.ReadFile(oldFull)
	require.NoError(t, err, "the full-size asset must be back at the old key")
	assert.Equal(t, srv.oldJPEG, full)
	preview, err := os.ReadFile(oldPreview)
	require.NoError(t, err)
	assert.Equal(t, previewBytes, preview)
	assertPathAbsent(t, filepath.Join(posterDir, newID+"-full.jpg"))
	assertPathAbsent(t, filepath.Join(posterDir, newID+".jpg"))
	res := storedMovieResultByPath(t, job, filePath)
	require.NotNil(t, res.Movie)
	assert.Equal(t, oldID, res.Movie.ID)
	assert.Equal(t, srv.URL+"/old.jpg", res.Movie.Poster.PosterURL)

	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenameMigrationFailureRejectsEdit pins the forward
// failure: when the A→B migration cannot complete (here the old key owns no
// full-size asset and the new key's stale destination is a non-empty
// directory that cannot be dropped), the edit is rejected with 500 BEFORE
// any part is persisted.
func TestUpdateBatchMovie_RenameMigrationFailureRejectsEdit(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID, newID = "RMF-OLD1", "RMF-NEW1"
	filePath := "/path/to/" + oldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Old", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	jobID := job.GetID()
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	// Stale destination that cannot be dropped: a NON-EMPTY directory.
	blockerDir := filepath.Join(posterDir, newID+"-full.jpg")
	require.NoError(t, os.MkdirAll(blockerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blockerDir, "marker"), []byte("stale"), 0o644))

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	renamed := &models.Movie{ID: newID, Title: "Renamed", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusInternalServerError, code, body)
	assert.Contains(t, body, "Failed to migrate poster assets")
	assert.NotContains(t, body, "partial move reversal failed",
		"the immediate reversal of the failed migration succeeded")

	// Best-effort reversal semantics, honestly documented: the old key owned
	// no assets, so nothing of value was lost; the offending stale directory
	// was relocated back onto the old key's name.
	assertPathAbsent(t, blockerDir)
	res := storedMovieResultByPath(t, job, filePath)
	require.NotNil(t, res.Movie)
	assert.Equal(t, oldID, res.Movie.ID, "the stored movie is untouched when the migration rejects")
	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenameRejectsInvalidNewMovieID covers the rename
// validation: a request movie ID failing safe-filename validation is
// rejected with 400 and no poster-source lock leaks.
func TestUpdateBatchMovie_RenameRejectsInvalidNewMovieID(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID = "RIV-OLD1"
	filePath := "/path/to/" + oldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Old"},
	})
	jobID := job.GetID()

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	code, body := patchRenameMovie(router, jobID, oldID, &models.Movie{ID: "../evil", Title: "x"})
	require.Equal(t, http.StatusBadRequest, code, body)
	assert.Contains(t, body, "invalid movie ID")

	res := storedMovieResultByPath(t, job, filePath)
	assert.Equal(t, oldID, res.Movie.ID, "the stored movie is untouched")
	assertPosterSourceLockFreeAPI(t, jobID, oldID)
}

// vanishAfterLookupsJob answers the first `realLookups`
// GetFileResultByResultID calls with real job data and every subsequent one
// with not-found — the rename gap path's verify read is the third lookup, so
// realLookups=2 deterministically lands the 404 in the gap (nothing else
// runs concurrently).
type vanishAfterLookupsJob struct {
	worker.BatchJobInterface
	realLookups int32
	calls       atomic.Int32
}

func (j *vanishAfterLookupsJob) GetFileResultByResultID(resultID string) (*resultstore.MovieResult, string, bool) {
	if j.calls.Add(1) > j.realLookups {
		return nil, "", false
	}
	return j.BatchJobInterface.GetFileResultByResultID(resultID)
}

// TestUpdateBatchMovie_RenameGapVanishesResult404 covers the rename gap
// path's vanished-result guard: the result is removed between the second
// and third lookup (while the handler holds the destination pair), so the
// PATCH answers 404 and releases both locks.
func TestUpdateBatchMovie_RenameGapVanishesResult404(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID, newID = "VAN-ZOLD", "VAN-ANEW" // dest < origin: the gap path
	filePath := "/path/to/" + oldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	jobID := job.GetID()
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      "res-van",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Old"},
	})
	jobIface, ok := deps.JobStore.GetBatchJob(jobID)
	require.True(t, ok)
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: &vanishAfterLookupsJob{BatchJobInterface: jobIface, realLookups: 2}}

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	code, body := patchRenameMovie(router, jobID, "res-van", &models.Movie{ID: newID, Title: "x"})
	require.Equal(t, http.StatusNotFound, code, body)
	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenameGapReconvergesAfterMidGapRekey pins the rename
// gap re-verification: the destination sorts BEFORE the origin, so the
// handler releases the origin lock to acquire the pair in lexical order —
// and a rescrape-style re-key (old → mid, state AND cache moving together)
// lands exactly in that gap. The handler must drop the stale pair,
// reconverge on the mid key, and run the rename migration from the MID key
// (not the long-gone origin).
func TestUpdateBatchMovie_RenameGapReconvergesAfterMidGapRekey(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const (
		oldID  = "ZGR-OLD"  // origin key (sorts last)
		destID = "AGR-DEST" // request rename target (sorts first → gap path)
		midID  = "MGR-MID"  // the mid-gap rekey destination (sorts between)
	)
	filePath := "/path/to/" + oldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	jobID := job.GetID()
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      "res-gap",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Gap", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	previewBytes := seedRenameCache(t, posterDir, oldID, srv.oldJPEG)

	jobIface, ok := deps.JobStore.GetBatchJob(jobID)
	require.True(t, ok)
	ready := make(chan struct{})
	wrappedJob := &signaledBatchJob{BatchJobInterface: jobIface, firstLookup: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrappedJob}

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	// Hold origin AND destination: the handler parks on the origin first,
	// then (after its release) on the destination — the mid-gap window.
	releaseA := worker.AcquirePosterSourceLock(jobID, oldID)
	releaseB := worker.AcquirePosterSourceLock(jobID, destID)
	aHeld, bHeld := true, true
	t.Cleanup(func() {
		if aHeld {
			releaseA()
		}
		if bHeld {
			releaseB()
		}
	})

	tempURL := func(key string) string { return "/api/v1/temp/posters/" + jobID + "/" + key + ".jpg?v=111" }
	type patchResult struct {
		code int
		body string
	}
	done := make(chan patchResult, 1)
	go func() {
		code, body := patchRenameMovie(router, jobID, "res-gap", &models.Movie{
			ID: destID, Title: "Renamed Gap",
			Poster: models.PosterState{
				PosterURL:        srv.URL + "/old.jpg",
				CroppedPosterURL: tempURL(midID), // the client would round-trip the CURRENT state
			},
		})
		done <- patchResult{code, body}
	}()

	<-ready // pre-lock lookup done; the PATCH now parks on the origin key.
	releaseA()
	aHeld = false
	// The handler verified the origin key, released it, and now parks on the
	// destination key — the gap is OPEN. Commit the rescrape-style re-key
	// old → mid (plain UpdateMovie takes no poster lock) and move the cached
	// assets with the state, exactly what a rescrape would do.
	require.NoError(t, jobIface.UpdateMovie(t.Context(), filePath, &models.Movie{
		ID: midID, Title: "Gap", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg", CroppedPosterURL: tempURL(midID)},
	}))
	require.NoError(t, os.Rename(filepath.Join(posterDir, oldID+"-full.jpg"), filepath.Join(posterDir, midID+"-full.jpg")))
	require.NoError(t, os.Rename(filepath.Join(posterDir, oldID+".jpg"), filepath.Join(posterDir, midID+".jpg")))
	releaseB()
	bHeld = false

	select {
	case out := <-done:
		require.Equal(t, http.StatusOK, out.code, out.body)
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH did not reconverge after the mid-gap re-key")
	}

	// The rename migrated the cache from the MID key (the converged key the
	// gap revealed), not from the long-gone origin.
	res := storedMovieResultByPath(t, job, filePath)
	require.NotNil(t, res.Movie)
	assert.Equal(t, destID, res.Movie.ID)
	assert.Equal(t, tempURL(destID), res.Movie.Poster.CroppedPosterURL)
	full, err := os.ReadFile(filepath.Join(posterDir, destID+"-full.jpg"))
	require.NoError(t, err, "the migrated full-size asset must land at the rename destination")
	assert.Equal(t, srv.oldJPEG, full)
	preview, err := os.ReadFile(filepath.Join(posterDir, destID+".jpg"))
	require.NoError(t, err)
	assert.Equal(t, previewBytes, preview)
	assertPathAbsent(t, filepath.Join(posterDir, midID+"-full.jpg"))
	assertPathAbsent(t, filepath.Join(posterDir, midID+".jpg"))
	assertPathAbsent(t, filepath.Join(posterDir, oldID+"-full.jpg"))
	assertPathAbsent(t, filepath.Join(posterDir, oldID+".jpg"))

	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, midID)
	assertPosterSourceLockFreeAPI(t, jobID, destID)
}
