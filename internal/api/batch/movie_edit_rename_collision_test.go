package batch

import (
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readCacheFile reads a poster-cache file so its bytes survive a rejection
// assertion unchanged.
func readCacheFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "%s must still exist", path)
	return b
}

// TestUpdateBatchMovie_RenameCollisionRejectsBeforeMigration pins the
// handler-layer fix for Codex P2 (updateBatchMovie's collision check before
// worker.MigratePosterCacheAssets, mirroring the id-override path's
// worker.CheckRenameDestinationCollision): a whole-movie PATCH that RENAMES
// movie A's ID to an ID another result family already uses must surface
// 400-class — rejected UNDER the held lexical lock pair with NO asset move
// and NO state mutation. Otherwise the move would replace B's cache with
// A's assets while B's result keeps its own poster source/crop state, and a
// later crop of either would fan bounds across both families (Organize then
// applies them against different sources).
func TestUpdateBatchMovie_RenameCollisionRejectsBeforeMigration(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID, heldID = "RNC-ORIG", "RNC-HELD"
	fileA := "/path/to/" + oldID + ".mp4"
	fileB := "/path/to/" + heldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{fileA, fileB})
	jobID := job.GetID()
	previewB := "/api/v1/temp/posters/" + jobID + "/" + heldID + ".jpg?v=other"
	setJobResult(job, fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: oldID, Title: "Movie A", Poster: models.PosterState{
			PosterURL: srv.URL + "/old.jpg",
		}},
	})
	setJobResult(job, fileB, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileB, MovieID: heldID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: heldID, Title: "Movie B", Poster: models.PosterState{
			PosterURL:        "https://other.invalid/poster.jpg",
			CroppedPosterURL: previewB,
		}},
	})
	// BOTH keys hold their own cached assets.
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	aPreview := seedRenameCache(t, posterDir, oldID, srv.oldJPEG)
	bFull := posterRefreshJPEG(t, 400, 300, color.RGBA{G: 0x77, A: 0xff})
	bPreview := seedRenameCache(t, posterDir, heldID, bFull)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	renamed := &models.Movie{
		ID:    heldID,
		Title: "Renamed Title",
		Poster: models.PosterState{
			PosterURL: srv.URL + "/old.jpg", // source unchanged: a pure rename
		},
	}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusBadRequest, code, "body: %s", body)
	assert.Contains(t, body, "already uses that movie ID")
	assert.NotContains(t, body, "not found",
		"a validation rejection must never read like a missing-result 404")

	// REJECTED BEFORE ANY ASSET MOVE: A's cache is still at the old key,
	// B's cache is byte-identical at the held key (never displaced).
	assert.Equal(t, srv.oldJPEG, readCacheFile(t, filepath.Join(posterDir, oldID+"-full.jpg")))
	assert.Equal(t, aPreview, readCacheFile(t, filepath.Join(posterDir, oldID+".jpg")))
	assert.Equal(t, bFull, readCacheFile(t, filepath.Join(posterDir, heldID+"-full.jpg")))
	assert.Equal(t, bPreview, readCacheFile(t, filepath.Join(posterDir, heldID+".jpg")))

	// NO state mutation: BOTH results keep their exact pre-patch movies.
	resA := storedMovieResultByPath(t, job, fileA)
	require.NotNil(t, resA.Movie)
	assert.Equal(t, oldID, resA.Movie.ID)
	assert.Equal(t, "Movie A", resA.Movie.Title)
	resB := storedMovieResultByPath(t, job, fileB)
	require.NotNil(t, resB.Movie)
	assert.Equal(t, heldID, resB.Movie.ID)
	assert.Equal(t, "Movie B", resB.Movie.Title)
	assert.Equal(t, previewB, resB.Movie.Poster.CroppedPosterURL,
		"B keeps its own preview state — migrated cache bytes must never displace it")

	assert.Equal(t, 0, srv.newHits, "a rejected rename must not refresh or re-download anything")
	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, heldID)
}

// TestUpdateBatchMovie_RenameFreeDestinationMigrates pins the non-colliding
// half: when the destination ID is unused by any other result, the PATCH
// rename proceeds — the cached assets migrate old→new and every persisted
// preview URL is re-pointed (mirrors the destination-ununsed worker case in
// field_override_id_collision_test.go's free-rename coverage).
func TestUpdateBatchMovie_RenameFreeDestinationMigrates(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID, newID = "RNF-ORIG", "RNF-NEW"
	fileA := "/path/to/" + oldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{fileA})
	jobID := job.GetID()
	tempURL := func(key string) string {
		return "/api/v1/temp/posters/" + jobID + "/" + key + ".jpg?v=111"
	}
	setJobResult(job, fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: oldID, Title: "Old Title", Poster: models.PosterState{
			PosterURL:        srv.URL + "/old.jpg",
			CroppedPosterURL: tempURL(oldID),
		}},
	})
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	previewBytes := seedRenameCache(t, posterDir, oldID, srv.oldJPEG)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	renamed := &models.Movie{
		ID:    newID,
		Title: "Renamed Title",
		Poster: models.PosterState{
			PosterURL:        srv.URL + "/old.jpg",
			CroppedPosterURL: tempURL(oldID),
		},
	}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusOK, code, body)

	res := storedMovieResultByPath(t, job, fileA)
	require.NotNil(t, res.Movie)
	assert.Equal(t, newID, res.Movie.ID)
	assert.Equal(t, tempURL(newID), res.Movie.Poster.CroppedPosterURL,
		"the preview URL followed the new key")
	assert.Equal(t, srv.oldJPEG, readCacheFile(t, filepath.Join(posterDir, newID+"-full.jpg")))
	assert.Equal(t, previewBytes, readCacheFile(t, filepath.Join(posterDir, newID+".jpg")))
	assertPathAbsent(t, filepath.Join(posterDir, oldID+"-full.jpg"))
	assertPathAbsent(t, filepath.Join(posterDir, oldID+".jpg"))
	assert.Equal(t, 0, srv.newHits, "an unchanged source must not re-download")

	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenameSameFamilyDestinationIsNotCollision pins the
