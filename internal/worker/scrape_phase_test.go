package worker

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Stub implementations for testing ScrapePhase without *BatchJob.

type stubBroadcaster struct {
	events []JobEvent
	closed bool
	mu     sync.Mutex
}

func (s *stubBroadcaster) Send(event JobEvent) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func (s *stubBroadcaster) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func (s *stubBroadcaster) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type stubUpdater struct {
	results    map[string]*MovieResult
	provenance map[string]*ProvenanceData
	mu         sync.Mutex
}

func newStubUpdater() *stubUpdater {
	return &stubUpdater{results: make(map[string]*MovieResult), provenance: make(map[string]*ProvenanceData)}
}

func (s *stubUpdater) UpdateFileResult(fp string, r *MovieResult) {
	s.mu.Lock()
	s.results[fp] = r
	s.mu.Unlock()
}

func (s *stubUpdater) SetProvenance(fp string, prov *ProvenanceData) {
	s.mu.Lock()
	s.provenance[fp] = prov
	s.mu.Unlock()
}

func (s *stubUpdater) AtomicUpdateFileResult(fp string, fn func(*MovieResult) (*MovieResult, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.results[fp]
	if current == nil {
		return fmt.Errorf("not found: %s", fp)
	}
	updated, err := fn(current)
	if err != nil {
		return err
	}
	s.results[fp] = updated
	return nil
}

func (s *stubUpdater) UpdateMovie(fp string, movie *models.Movie) error {
	return s.AtomicUpdateFileResult(fp, func(current *MovieResult) (*MovieResult, error) {
		current.Movie = movie
		return current, nil
	})
}

func (s *stubUpdater) MarkExcluded(fp string) {}

func (s *stubUpdater) getResult(fp string) *MovieResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.results[fp]
}

type stubLifecycle struct {
	completed bool
	failed    bool
	cancelled bool
	organized bool
}

func (s *stubLifecycle) MarkCompleted() { s.completed = true }
func (s *stubLifecycle) MarkFailed()    { s.failed = true }
func (s *stubLifecycle) MarkCancelled() { s.cancelled = true }
func (s *stubLifecycle) MarkOrganized() { s.organized = true }

type stubMatcher struct{ result string }

func (s *stubMatcher) MatchString(_ string) string                           { return s.result }
func (s *stubMatcher) Match(_ []models.FileMatchInfo) []matcher.MatchResult  { return nil }
func (s *stubMatcher) MatchFile(_ models.FileMatchInfo) *matcher.MatchResult { return nil }

// stubWorkflow implements workflow.WorkflowInterface for testing.
// Only Scrape is functional; other methods return nil/zero.
type stubWorkflow struct {
	scrapeResult *scrape.ScrapeResult
	scrapeErr    error
}

func (s *stubWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return s.scrapeResult, nil, s.scrapeErr
}

func (s *stubWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}

func (s *stubWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (s *stubWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (s *stubWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func makeScrapeResult(movieID string) *scrape.ScrapeResult {
	return &scrape.ScrapeResult{
		Movie: &models.Movie{
			ID: movieID,
		},
	}
}

func makeInputs(wf *stubWorkflow) scrapePhaseInputs {
	return scrapePhaseInputs{
		JobID:               "test-job-001",
		Concurrency:         concurrencyConfig{MaxWorkers: 1, WorkerTimeout: 0},
		WF:                  wf,
		Matcher:             &stubMatcher{result: "TEST-001"},
		KeepBroadcasterOpen: false,
		Broadcaster:         &stubBroadcaster{},
		Updater:             newStubUpdater(),
		Lifecycle:           &stubLifecycle{},
		persister:           nil,
	}
}

func TestScrapePhase_Run_Success(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	lc := inputs.Lifecycle.(*stubLifecycle)
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	assert.True(t, lc.completed, "Lifecycle.MarkCompleted should be called")
	r := updater.getResult("file.mp4")
	require.NotNil(t, r, "Updater should have a result for file.mp4")
	assert.Equal(t, models.JobStatusCompleted, r.Status)
	assert.Equal(t, "TEST-001", r.FileMatchInfo.MovieID)
}

func TestScrapePhase_Run_BroadcasterClosed(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.KeepBroadcasterOpen = false
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	assert.True(t, broadcaster.isClosed(), "Broadcaster.Close() should be called when KeepBroadcasterOpen=false")
}

func TestScrapePhase_Run_BroadcasterKeptOpen(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.KeepBroadcasterOpen = true
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	assert.False(t, broadcaster.isClosed(), "Broadcaster.Close() should NOT be called when KeepBroadcasterOpen=true")
}

func TestScrapePhase_Run_PersistFnCalled(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	persisted := false
	inputs.persister = persistFunc(func() { persisted = true })

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	assert.True(t, persisted, "PersistFn should be called after Run completes")
}

func TestPersistFunc_NilNoPanic(t *testing.T) {
	var p persistFunc = nil
	assert.NotPanics(t, func() { p.Persist() }, "Nil persistFunc should not panic")
}

func TestPersistFunc_CallsFunction(t *testing.T) {
	called := false
	p := persistFunc(func() { called = true })
	p.Persist()
	assert.True(t, called, "persistFunc should call the wrapped function")
}

func TestPersistFunc_SatisfiesPersister(t *testing.T) {
	var _ persister = persistFunc(nil)
	var _ persister = persistFunc(func() {})
}
