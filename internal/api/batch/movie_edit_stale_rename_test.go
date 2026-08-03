package batch

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

// signalingResultLookup wraps a BatchJobInterface and closes firstResolve
// when the first GetFileResultByResultID returns — updateBatchMovie's
// INITIAL lookupResultByResultID, the last store read before the handler
// computes the (never rebased) request movie and blocks on the
// poster-source lock — so the re-key-under-wait interleave below is
// deterministic (same role signalingMovieLookup plays for the crop
// endpoint).
type signalingResultLookup struct {
	worker.BatchJobInterface
	firstResolve chan struct{}
	once         sync.Once
}

func (j *signalingResultLookup) GetFileResultByResultID(resultID string) (*resultstore.MovieResult, string, bool) {
	res, fp, found := j.BatchJobInterface.GetFileResultByResultID(resultID)
	j.once.Do(func() { close(j.firstResolve) })
	return res, fp, found
}

// TestUpdateBatchMovie_RejectsStaleRequestIDAfterMidWaitRekey pins Codex
// P1-2: the whole-movie PATCH decodes the request movie BEFORE acquiring
// the poster-source lock and never rebases it. When the target re-keys
// (A→B, a rescrape commit) while the request waits on A's lock, the
// convergence loop hands the lock off to B — but the request still
// carries movie.ID == A (the client PATCHed A's own view with NO rename
// intent). Pre-fix the pairing logic treated that stale ID as an explicit
// rename target B→A, reversing the committed re-key (or tripping a false
// rename collision). The handler must instead reject with 409
// stale-conflict (parity with the crop endpoint's post-lock source
// re-check, P1-5) and leave B's committed movie untouched.
func TestUpdateBatchMovie_RejectsStaleRequestIDAfterMidWaitRekey(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieA, movieB = "STKY-A", "STKY-B"
	filePath := "/path/to/" + movieA + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      "res-stky",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: "https://example.com/a.jpg"}},
	})

	jobID := job.GetID()
	jobIface, ok := deps.JobStore.GetBatchJob(jobID)
	require.True(t, ok)
	ready := make(chan struct{})
	wrapped := &signalingResultLookup{BatchJobInterface: jobIface, firstResolve: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrapped}

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	// The client PATCHes A's own movie view — body id == A, only a title
	// change, NO rename intent.
	patch := func() *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"movie":{"id":%q,"title":%q}}`, movieA, "Renamed Title")
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/res-stky", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	release := worker.AcquirePosterSourceLock(jobID, movieA)
	held := true
	t.Cleanup(func() {
		if held {
			release()
		}
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- patch() }()

	// The request has locked in its PRE-wait identity (A) and is parked on
	// A's lock. Commit the rescrape-corrected re-key A→B.
	<-ready
	select {
	case rec := <-done:
		release()
		held = false
		t.Fatalf("PATCH completed (%d) before the lock was released", rec.Code)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, jobIface.UpdateMovie(t.Context(), filePath, &models.Movie{
		ID: movieB, Title: "Movie B",
		Poster: models.PosterState{PosterURL: "https://example.com/a.jpg"},
	}))
	release()
	held = false

	var rec *httptest.ResponseRecorder
	select {
	case rec = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH did not complete after the lock was released")
	}
	require.Equal(t, http.StatusConflict, rec.Code,
		"a stale request ID must be rejected, not treated as a rename back to A; body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "movie identity changed")

	// B's committed movie is untouched: no rename back to A, no title edit.
	current := job.GetStatus().Results[filePath]
	require.NotNil(t, current)
	require.NotNil(t, current.Movie)
	assert.Equal(t, movieB, current.Movie.ID, "the committed re-key must not be reversed by the rejected edit")
	assert.Equal(t, "Movie B", current.Movie.Title)

	assertPosterSourceLockFreeAPI(t, jobID, movieA)
	assertPosterSourceLockFreeAPI(t, jobID, movieB)
}

// TestUpdateBatchMovie_AllowsRenameAcrossUnchangedIdentity is the gate's
// negative control: with no mid-wait re-key, an explicit rename A→C still
// proceeds (the request ID matches the converged identity, so the gate is
// inert).
func TestUpdateBatchMovie_AllowsRenameAcrossUnchangedIdentity(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieA, movieC = "RNOK-A", "RNOK-C"
	filePath := "/path/to/" + movieA + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      "res-rnok",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: "https://example.com/a.jpg"}},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	body := fmt.Sprintf(`{"movie":{"id":%q,"title":%q}}`, movieC, "Renamed")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/res-rnok", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "a genuine rename with no mid-wait re-key must proceed; body=%s", rec.Body.String())
	current := job.GetStatus().Results[filePath]
	require.NotNil(t, current)
	require.NotNil(t, current.Movie)
	assert.Equal(t, movieC, current.Movie.ID)
	assert.Equal(t, "Renamed", current.Movie.Title)
}
