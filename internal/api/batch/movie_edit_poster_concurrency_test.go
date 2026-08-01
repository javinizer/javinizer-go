package batch

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// posterConcurrencyServer serves three distinguishable posters (/old.jpg,
// /a.jpg, /b.jpg) — each with a small delay so concurrent refreshes overlap —
// plus a /broken.jpg endpoint. Hit counters are atomic; handlers run on
// separate server goroutines.
type posterConcurrencyServer struct {
	*httptest.Server
	images map[string][]byte // path ("/a.jpg") -> JPEG bytes
	hits   map[string]*atomic.Int64
	delay  time.Duration
}

func newPosterConcurrencyServer(t *testing.T) *posterConcurrencyServer {
	t.Helper()
	s := &posterConcurrencyServer{
		delay: 20 * time.Millisecond,
		hits: map[string]*atomic.Int64{
			"/old.jpg": {}, "/a.jpg": {}, "/b.jpg": {},
		},
	}
	s.images = map[string][]byte{
		"/old.jpg": posterRefreshJPEG(t, 640, 400, color.RGBA{R: 0x30, G: 0x30, B: 0x30, A: 0xff}),
		"/a.jpg":   posterRefreshJPEG(t, 640, 400, color.RGBA{R: 0xcc, G: 0x20, B: 0x20, A: 0xff}),
		"/b.jpg":   posterRefreshJPEG(t, 640, 400, color.RGBA{R: 0x20, G: 0x20, B: 0xcc, A: 0xff}),
	}
	require.NotEqual(t, s.images["/a.jpg"], s.images["/b.jpg"], "fixture images must be distinguishable")
	mux := http.NewServeMux()
	for path, img := range s.images {
		img := img
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			s.hits[path].Add(1)
			time.Sleep(s.delay) // widen the refresh window so concurrent requests overlap
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(img)
		})
	}
	mux.HandleFunc("/broken.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Server.Close)
	return s
}

// patchPosterURL issues one whole-movie PATCH with the given poster URL and
// returns the HTTP status code.
func patchPosterURL(t *testing.T, router *gin.Engine, jobID, movieID, posterURL string) int {
	t.Helper()
	body := fmt.Sprintf(`{"movie":{"id":"%s","title":"Race","poster_url":%q,"cover_url":""}}`, movieID, posterURL)
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// assertCachedPosterMatchesStoredURL is the core invariant of the fix: after
// concurrent source-changing edits, the job's persisted poster URL and the
// cached {movieID}-full.jpg must describe the same image.
func assertCachedPosterMatchesStoredURL(t *testing.T, job *worker.BatchJob, movieID, fullPath string, urlToImage map[string][]byte) {
	t.Helper()
	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	stored := current.Movie.Poster.PosterURL
	want, known := urlToImage[stored]
	require.True(t, known, "stored poster URL %q must be one of the patched sources", stored)
	content, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(want, content),
		"cached -full.jpg must match the persisted poster URL %q", stored)
}

// assertPosterSourceLockFreeAPI proves no goroutine still holds the shared
// lock for (jobID, movieID): a fresh acquire must complete immediately. A
// leak (a missed release on an error path) would deadlock future edits, so
// the acquisition runs in a goroutine with a bounded wait.
func assertPosterSourceLockFreeAPI(t *testing.T, jobID, movieID string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		release := worker.AcquirePosterSourceLock(jobID, movieID)
		release()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("poster source lock for (%s, %s) was not released", jobID, movieID)
	}
}

type signaledBatchJob struct {
	worker.BatchJobInterface
	firstLookup chan struct{}
	once        sync.Once
}

func (j *signaledBatchJob) GetFileResultByResultID(resultID string) (*resultstore.MovieResult, string, bool) {
	result, path, found := j.BatchJobInterface.GetFileResultByResultID(resultID)
	j.once.Do(func() { close(j.firstLookup) })
	return result, path, found
}

// vanishingBatchJob answers the first GetFileResultByResultID with the real
// result (signaling after it returns) and every subsequent one with
// not-found — the window in which a concurrent delete removes the result
// between the handler's pre-lock lookup and its post-lock re-read.
type vanishingBatchJob struct {
	worker.BatchJobInterface
	firstLookup chan struct{}
	once        sync.Once
	calls       atomic.Int32
}

func (j *vanishingBatchJob) GetFileResultByResultID(resultID string) (*resultstore.MovieResult, string, bool) {
	if j.calls.Add(1) > 1 {
		return nil, "", false
	}
	result, path, found := j.BatchJobInterface.GetFileResultByResultID(resultID)
	j.once.Do(func() { close(j.firstLookup) })
	return result, path, found
}

