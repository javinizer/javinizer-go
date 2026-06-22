package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// concurrencyTrackingWorkflow tracks the peak number of concurrently in-flight
// Scrape calls. Used to detect worker-pool degradation from N -> 1.
type concurrencyTrackingWorkflow struct {
	inFlight  int32
	peak      int32
	completed int32
	release   chan struct{}
}

func newConcurrencyTrackingWorkflow(release chan struct{}) *concurrencyTrackingWorkflow {
	return &concurrencyTrackingWorkflow{release: release}
}

func (w *concurrencyTrackingWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	cur := atomic.AddInt32(&w.inFlight, 1)
	for {
		p := atomic.LoadInt32(&w.peak)
		if cur <= p || atomic.CompareAndSwapInt32(&w.peak, p, cur) {
			break
		}
	}
	defer atomic.AddInt32(&w.inFlight, -1)

	// Wait until released by the test harness. This holds the worker slot
	// open so we can observe true concurrency.
	<-w.release
	atomic.AddInt32(&w.completed, 1)
	return makeScrapeResult("CONC-001"), nil, nil
}

func (w *concurrencyTrackingWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (w *concurrencyTrackingWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (w *concurrencyTrackingWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (w *concurrencyTrackingWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

// TestScrapePhase_PeakConcurrencyDoesNotDegrade verifies that the scrape phase
// actually runs MaxWorkers goroutines in parallel throughout the batch — it
// does not degrade to processing one file at a time (the reported regression
// after the architecture-refactor rebase).
//
// With MaxWorkers=5 and 15 files where each Scrape blocks until released,
// the peak in-flight count must reach 5. If the pool leaks semaphore slots
// (e.g. acquire-without-release on an error/panic path) the peak will be < 5,
// eventually degrading to 1.
func TestScrapePhase_PeakConcurrencyDoesNotDegrade(t *testing.T) {
	const maxWorkers = 5
	const numFiles = 15

	release := make(chan struct{}, numFiles)
	wf := newConcurrencyTrackingWorkflow(release)

	files := make([]string, numFiles)
	for i := range files {
		files[i] = "file-" + string(rune('a'+i)) + ".mp4"
	}

	inputs := makeInputs(nil)
	inputs.WF = wf
	inputs.Concurrency = concurrencyConfig{MaxWorkers: maxWorkers, WorkerTimeout: 0}
	fmi := make(map[string]models.FileMatchInfo, numFiles)
	for _, f := range files {
		fmi[f] = models.FileMatchInfo{Path: f}
	}
	inputs.FileMatchInfo = fmi

	done := make(chan struct{})
	go func() {
		NewScrapePhase().Run(context.Background(), inputs, files, ScrapePhaseConfig{})
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&wf.peak) >= int32(maxWorkers) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < numFiles; i++ {
		release <- struct{}{}
	}
	<-done

	peak := int(atomic.LoadInt32(&wf.peak))
	t.Logf("peak in-flight workers = %d (expected >= %d)", peak, maxWorkers)
	assert.GreaterOrEqual(t, peak, maxWorkers,
		"scrape phase must sustain MaxWorkers concurrent workers; degradation to fewer indicates a semaphore/goroutine leak")
	assert.Equal(t, int32(numFiles), atomic.LoadInt32(&wf.completed), "all files must complete")
}

// errorTrackingWorkflow returns an error on every Scrape call while tracking
// peak in-flight concurrency.
type errorTrackingWorkflow struct {
	inFlight  int32
	peak      int32
	completed int32
	release   chan struct{}
}

func (w *errorTrackingWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	cur := atomic.AddInt32(&w.inFlight, 1)
	for {
		p := atomic.LoadInt32(&w.peak)
		if cur <= p || atomic.CompareAndSwapInt32(&w.peak, p, cur) {
			break
		}
	}
	defer atomic.AddInt32(&w.inFlight, -1)
	<-w.release
	atomic.AddInt32(&w.completed, 1)
	return nil, nil, fmt.Errorf("scrape failed")
}

func (w *errorTrackingWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (w *errorTrackingWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (w *errorTrackingWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (w *errorTrackingWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

// TestScrapePhase_PeakConcurrencyOnErrorPath verifies that scrape errors do
// not leak worker slots — the pool must still sustain MaxWorkers concurrency
// even when every scrape returns an error.
func TestScrapePhase_PeakConcurrencyOnErrorPath(t *testing.T) {
	const maxWorkers = 5
	const numFiles = 15

	release := make(chan struct{}, numFiles)
	wf := &errorTrackingWorkflow{release: release}

	files := make([]string, numFiles)
	for i := range files {
		files[i] = "file-" + string(rune('a'+i)) + ".mp4"
	}

	inputs := makeInputs(nil)
	inputs.WF = wf
	inputs.Concurrency = concurrencyConfig{MaxWorkers: maxWorkers, WorkerTimeout: 0}
	fmi := make(map[string]models.FileMatchInfo, numFiles)
	for _, f := range files {
		fmi[f] = models.FileMatchInfo{Path: f}
	}
	inputs.FileMatchInfo = fmi

	done := make(chan struct{})
	go func() {
		NewScrapePhase().Run(context.Background(), inputs, files, ScrapePhaseConfig{})
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&wf.peak) >= int32(maxWorkers) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < numFiles; i++ {
		release <- struct{}{}
	}
	<-done

	peak := int(atomic.LoadInt32(&wf.peak))
	t.Logf("error path: peak in-flight workers = %d (expected >= %d)", peak, maxWorkers)
	assert.GreaterOrEqual(t, peak, maxWorkers,
		"scrape phase must sustain MaxWorkers concurrency even on the error path; degradation indicates a slot leak on error returns")
	assert.Equal(t, int32(numFiles), atomic.LoadInt32(&wf.completed), "all files must complete")
}

// panicTrackingWorkflow panics on every Scrape call while tracking peak
// in-flight concurrency before the panic.
type panicTrackingWorkflow struct {
	inFlight  int32
	peak      int32
	completed int32
	release   chan struct{}
}

func (w *panicTrackingWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	cur := atomic.AddInt32(&w.inFlight, 1)
	for {
		p := atomic.LoadInt32(&w.peak)
		if cur <= p || atomic.CompareAndSwapInt32(&w.peak, p, cur) {
			break
		}
	}
	defer atomic.AddInt32(&w.inFlight, -1)
	<-w.release
	atomic.AddInt32(&w.completed, 1)
	panic("boom")
}

func (w *panicTrackingWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (w *panicTrackingWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (w *panicTrackingWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (w *panicTrackingWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

// TestScrapePhase_PeakConcurrencyOnPanicPath verifies that a panicking scrape
// does not leak worker slots — withFileRecovery must recover and release the
// slot so the pool sustains MaxWorkers concurrency.
func TestScrapePhase_PeakConcurrencyOnPanicPath(t *testing.T) {
	const maxWorkers = 5
	const numFiles = 15

	release := make(chan struct{}, numFiles)
	wf := &panicTrackingWorkflow{release: release}

	files := make([]string, numFiles)
	for i := range files {
		files[i] = "file-" + string(rune('a'+i)) + ".mp4"
	}

	inputs := makeInputs(nil)
	inputs.WF = wf
	inputs.Concurrency = concurrencyConfig{MaxWorkers: maxWorkers, WorkerTimeout: 0}
	fmi := make(map[string]models.FileMatchInfo, numFiles)
	for _, f := range files {
		fmi[f] = models.FileMatchInfo{Path: f}
	}
	inputs.FileMatchInfo = fmi

	done := make(chan struct{})
	go func() {
		NewScrapePhase().Run(context.Background(), inputs, files, ScrapePhaseConfig{})
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&wf.peak) >= int32(maxWorkers) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < numFiles; i++ {
		release <- struct{}{}
	}
	<-done

	peak := int(atomic.LoadInt32(&wf.peak))
	t.Logf("panic path: peak in-flight workers = %d (expected >= %d)", peak, maxWorkers)
	assert.GreaterOrEqual(t, peak, maxWorkers,
		"scrape phase must sustain MaxWorkers concurrency even on the panic path; degradation indicates a slot leak in the recovery path")
	assert.Equal(t, int32(numFiles), atomic.LoadInt32(&wf.completed), "all files must complete")
}

// serializingPersistRepo simulates the SQLite single-writer serialization that
// QuickLion identified as the steady-state cause of the 5→1 worker degradation:
// every UpsertWithTranslations call acquires a shared mutex and sleeps briefly,
// modeling the DB writer lock + the heavier UpsertWithTranslations transaction.
// Used by TestScrapePhase_PersistOffloadedSustainsConcurrency to prove that
// offloading persist to a dedicated pool lets the scrape goroutines stay
// concurrent while serialization happens off their critical path.
type serializingPersistRepo struct {
	mu      sync.Mutex
	upserts int32
}

func (r *serializingPersistRepo) UpsertWithTranslations(_ context.Context, movie *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	atomic.AddInt32(&r.upserts, 1)
	r.mu.Lock()
	defer r.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	return movie, nil
}

// The remaining MovieRepositoryInterface methods are no-ops — only
// UpsertWithTranslations is exercised by the scrape phase's persist pool.
func (r *serializingPersistRepo) Create(_ context.Context, _ *models.Movie) error { return nil }
func (r *serializingPersistRepo) Update(_ context.Context, _ *models.Movie) error { return nil }
func (r *serializingPersistRepo) Upsert(_ context.Context, movie *models.Movie) (*models.Movie, error) {
	return movie, nil
}
func (r *serializingPersistRepo) FindByID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (r *serializingPersistRepo) FindByContentID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (r *serializingPersistRepo) Delete(_ context.Context, _ string) error { return nil }
func (r *serializingPersistRepo) List(_ context.Context, _, _ int) ([]models.Movie, error) {
	return nil, nil
}

// serializingWorkflow simulates a real scrape: a short fixed sleep (HTTP
// latency) followed by a shared-mutex critical section (per-source rate
// limiter). It honors cmd.SkipPersist so that, with Fix A, the DB persist is
// done by the offloaded persist pool rather than inline on the scrape goroutine.
type serializingWorkflow struct {
	inFlight  int32
	peak      int32
	completed int32
	mu        sync.Mutex // simulates the per-source rate limiter
	repo      *serializingPersistRepo
}

func (w *serializingWorkflow) Scrape(_ context.Context, cmd scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	cur := atomic.AddInt32(&w.inFlight, 1)
	for {
		p := atomic.LoadInt32(&w.peak)
		if cur <= p || atomic.CompareAndSwapInt32(&w.peak, p, cur) {
			break
		}
	}
	defer atomic.AddInt32(&w.inFlight, -1)

	// Simulate HTTP latency (overlapped across workers).
	time.Sleep(25 * time.Millisecond)

	// Simulate the per-source rate limiter — serializes requests within a source.
	w.mu.Lock()
	time.Sleep(5 * time.Millisecond)
	w.mu.Unlock()

	// pre-Fix-A behavior: inline DB persist on the scrape goroutine. SkipPersist
	// is only false when no MovieRepo is wired (single-scrape paths). The batch
	// path sets SkipPersist=true via buildScrapeCmd, so the persist runs in the
	// offloaded pool instead.
	if !cmd.SkipPersist && w.repo != nil {
		_, _ = w.repo.UpsertWithTranslations(context.Background(), makeScrapeResult(cmd.MovieID).Movie, nil, nil)
	}
	atomic.AddInt32(&w.completed, 1)
	return makeScrapeResult(cmd.MovieID), &workflow.OrchestrationMeta{}, nil
}

func (w *serializingWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (w *serializingWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (w *serializingWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (w *serializingWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

// TestScrapePhase_PersistOffloadedSustainsConcurrency is the Fix A regression
// test. Unlike the release-gated tests above (which hold worker slots open via
// a channel and bypass both the rate limiter and the DB persist), this stub
// self-completes each Scrape after a short sleep + a shared-mutex critical
// section, and the persist runs through a serializing MovieRepo in the
// offloaded persist pool.
//
// With Fix A (SkipPersist=true on the batch path), the scrape goroutines are
// never blocked by the serializing DB writer — they sustain MaxWorkers
// concurrency at steady state, not only at t=0. The persist pool serializes
// independently, and Persisted=true is observed on every result once the
// phase completes.
//
// If the offload is reverted (SkipPersist forced false), the inline persist
// would serialize each scrape goroutine on the DB writer and the peak in-flight
// count would collapse as the errgroup drains past the serial bottleneck.
func TestScrapePhase_PersistOffloadedSustainsConcurrency(t *testing.T) {
	const maxWorkers = 5
	const numFiles = 20

	repo := &serializingPersistRepo{}
	wf := &serializingWorkflow{repo: repo}

	files := make([]string, numFiles)
	for i := range files {
		files[i] = fmt.Sprintf("file-%02d.mp4", i)
	}

	inputs := makeInputs(nil)
	inputs.WF = wf
	inputs.MovieRepo = repo
	inputs.Concurrency = concurrencyConfig{MaxWorkers: maxWorkers, WorkerTimeout: 0}
	fmi := make(map[string]models.FileMatchInfo, numFiles)
	for _, f := range files {
		fmi[f] = models.FileMatchInfo{Path: f}
	}
	inputs.FileMatchInfo = fmi
	// Matcher stays nil — buildScrapeCmd falls back to filepath.Base so each file
	// gets a distinct movie ID (file-00, file-01, ...).

	done := make(chan struct{})
	go func() {
		NewScrapePhase().Run(context.Background(), inputs, files, ScrapePhaseConfig{})
		close(done)
	}()
	<-done

	peak := int(atomic.LoadInt32(&wf.peak))
	t.Logf("serializing persist: peak in-flight workers = %d (expected >= %d)", peak, maxWorkers)
	assert.GreaterOrEqual(t, peak, maxWorkers,
		"scrape phase must sustain MaxWorkers concurrent workers while the DB persist serializes in the offloaded pool; degradation indicates the persist is back on the scrape critical path")
	assert.Equal(t, int32(numFiles), atomic.LoadInt32(&wf.completed), "all files must complete")
	assert.Equal(t, int32(numFiles), atomic.LoadInt32(&repo.upserts), "every scraped movie must be persisted")

	// Persisted flag must be flipped on each result by the async persist pool.
	updater := inputs.Updater.(*stubUpdater)
	for _, f := range files {
		r := updater.getResult(f)
		require.NotNil(t, r, "result for %s", f)
		assert.True(t, r.Persisted, "result %s must be marked persisted", f)
	}
}
