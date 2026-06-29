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
	if s.scrapeResult == nil {
		return nil, nil, s.scrapeErr
	}
	// Return a fresh copy per call so concurrent scrape workers (MaxWorkers>1)
	// don't share a Movie pointer — establishScrapedBaseline writes to
	// fileResult.Movie's Original* fields and would race under -race if two
	// goroutines received the same pointer.
	clone := *s.scrapeResult
	if s.scrapeResult.Movie != nil {
		movieClone := *s.scrapeResult.Movie
		clone.Movie = &movieClone
	}
	return &clone, nil, s.scrapeErr
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

func TestBuildScrapeCmd_ManualInputTrimmedForMovieIDAndRawInput(t *testing.T) {
	const file = "/videos/ABC-001.mp4"
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "ABC-001"}}

	cmd := buildScrapeCmd(file, inputs, ScrapePhaseConfig{RawInputOverride: map[string]string{file: "  IPX-123  "}})

	assert.Equal(t, "IPX-123", cmd.MovieID, "MovieID is trimmed so failure JobEvents + row-identity identify the row, not '  IPX-123  '")
	assert.Equal(t, "IPX-123", cmd.RawInput, "RawInput is trimmed (mirrors rescrape_phase.go:203 queryOverride = TrimSpace(ManualSearchInput))")
}

func TestBuildScrapeCmd_EmptyOrWhitespaceManualInputIsNoOverride(t *testing.T) {
	const file = "/videos/ABC-001.mp4"
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "ABC-001"}}

	for _, raw := range []string{"", "   ", "\t\n"} {
		cmd := buildScrapeCmd(file, inputs, ScrapePhaseConfig{RawInputOverride: map[string]string{file: raw}})
		assert.Equal(t, "ABC-001", cmd.MovieID, "empty/whitespace input %q should be no override (auto-ID from matcher)", raw)
		assert.Equal(t, "", cmd.RawInput, "empty/whitespace input %q should not set RawInput", raw)
	}
}

func TestBuildScrapeCmd_URLInputBypassesBatchGlobalScrapers_IDInputKeepsThem(t *testing.T) {
	const file = "/videos/ABC-001.mp4"
	batchGlobal := []string{"r18dev", "dmm"}
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "ABC-001"}}

	urlCmd := buildScrapeCmd(file, inputs, ScrapePhaseConfig{
		SelectedScrapers: batchGlobal,
		RawInputOverride: map[string]string{file: "https://www.javbus.com/ABC-001"},
	})
	assert.Equal(t, "https://www.javbus.com/ABC-001", urlCmd.RawInput)
	assert.Empty(t, urlCmd.SelectedScrapers, "URL rows bypass the batch-global scraper picker so resolveScrapeInput sets PriorityOverride")

	idCmd := buildScrapeCmd(file, inputs, ScrapePhaseConfig{
		SelectedScrapers: batchGlobal,
		RawInputOverride: map[string]string{file: "ABC-001"},
	})
	assert.Equal(t, "ABC-001", idCmd.RawInput)
	assert.Equal(t, batchGlobal, idCmd.SelectedScrapers, "ID rows keep the batch-global scrapers so resolveScrapeInput reorders them")
}

// A manual URL input carrying a query token must be redacted from cmd.MovieID
// (which surfaces in persisted job state, WebSocket events, and progress
// messages) while cmd.RawInput keeps the raw URL so resolveScrapeInput/ScrapeURL
// still see it. Regression guard for the buildScrapeCmd redaction seam.
func TestBuildScrapeCmd_ManualURLInput_RedactsQueryFromMovieID(t *testing.T) {
	const file = "/videos/ABC-001.mp4"
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "ABC-001"}}

	cmd := buildScrapeCmd(file, inputs, ScrapePhaseConfig{
		RawInputOverride: map[string]string{file: "https://www.javbus.com/ABC-001?token=secret&sig=abc"},
	})

	assert.Equal(t, "https://www.javbus.com/ABC-001", cmd.MovieID, "MovieID must strip the query/fragment so tokens don't leak to persisted state/events")
	assert.Equal(t, "https://www.javbus.com/ABC-001?token=secret&sig=abc", cmd.RawInput, "RawInput must stay unredacted so the scraper receives the real URL")
	assert.Empty(t, cmd.SelectedScrapers, "URL rows bypass the batch-global scraper picker")
}

