package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	dbmocks "github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- shared helpers for the API envelope-serialization tests ---------------

// parkedPersist builds a real JobStore whose envelope upsert — once armed via
// arm() — closes entered, parks until releasePersist() is called, then fails.
// It hands a test deterministic control over a handler's persist window (the
// moment the whole-job snapshot is durably written), mirroring the
// batch_rescrape_envelope_lock_test.go approach.
type parkedPersist struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func newParkedPersist(t *testing.T, cfg *config.Config) (*worker.JobStore, *parkedPersist) {
	t.Helper()
	p := &parkedPersist{entered: make(chan struct{}), release: make(chan struct{})}
	repo := dbmocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().List(mock.Anything).Return([]models.Job{}, nil)
	repo.EXPECT().Upsert(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, dbJob *models.Job) error {
			if dbJob == nil || !p.armed.CompareAndSwap(true, false) {
				return nil
			}
			close(p.entered)
			<-p.release
			return errors.New("job repository unavailable")
		})
	return worker.NewJobStore(repo, nil, nil, cfg.System.TempDir, nil, nil), p
}

func (p *parkedPersist) arm() { p.armed.Store(true) }

func (p *parkedPersist) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler never reached its envelope persist — the finding's window was never exercised")
	}
}

func (p *parkedPersist) failPersist() { close(p.release) }

// probeLockAsync starts a goroutine acquiring (and immediately releasing) the
// given lock; the returned channel closes once the probe ran.
func probeLockAsync(acquire func() func()) chan struct{} {
	done := make(chan struct{})
	go func() {
		release := acquire()
		release()
		close(done)
	}()
	return done
}

// requireLockBlocked asserts the probed lock is NOT acquirable within the
// window — the handler under test must be holding it. On violation it runs
// unblock (dropping the parked persist) before failing so nothing hangs.
func requireLockBlocked(t *testing.T, probe chan struct{}, verdict string, unblock func()) {
	t.Helper()
	select {
	case <-probe:
		unblock()
		t.Fatal(verdict)
	case <-time.After(50 * time.Millisecond):
	}
}

// requireLockReleased asserts the previously blocked probe acquires once the
// lock holder's window closes — proving the lock was held across the whole
// window (persist AND rollback), since the probe was queued before the
// persist was released.
func requireLockReleased(t *testing.T, probe chan struct{}, verdict string) {
	t.Helper()
	select {
	case <-probe:
	case <-time.After(10 * time.Second):
		t.Fatal(verdict)
	}
}

// assertJobEnvelopeLockFree proves the per-job envelope lock is not held: a
// fresh acquire must complete immediately. A leak (a missed release on an
// error path) would deadlock future edits on the job.
func assertJobEnvelopeLockFree(t *testing.T, jobID string) {
	t.Helper()
	probe := probeLockAsync(func() func() { return worker.AcquireJobEnvelopeLock(jobID) })
	select {
	case <-probe:
	case <-time.After(2 * time.Second):
		t.Fatalf("job envelope lock for %s was not released", jobID)
	}
}