// setupPosterRaceJob builds a real batch job with a completed result whose
// poster source is /old.jpg, seeds the cached -full.jpg with the old image,
// and returns the deps (whose job store owns the job — the router must be
// built from these), the job, and the -full.jpg path.
func setupPosterRaceJob(t *testing.T, srv *posterConcurrencyServer, movieID string) (*core.APIDeps, *worker.BatchJob, string) {
	t.Helper()
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	// Loopback SSRF override scoped to this test.
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	filePath := "/path/to/" + movieID + ".mp4"
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
	require.NoError(t, os.WriteFile(fullPath, srv.images["/old.jpg"], 0o644))
	return deps, job, fullPath
}

func TestUpdateBatchMovie_ReReadsStateAfterWaitingForPosterLock(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	const movieID = "RACE-STALE"
	deps, job, fullPath := setupPosterRaceJob(t, srv, movieID)
	jobIface, ok := deps.JobStore.GetBatchJob(job.GetID())
	require.True(t, ok)

	ready := make(chan struct{})
	wrappedJob := &signaledBatchJob{BatchJobInterface: jobIface, firstLookup: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrappedJob}
	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	release := worker.AcquirePosterSourceLock(job.GetID(), movieID)
	result := make(chan int, 1)
	go func() { result <- patchPosterURL(t, router, job.GetID(), movieID, srv.URL+"/old.jpg") }()
	<-ready

	newURL := srv.URL + "/b.jpg"
	require.NoError(t, jobIface.UpdatePosterFromURL(context.Background(), movieID, newURL, newURL))
	tempPosterDir := filepath.Dir(fullPath)
	require.NoError(t, os.WriteFile(fullPath, srv.images["/b.jpg"], 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tempPosterDir, movieID+".jpg"), srv.images["/b.jpg"], 0o644))
	release()

	require.Equal(t, http.StatusOK, <-result)
	current := storedMovieResult(t, job, movieID)
	require.Equal(t, srv.URL+"/old.jpg", current.Movie.Poster.PosterURL)
	full, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	require.Equal(t, srv.images["/old.jpg"], full)
	preview, err := os.ReadFile(filepath.Join(tempPosterDir, movieID+".jpg"))
	require.NoError(t, err)
	assert.NotEqual(t, srv.images["/b.jpg"], preview)
}

