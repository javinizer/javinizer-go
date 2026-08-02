package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	dbmocks "github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// gatedBulkScraper answers every Search immediately EXCEPT for gateID, which
// parks until gate closes — giving the test deterministic control over which
// bulk worker reaches the commit/persist window first.
type gatedBulkScraper struct {
	gateID string
	gate   chan struct{}
	titles map[string]string
}

func (s *gatedBulkScraper) Name() string { return "gated-bulk" }

func (s *gatedBulkScraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if id == s.gateID {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	releaseDate, _ := time.Parse("2006-01-02", "2024-01-15")
	return &models.ScraperResult{
		Source:      s.Name(),
		ID:          id,
		ContentID:   id,
		Title:       s.titles[id],
		ReleaseDate: &releaseDate,
	}, nil
}

func (s *gatedBulkScraper) GetURL(_ context.Context, id string) (string, error) {
	return "https://example.invalid/" + id, nil
}

func (s *gatedBulkScraper) IsEnabled() bool { return true }

func (s *gatedBulkScraper) Close() error { return nil }

func (s *gatedBulkScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}

// TestBatchRescrapeMovies_BulkEnvelopeSerializationOnPersistFailure pins the
// Codex P2 finding "serialize whole-envelope persistence across bulk
// workers" end to end: two bulk rescrape workers run concurrently; worker
// B's whole-envelope persist fails AFTER B's commit (so B rolls itself
// back). Without job-scope serialization, worker A's persist could durably
// capture B's committed-but-later-rolled-back state — a restart would
// resurrect B's rescrape against the restored old poster cache.
//
// Determinism: movie A's scrape parks on a gate, so B is always the first
// worker to reach the commit→persist window. While B's persist blocks inside
// the repository upsert, a contended-mutex probe proves the per-job envelope
// lock (worker.AcquireJobEnvelopeLock) is held for the whole window —
// including the rollback — and only then A is released to commit and persist
// successfully. Every SUCCESSFULLY persisted envelope must therefore contain
// B's PRE-rescrape state (never B's committed-but-failed state), and the
// final in-memory state is coherent: A committed, B reverted.
func TestBatchRescrapeMovies_BulkEnvelopeSerializationOnPersistFailure(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const (
		movieA = "SRL-AAA"
		movieB = "SRL-BBB"
		fileA  = "/tmp/" + movieA + ".mp4"
		fileB  = "/tmp/" + movieB + ".mp4"
		oldA   = "SRL A OLD title"
		oldB   = "SRL B OLD title"
		newA   = "SRL A NEW title"
		newB   = "SRL B NEW title"
	)

	gateA := make(chan struct{})
	bPersistEntered := make(chan struct{})
	bPersistRelease := make(chan struct{})

	// Mocked persistence capturing every whole-envelope snapshot (the
	// results JSON the restart would reconstruct from), keyed off content:
	// the single upsert carrying B's committed state is B's own persist —
	// it parks (so the contention probe can run) and then fails.
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
				// commit→persist window: park it until the test's probe has
				// proven the envelope lock is held.
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

	deps.CoreDeps.GetRegistry().RegisterInstance(&gatedBulkScraper{
		gateID: movieA,
		gate:   gateA,
		titles: map[string]string{movieA: newA, movieB: newB},
	})

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
	router.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(testkit.GetTestRuntime(deps)))

	reqDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body, err := json.Marshal(contracts.BulkRescrapeRequest{
			MovieIDs:         []string{movieA, movieB},
			SelectedScrapers: []string{"gated-bulk"},
		})
		if err != nil {
			t.Errorf("marshal request: %v", err)
			reqDone <- nil
			return
		}
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/movies/batch-rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		reqDone <- rec
	}()

	// B committed and is parked inside its whole-envelope persist. (The
	// pre-fix code has no envelope lock, so the probe below would acquire
	// immediately and fail this test — the serialization proof.)
	select {
	case <-bPersistEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker B never reached its envelope persist — the bulk run never exercised the finding's window")
	}

	probeAcquired := make(chan struct{})
	go func() {
		release := worker.AcquireJobEnvelopeLock(job.GetID())
		release()
		close(probeAcquired)
	}()
	select {
	case <-probeAcquired:
		close(bPersistRelease)
		t.Fatal("the per-job envelope lock was free while B's persist executed — commit and persist are NOT serialized across bulk workers")
	case <-time.After(50 * time.Millisecond):
	}

	// Let B's persist fail: B's rollback (memory + provenance + cache legs)
	// runs while the envelope lock is STILL held, so it must complete before
	// the probe can acquire. Only then does A's commit window open.
	close(bPersistRelease)
	select {
	case <-probeAcquired:
	case <-time.After(10 * time.Second):
		t.Fatal("B's persist+rollback window never closed")
	}

	close(gateA) // A's scrape unblocks; A commits and persists successfully.

	var rec *httptest.ResponseRecorder
	select {
	case rec = <-reqDone:
	case <-time.After(10 * time.Second):
		t.Fatal("the bulk rescrape request never completed")
	}

	require.Equal(t, http.StatusInternalServerError, rec.Code, "body: %s", rec.Body.String())
	var resp contracts.BulkRescrapeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.PersistError, "job repository unavailable")
	require.Len(t, resp.Results, 2)
	byMovie := map[string]contracts.BulkRescrapeMovieResult{}
	for _, r := range resp.Results {
		byMovie[r.MovieID] = r
	}
	require.Contains(t, byMovie, movieA)
	require.Contains(t, byMovie, movieB)
	assert.Equal(t, models.RescrapeStatusSuccess, byMovie[movieA].Status)
	assert.Equal(t, models.RescrapeStatusFailed, byMovie[movieB].Status,
		"the rolled-back movie reports failure — its state is pre-rescrape")
	assert.Contains(t, byMovie[movieB].Error, "job repository unavailable")

	// The core invariant: every envelope snapshot captured during the run
	// that was NOT B's own failed persist must be free of B's
	// committed-but-failed state (B's own failed attempt legitimately
	// carried it — that is the write the failure rejected). A's successful
	// persist recorded A's commit while B was already rolled back, so it
	// carries B's pre-rescrape title.
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
			"snapshot %d: no durably persisted envelope may contain B's committed-but-failed state", i)
		if strings.Contains(sn.results, newA) {
			aPersistSeen++
			assert.Contains(t, sn.results, oldB,
				"snapshot %d: A's successful persist saw B rolled all the way back to its pre-rescrape state", i)
		}
	}
	assert.Equal(t, 1, failedSeen, "exactly one persist failed (B's)")
	assert.Equal(t, 1, aPersistSeen, "exactly one persist carried A's commit (A's own)")

	// Final in-memory coherence, mirroring what the last successful
	// envelope durably recorded: A's rescrape stands, B's is fully reverted.
	finalA := storedMovieResult(t, job, movieA)
	require.NotNil(t, finalA.Movie)
	assert.Equal(t, newA, finalA.Movie.Title, "A's committed rescrape stands")
	finalB := storedMovieResult(t, job, movieB)
	require.NotNil(t, finalB.Movie)
	assert.Equal(t, oldB, finalB.Movie.Title,
		"B's memory converged back to the pre-rescrape state before its window closed")
}