// patchMovieTitle issues one whole-movie PATCH carrying only the title (no
// poster-source change) and returns the recorder.
func patchMovieTitle(router *gin.Engine, jobID, movieID, title string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"movie":{"id":%q,"title":%q}}`, movieID, title)
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// --- the deterministic two-request interleave (finding B, core scenario) ---

// TestAPIEditHandlers_CrossMovieEnvelopeSerializationOnPersistFailure pins the
// Codex P2 finding "serialize API envelope commits across movie IDs"
// end to end: two concurrent whole-movie PATCHes edit DIFFERENT movies of the
// same job (so they hold DIFFERENT poster-source locks). Request B commits its
// edit and parks mid-persist; request A then runs its whole edit. Without
// per-job serialization A's whole-envelope persist durably captures B's
// committed-but-later-rejected edit — and B's subsequent persist failure +
// in-memory rollback would be resurrected by a restart from A's envelope.
//
// Determinism: B's persist parks inside the repository upsert (it already
// committed), so B's rejected state IS in memory while A runs. While B is
// parked, a contended-mutex probe proves the per-job envelope lock covers the
// whole persist+rollback window, and a second probe proves the lock ORDERING
// (B's poster-source lock is also still held — envelope nests inside poster,
// never the reverse). A is started with B's window still open: any persist A
// performs must therefore capture B ROLLED BACK. Every captured envelope
// snapshot is asserted free of B's rejected title except B's own failed
// attempt.
func TestAPIEditHandlers_CrossMovieEnvelopeSerializationOnPersistFailure(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const (
		movieA = "XSRL-AAA"
		movieB = "XSRL-BBB"
		fileA  = "/path/to/" + movieA + ".mp4"
		fileB  = "/path/to/" + movieB + ".mp4"
		oldA   = "XSRL A OLD title"
		oldB   = "XSRL B OLD title"
		newA   = "XSRL A NEW title"
		newB   = "XSRL B NEW title"
	)

	bPersistEntered := make(chan struct{})
	bPersistRelease := make(chan struct{})

	// Capture every whole-envelope snapshot (the results JSON a restart would
	// reconstruct from), keyed off content: the single upsert carrying B's
	// committed state is B's own persist — it parks (so the probes can run)
	// and then fails.
	type envelopeCapture struct {
		results string
		failed  bool
	}
	var (
		capMu      sync.Mutex
		captured   []envelopeCapture
		bFailArmed = true
	)
	repo := dbmocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().List(mock.Anything).Return([]models.Job{}, nil)
	repo.EXPECT().Upsert(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, dbJob *models.Job) error {
			if dbJob == nil {
				return nil
			}
			capMu.Lock()
			failThis := bFailArmed && strings.Contains(dbJob.Results, newB)
			if failThis {
				bFailArmed = false
			}
			capMu.Unlock()
			if failThis {
				// B's whole-envelope persist, mid-flight under B's held
				// mutation→persist→rollback window: park it until the probes
				// have proven both locks are held.
				close(bPersistEntered)
				<-bPersistRelease
			}
			capMu.Lock()
			captured = append(captured, envelopeCapture{results: dbJob.Results, failed: failThis})
			capMu.Unlock()
			if failThis {
				return errors.New("job repository unavailable")
			}
			return nil
		})
	deps.JobStore = worker.NewJobStore(repo, nil, nil, cfg.System.TempDir, nil, nil)

	job := createJobWithWF(deps, cfg, []string{fileA, fileB})
	setJobResult(job, fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: oldA},
	})
	setJobResult(job, fileB, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileB, MovieID: movieB},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieB, Title: oldB},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	bDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { bDone <- patchMovieTitle(router, job.GetID(), movieB, newB) }()

	// B committed and is parked inside its whole-envelope persist.
	select {
	case <-bPersistEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("request B never reached its envelope persist — the finding's window was never exercised")
	}

	// Probe 1: the per-job envelope lock must be held while B's persist
	// executes. Pre-fix (no envelope lock in the handler) this acquires at
	// once and fails the test.
	envelopeProbe := probeLockAsync(func() func() { return worker.AcquireJobEnvelopeLock(job.GetID()) })
	requireLockBlocked(t, envelopeProbe,
		"the job envelope lock was free while B's persist executed — mutation→persist→rollback is not serialized across API edits",
		func() { close(bPersistRelease) })

	// Probe 2 (lock ordering): B's poster-source lock is STILL held while the
	// envelope lock is held — the envelope window nests INSIDE the poster
	// window (poster → envelope), never the reverse (no publisher may
	// acquire a poster lock while holding the envelope lock).
	posterProbe := probeLockAsync(func() func() { return worker.AcquirePosterSourceLock(job.GetID(), movieB) })
	requireLockBlocked(t, posterProbe,
		"B's poster-source lock was free while its envelope persist executed — the envelope window must nest inside the poster lock",
		func() { close(bPersistRelease) })

	// A starts with B's window still open: with the lock in place A cannot
	// persist until B has rolled back and released; without it A would
	// persist B's committed-but-later-rejected title right now.
	aDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { aDone <- patchMovieTitle(router, job.GetID(), movieA, newA) }()

	// Let B's persist fail: B's compensation (revert its part in memory)
	// runs while the envelope lock is STILL held, so it must complete before
	// the queued probe can acquire.
	close(bPersistRelease)
	requireLockReleased(t, envelopeProbe,
		"B's persist+rollback window never closed after the failed persist")
	requireLockReleased(t, posterProbe,
		"B's poster-source lock was never released after its handler returned")

	// Ordering proof, tail: the probe had been queued since BEFORE the
	// persist was released, so by the time it acquired, B's rollback had
	// already run inside the window — B's in-memory state is back at the
	// pre-edit title before ANY other envelope writer could persist.
	midB := storedMovieResult(t, job, movieB)
	require.NotNil(t, midB.Movie)
	assert.Equal(t, oldB, midB.Movie.Title,
		"B's in-memory rollback must complete inside the envelope-lock window, before the lock was releasable")

	bRec := <-bDone
	require.Equal(t, http.StatusInternalServerError, bRec.Code, "body: %s", bRec.Body.String())
	assert.Contains(t, bRec.Body.String(), "persist")

	aRec := <-aDone
	require.Equal(t, http.StatusOK, aRec.Code, "body: %s", aRec.Body.String())

	// The core invariant: no durably persisted envelope may contain B's
	// committed-but-rejected edit. B's own failed attempt legitimately
	// carried it (that is the write the failure rejected); A's successful
	// persist ran strictly after B's rollback, so it carries B's pre-edit
	// title.
	capMu.Lock()
	snaps := append([]envelopeCapture(nil), captured...)
	capMu.Unlock()
	var failedSeen, aPersistSeen int
	for i, sn := range snaps {
		if sn.failed {
			failedSeen++
			assert.Contains(t, sn.results, newB,
				"snapshot %d: B's own rejected persist is the one carry of B's committed state", i)
			continue
		}
		assert.NotContains(t, sn.results, newB,
			"snapshot %d: no durably persisted envelope may contain B's committed-but-failed edit", i)
		if strings.Contains(sn.results, newA) {
			aPersistSeen++
			assert.Contains(t, sn.results, oldB,
				"snapshot %d: A's successful persist saw B rolled all the way back to its pre-edit state", i)
		}
	}
	assert.Equal(t, 1, failedSeen, "exactly one persist failed (B's)")
	assert.Equal(t, 1, aPersistSeen, "exactly one persist carried A's commit (A's own)")

	// Final in-memory coherence, mirroring the last durable envelope.
	finalA := storedMovieResult(t, job, movieA)
	require.NotNil(t, finalA.Movie)
	assert.Equal(t, newA, finalA.Movie.Title, "A's committed edit stands")
	finalB := storedMovieResult(t, job, movieB)
	require.NotNil(t, finalB.Movie)
	assert.Equal(t, oldB, finalB.Movie.Title, "B's memory converged back to the pre-edit state")

	assertPosterSourceLockFreeAPI(t, job.GetID(), movieA)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieB)
	assertJobEnvelopeLockFree(t, job.GetID())
}

// --- per-endpoint envelope-lock probes --------------------------------------

// TestUpdateBatchMoviePosterCrop_HoldsJobEnvelopeLockAcrossPersistWindow pins
// finding B for the crop handler: while the request is parked mid-persist,
// the job envelope lock AND the movie's poster-source lock are both held
// (poster → envelope ordering), and the rollback completes inside the window
// (visible reverted before the queued probe acquires).
func TestUpdateBatchMoviePosterCrop_HoldsJobEnvelopeLockAcrossPersistWindow(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	store, parked := newParkedPersist(t, cfg)
	deps.JobStore = store

	const movieID = "ESRL-CROP"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Envelope Crop", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/pre-crop.jpg",
			ShouldCropPoster: true,
		}},
	})
	seedCropFullPoster(t, job.GetID(), movieID)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	parked.arm()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { _, rec := postPosterCrop(router, job.GetID(), movieID); done <- rec }()
	parked.waitEntered(t)

	envelopeProbe := probeLockAsync(func() func() { return worker.AcquireJobEnvelopeLock(job.GetID()) })
	requireLockBlocked(t, envelopeProbe,
		"the crop handler persisted the envelope without holding the job envelope lock", parked.failPersist)
	posterProbe := probeLockAsync(func() func() { return worker.AcquirePosterSourceLock(job.GetID(), movieID) })
	requireLockBlocked(t, posterProbe,
		"the crop handler's poster lock was free mid-persist — envelope must nest inside poster", parked.failPersist)

	parked.failPersist()
	requireLockReleased(t, envelopeProbe, "the crop handler's envelope window never closed")
	requireLockReleased(t, posterProbe, "the crop handler's poster lock was never released")

	// The rollback ran under the still-held envelope lock: by the time the
	// queued probe acquired, the crop was already reverted in memory.
	stored := storedMovieResult(t, job, movieID)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "/api/v1/temp/posters/pre-crop.jpg", stored.Movie.Poster.CroppedPosterURL)
	assert.True(t, stored.Movie.Poster.ShouldCropPoster)
	assert.Nil(t, stored.Movie.Poster.CropBounds)

	rec := <-done
	assertPersistFailed500(t, rec, job)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	assertJobEnvelopeLockFree(t, job.GetID())
}

// TestUpdateBatchMoviePosterFromURL_HoldsJobEnvelopeLockAcrossPersistWindow is
// the same probe for the poster-from-URL handler.
func TestUpdateBatchMoviePosterFromURL_HoldsJobEnvelopeLockAcrossPersistWindow(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)
	chdirWorkDir(t)

	img := image.NewRGBA(image.Rect(0, 0, 800, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	store, parked := newParkedPersist(t, cfg)
	deps.JobStore = store

	const movieID = "ESRL-PURL"
	filePath := "/path/to/" + movieID + ".mp4"
	oldURL := "https://example.com/old-poster.jpg"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "From URL", Poster: models.PosterState{
			PosterURL: oldURL,
		}},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	parked.arm()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/poster.jpg"})
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		done <- rec
	}()
	parked.waitEntered(t)

	envelopeProbe := probeLockAsync(func() func() { return worker.AcquireJobEnvelopeLock(job.GetID()) })
	requireLockBlocked(t, envelopeProbe,
		"the poster-from-URL handler persisted the envelope without holding the job envelope lock", parked.failPersist)
	posterProbe := probeLockAsync(func() func() { return worker.AcquirePosterSourceLock(job.GetID(), movieID) })
	requireLockBlocked(t, posterProbe,
		"the poster-from-URL handler's poster lock was free mid-persist — envelope must nest inside poster", parked.failPersist)

	parked.failPersist()
	requireLockReleased(t, envelopeProbe, "the poster-from-URL handler's envelope window never closed")
	requireLockReleased(t, posterProbe, "the poster-from-URL handler's poster lock was never released")

	// Rollback completed inside the window before the probe could acquire.
	stored := storedMovieResult(t, job, movieID)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, oldURL, stored.Movie.Poster.PosterURL)

	rec := <-done
	assertPersistFailed500(t, rec, job)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	assertJobEnvelopeLockFree(t, job.GetID())
}

// TestOverrideBatchMovieField_HoldsJobEnvelopeLockAcrossPersistWindow is the
// same probe for the field-override endpoint, whose persist lives inside
// jobEditorImpl.ApplyFieldOverride — proving the envelope lock was layered at
// the persist-owning layer, serialized with the poster-source lock it already
// held.
func TestOverrideBatchMovieField_HoldsJobEnvelopeLockAcrossPersistWindow(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	store, parked := newParkedPersist(t, cfg)
	deps.JobStore = store

	filePath := "/path/to/ESRL-FOVR.mp4"
	const resultID = "ESRL-FOVR"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: resultID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: resultID, ContentID: resultID, Title: "Aggregated", Maker: "AggregatedMaker"},
		StartedAt:     time.Now(),
	})
	job.ResultsWriter().SetProvenance(filePath, &resultstore.ProvenanceData{
		FieldSources: map[string]string{"maker": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Maker: "R18Maker", Title: "R18Title"},
			{Source: "dmm", Maker: "DMMMaker", Title: "DMMTitle"},
		},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	parked.arm()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		done <- rec
	}()
	parked.waitEntered(t)

	envelopeProbe := probeLockAsync(func() func() { return worker.AcquireJobEnvelopeLock(job.GetID()) })
	requireLockBlocked(t, envelopeProbe,
		"the field override persisted the envelope without holding the job envelope lock", parked.failPersist)
	posterProbe := probeLockAsync(func() func() { return worker.AcquirePosterSourceLock(job.GetID(), resultID) })
	requireLockBlocked(t, posterProbe,
		"the field override's poster lock was free mid-persist — envelope must nest inside poster", parked.failPersist)

	parked.failPersist()
	requireLockReleased(t, envelopeProbe, "the field override's envelope window never closed")
	requireLockReleased(t, posterProbe, "the field override's poster lock was never released")

	// The in-section compensation completed before the probe could acquire:
	// the overridden maker already reverted.
	stored := storedMovieResult(t, job, resultID)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "AggregatedMaker", stored.Movie.Maker)

	rec := <-done
	assertPersistFailed500(t, rec, job)
	assertPosterSourceLockFreeAPI(t, job.GetID(), resultID)
	assertJobEnvelopeLockFree(t, job.GetID())
}