// TestUpdateBatchMovie_Returns404WhenResultVanishesUnderPosterLock pins the
// post-lock re-read guard: if the result is removed between the pre-lock
// lookup and the re-read taken after AcquirePosterSourceLock unblocks, the
// handler must answer 404 rather than edit stale state — and must still
// release the lock.
func TestUpdateBatchMovie_Returns404WhenResultVanishesUnderPosterLock(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	const movieID = "RACE-VANISH"
	deps, job, _ := setupPosterRaceJob(t, srv, movieID)
	jobIface, ok := deps.JobStore.GetBatchJob(job.GetID())
	require.True(t, ok)

	ready := make(chan struct{})
	wrappedJob := &vanishingBatchJob{BatchJobInterface: jobIface, firstLookup: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrappedJob}
	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	release := worker.AcquirePosterSourceLock(job.GetID(), movieID)
	result := make(chan int, 1)
	go func() { result <- patchPosterURL(t, router, job.GetID(), movieID, srv.URL+"/a.jpg") }()
	<-ready // pre-lock lookup done; PATCH now blocks on the poster lock
	release()

	require.Equal(t, http.StatusNotFound, <-result)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMovie_ConcurrentPosterSourceChanges runs two concurrent
// whole-movie PATCHes on the same job that switch the poster source to
// different images — the exact interleave from the Codex finding. With the
// shared per-(jobID, movieID) lock, the refresh+persist sequences cannot
// interleave, so the final persisted poster URL and the cached -full.jpg must
// agree (whichever request persisted last owns both).
func TestUpdateBatchMovie_ConcurrentPosterSourceChanges(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	const movieID = "RACE-001"
	deps, job, fullPath := setupPosterRaceJob(t, srv, movieID)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	urlA := srv.URL + "/a.jpg"
	urlB := srv.URL + "/b.jpg"

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	wg.Add(2)
	go func() { defer wg.Done(); codes <- patchPosterURL(t, router, job.GetID(), movieID, urlA) }()
	go func() { defer wg.Done(); codes <- patchPosterURL(t, router, job.GetID(), movieID, urlB) }()
	wg.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, http.StatusOK, code)
	}

	assertCachedPosterMatchesStoredURL(t, job, movieID, fullPath, map[string][]byte{
		urlA: srv.images["/a.jpg"],
		urlB: srv.images["/b.jpg"],
	})
	assert.Equal(t, int64(1), srv.hits["/a.jpg"].Load(), "each new source is refreshed exactly once")
	assert.Equal(t, int64(1), srv.hits["/b.jpg"].Load(), "each new source is refreshed exactly once")
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMovie_PosterSourceChangeSerializesWithFieldOverride is the
// cross-path twin: a whole-movie PATCH and a poster_url field override run
// concurrently against the same job/movie. Both paths take the same shared
// lock instance, so their refresh+persist sequences serialize and the final
// stored URL agrees with the cached -full.jpg. The override path's generator
// is wired explicitly here (via SetReconstructionDeps, before any job adapter
// is built) because the job-edit adapter's posterGen otherwise stays nil in
// this fixture.
func TestUpdateBatchMovie_PosterSourceChangeSerializesWithFieldOverride(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	const movieID = "RACE-002"
	deps, job, fullPath := setupPosterRaceJob(t, srv, movieID)

	// Provenance for the override path: the dmm raw result contributes
	// /b.jpg as its poster URL.
	urlB := srv.URL + "/b.jpg"
	job.ResultsWriter().SetProvenance("/path/to/"+movieID+".mp4", &resultstore.ProvenanceData{
		FieldSources: map[string]string{"poster_url": "old"},
		ScraperResults: []*models.ScraperResult{
			{Source: "dmm", PosterURL: urlB, Title: "DMM Title"},
		},
	})

	// Wire the override path's poster generator (same on-disk layout as the
	// runtime's generator: {tempDir}/posters/{jobID}/{movieID}-full.jpg).
	pm := poster.NewPosterManager(afero.NewOsFs(), filepath.Join("data", "temp"), srv.Client()).
		WithSSRFCheck(func(_ string) error { return nil })
	gen := poster.NewScrapePosterGenerator(pm, "", "")
	deps.JobStore.SetReconstructionDeps(nil, gen, worker.BatchJobConfig{})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	urlA := srv.URL + "/a.jpg"
	jobIface, ok := deps.JobStore.GetBatchJob(job.GetID())
	require.True(t, ok)

	var wg sync.WaitGroup
	patchCode := make(chan int, 1)
	overrideErr := make(chan error, 1)
	wg.Add(2)
	go func() { defer wg.Done(); patchCode <- patchPosterURL(t, router, job.GetID(), movieID, urlA) }()
	go func() {
		defer wg.Done()
		_, _, err := jobIface.ApplyFieldOverride(t.Context(), movieID, "poster_url", "dmm")
		overrideErr <- err
	}()
	wg.Wait()
	require.Equal(t, http.StatusOK, <-patchCode)
	require.NoError(t, <-overrideErr)

	assertCachedPosterMatchesStoredURL(t, job, movieID, fullPath, map[string][]byte{
		urlA: srv.images["/a.jpg"],
		urlB: srv.images["/b.jpg"],
	})
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMovie_PosterSourceLockReleasedOnHandlerErrorPaths pins the
// deadlock-safety property on the handler side: every PATCH outcome —
// success, rejected refresh, missing result — leaves the shared
// per-(jobID, movieID) lock free, so a failed edit can never wedge all
// future poster edits for that movie.
func TestUpdateBatchMovie_PosterSourceLockReleasedOnHandlerErrorPaths(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	const movieID = "RACE-003"
	deps, job, _ := setupPosterRaceJob(t, srv, movieID)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	require.Equal(t, http.StatusOK,
		patchPosterURL(t, router, job.GetID(), movieID, srv.URL+"/a.jpg"))
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)

	// A refresh that fails rejects the edit with 500 — the lock must still
	// be released via the deferred unlock.
	require.Equal(t, http.StatusInternalServerError,
		patchPosterURL(t, router, job.GetID(), movieID, srv.URL+"/broken.jpg"))
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)

	// A PATCH for an unknown result 404s before taking the lock; the lock
	// for that movie must be (and remain) free.
	require.Equal(t, http.StatusNotFound,
		patchPosterURL(t, router, job.GetID(), "NO-SUCH-RESULT", srv.URL+"/a.jpg"))
	assertPosterSourceLockFreeAPI(t, job.GetID(), "NO-SUCH-RESULT")
}

// TestUpdateBatchMovie_PosterSourceLockKeyFallsBackToMatchInfo covers the
// lock-key fallback: a stored movie whose ID is empty keys the shared lock on
// FileMatchInfo.MovieID instead, and the PATCH still completes (and
// releases).
func TestUpdateBatchMovie_PosterSourceLockKeyFallsBackToMatchInfo(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	const movieID = "RACE-004"
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	// Empty stored movie ID → the lock falls back to FileMatchInfo.MovieID.
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{Title: "Race"},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	code := patchPosterURL(t, router, job.GetID(), movieID, srv.URL+"/a.jpg")
	require.Equal(t, http.StatusOK, code)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}