// A manual ID input is unaffected by redaction — RedactURLQuery passes plain
// IDs (no scheme/host) through unchanged.
func TestBuildScrapeCmd_ManualIDInput_RedactionIsNoOp(t *testing.T) {
	const file = "/videos/ABC-001.mp4"
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "ABC-001"}}

	cmd := buildScrapeCmd(file, inputs, ScrapePhaseConfig{
		RawInputOverride: map[string]string{file: "IPX-123"},
	})
	assert.Equal(t, "IPX-123", cmd.MovieID)
	assert.Equal(t, "IPX-123", cmd.RawInput)
}

func TestBuildScrapeCmd_NoManualInputAutoIDsFromMatcher(t *testing.T) {
	const file = "/videos/ABC-001.mp4"
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "ABC-001"}}
	cfg := ScrapePhaseConfig{}

	cmd := buildScrapeCmd(file, inputs, cfg)

	assert.Equal(t, "ABC-001", cmd.MovieID, "no manual input: the matcher result is used as the MovieID")
	assert.Equal(t, "", cmd.RawInput, "no manual input: RawInput stays empty so resolveScrapeInput is a no-op")
}

func TestBuildScrapeCmd_ManualInputUsedAsIDBypassingMatcher(t *testing.T) {
	const file = "/videos/MANUAL-123.mp4"
	inputs := scrapePhaseInputs{Matcher: &stubMatcher{result: "MATCHED-001"}}
	cfg := ScrapePhaseConfig{RawInputOverride: map[string]string{file: "MANUAL-123"}}

	cmd := buildScrapeCmd(file, inputs, cfg)

	assert.Equal(t, "MANUAL-123", cmd.MovieID, "manual input is used as the MovieID, not the matcher result")
	assert.Equal(t, "MANUAL-123", cmd.RawInput, "RawInput carries the manual input so resolveScrapeInput parses it downstream")
	assert.NotEqual(t, "MATCHED-001", cmd.MovieID, "the filename matcher is bypassed when a manual input is present")
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

// TestScrapePhase_Run_EstablishesScrapedBaseline verifies the initial scrape
// eagerly sets the poster-original revert group (Original*) to the scraper's
// value, so the review UI's Reset has a baseline immediately — symmetric with
// the rescrape path. Without this, Original* stays empty until the first
// manual edit snapshots it lazily via backupPosterOriginals.
func TestScrapePhase_Run_EstablishesScrapedBaseline(t *testing.T) {
	movie := &models.Movie{
		ID:    "TEST-001",
		Title: "Scraped Title",
	}
	movie.Poster.PosterURL = "https://scraper.invalid/poster.jpg"
	movie.Poster.CoverURL = "https://scraper.invalid/cover.jpg"
	movie.Poster.ShouldCropPoster = true
	wf := &stubWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: movie}}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	require.NotNil(t, r.Movie)
	assert.Equal(t, "https://scraper.invalid/poster.jpg", r.Movie.Poster.OriginalPosterURL)
	assert.Equal(t, "https://scraper.invalid/cover.jpg", r.Movie.Poster.OriginalCoverURL)
	if r.Movie.Poster.OriginalShouldCropPoster == nil || !*r.Movie.Poster.OriginalShouldCropPoster {
		t.Fatal("OriginalShouldCropPoster should mirror the scraped ShouldCropPoster (true)")
	}
	// No PosterGen in this test, so CroppedPosterURL is empty and its baseline
	// mirrors that (the rescrape path captures the generated crop separately).
	assert.Equal(t, "", r.Movie.Poster.OriginalCroppedPosterURL)
}