// exclusion half of the collision check at the PATCH layer: a path indexed
// at the destination key that belongs to the SAME movie family (a multipart
// sibling whose FileMatchInfo.MovieID already equals the destination) is the
// normal fan-out case — the rename must proceed and re-key the whole family.
func TestUpdateBatchMovie_RenameSameFamilyDestinationIsNotCollision(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID, newID = "RNS-ORIG", "RNS-NEW"
	part1 := "/path/to/" + oldID + "-cd1.mp4"
	part2 := "/path/to/" + oldID + "-cd2.mp4"
	job := createJobWithWF(deps, cfg, []string{part1, part2})
	jobID := job.GetID()
	setJobResult(job, part1, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: part1, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Family", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	setJobResult(job, part2, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: part2, MovieID: newID}, // same family, already indexed at the destination key
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Family", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	previewBytes := seedRenameCache(t, posterDir, oldID, srv.oldJPEG)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	renamed := &models.Movie{
		ID:    newID,
		Title: "Renamed Title",
		Poster: models.PosterState{
			PosterURL: srv.URL + "/old.jpg",
		},
	}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusOK, code, body,
		"a same-family path at the destination key is the fan-out case, not a collision")

	for _, fp := range []string{part1, part2} {
		res := storedMovieResultByPath(t, job, fp)
		require.NotNil(t, res.Movie)
		assert.Equal(t, newID, res.Movie.ID, "part %s adopts the new movie ID", fp)
	}
	assert.Equal(t, srv.oldJPEG, readCacheFile(t, filepath.Join(posterDir, newID+"-full.jpg")))
	assert.Equal(t, previewBytes, readCacheFile(t, filepath.Join(posterDir, newID+".jpg")))
	assertPathAbsent(t, filepath.Join(posterDir, oldID+"-full.jpg"))
	assertPathAbsent(t, filepath.Join(posterDir, oldID+".jpg"))

	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, newID)
}

// TestUpdateBatchMovie_RenameCollisionWithAssetlessOriginKeepsDestCache pins
// the deletion leg of the Codex P2 finding: when the renaming movie has NO
// cached assets, MigratePosterCacheAssets' normalizing semantics would
// DELETE the destination family's cache outright (absent-source leg). The
// collision rejection must run first — B's cache survives byte-identical.
func TestUpdateBatchMovie_RenameCollisionWithAssetlessOriginKeepsDestCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const oldID, heldID = "RND-ORIG", "RND-HELD"
	fileA := "/path/to/" + oldID + ".mp4"
	fileB := "/path/to/" + heldID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{fileA, fileB})
	jobID := job.GetID()
	previewB := "/api/v1/temp/posters/" + jobID + "/" + heldID + ".jpg?v=other"
	setJobResult(job, fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: oldID, Title: "Movie A", Poster: models.PosterState{
			// No poster source: deleting the preview cleanup path is not the
			// subject here — the ORIGIN simply holds no cached assets.
			PosterURL: "",
		}},
	})
	setJobResult(job, fileB, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileB, MovieID: heldID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: heldID, Title: "Movie B", Poster: models.PosterState{
			PosterURL:        srv.URL + "/old.jpg",
			CroppedPosterURL: previewB,
		}},
	})
	// Only B's key holds cached assets — the deletion leg would consume them.
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	bPreview := seedRenameCache(t, posterDir, heldID, srv.oldJPEG)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	renamed := &models.Movie{
		ID:    heldID,
		Title: "Renamed Title",
		Poster: models.PosterState{
			PosterURL: "", // unchanged: still no source on A
		},
	}
	code, body := patchRenameMovie(router, jobID, oldID, renamed)
	require.Equal(t, http.StatusBadRequest, code, "body: %s", body)
	assert.Contains(t, body, "already uses that movie ID")

	// The deletion never ran: B's cache is intact, B's state untouched.
	assert.Equal(t, srv.oldJPEG, readCacheFile(t, filepath.Join(posterDir, heldID+"-full.jpg")))
	assert.Equal(t, bPreview, readCacheFile(t, filepath.Join(posterDir, heldID+".jpg")),
		"the colliding rename must not delete the destination family's cache")
	resB := storedMovieResultByPath(t, job, fileB)
	require.NotNil(t, resB.Movie)
	assert.Equal(t, heldID, resB.Movie.ID)
	assert.Equal(t, "Movie B", resB.Movie.Title)
	assert.Equal(t, previewB, resB.Movie.Poster.CroppedPosterURL)
	resA := storedMovieResultByPath(t, job, fileA)
	require.NotNil(t, resA.Movie)
	assert.Equal(t, oldID, resA.Movie.ID, "A must not be re-keyed by the rejected rename")

	assertPosterSourceLockFreeAPI(t, jobID, oldID)
	assertPosterSourceLockFreeAPI(t, jobID, heldID)
}
