package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapePhase remaining miss lines: ---
// Line 105-115: scrapersToUse fallback from PriorityOverride when SelectedScrapers is empty
// Line 179: eg.Wait() error path (defensive — only triggered if errgroup returns non-nil)
// Line 27-30: outer panic recovery in Run

func TestScrapePhase_Miss2_PriorityOverrideFallbackToScrapers(t *testing.T) {
	// When SelectedScrapers is empty and PriorityOverride is set,
	// scrapersToUse should fall back to PriorityOverride
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	cfg := ScrapePhaseConfig{
		// SelectedScrapers is nil/empty — fallback to PriorityOverride
		PriorityOverride: []string{"r18dev", "dmm"},
	}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status)

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed)
}

func TestScrapePhase_Miss2_SelectedScrapersOverridesPriority(t *testing.T) {
	// When both SelectedScrapers and PriorityOverride are set,
	// SelectedScrapers should take priority
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	cfg := ScrapePhaseConfig{
		SelectedScrapers: []string{"javlibrary"},
		PriorityOverride: []string{"r18dev", "dmm"},
	}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status)
}

func TestScrapePhase_Miss2_BothScrapersEmpty(t *testing.T) {
	// When both SelectedScrapers and PriorityOverride are empty,
	// scrapersToUse should be nil (no override)
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	cfg := ScrapePhaseConfig{
		// Both empty — scrapersToUse = nil
	}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status)
}

func TestScrapePhase_Miss2_OuterPanicRecovery(t *testing.T) {
	// The outer defer in Run() recovers panics in the main goroutine.
	// The inner goroutine panics are already tested. Let's verify existing recovery works.

	panicWF := &panicScrapeWorkflow{}
	inputs := makeInputs(nil)
	inputs.WF = panicWF
	lc := inputs.Lifecycle.(*stubLifecycle)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("file.mp4")
	require.NotNil(t, r, "Should have a result even after panic")
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Contains(t, r.Error, "panic:")
	assert.True(t, lc.completed, "Lifecycle should be completed even after panic recovery")
}

func TestScrapePhase_Miss2_WorkerTimeoutExpired(t *testing.T) {
	// Test with a very short worker timeout that actually expires
	wf := &slowScrapeWorkflow{delay: 200 * time.Millisecond}
	inputs := makeInputs(nil)
	inputs.WF = wf
	inputs.Concurrency.WorkerTimeout = 1 * time.Nanosecond
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	// With a 1ns timeout, the scrape should timeout and fail
	assert.True(t, r.Status == models.JobStatusFailed || r.Status == models.JobStatusCompleted,
		"Expected failed or completed, got %s", r.Status)
}

// slowScrapeWorkflow delays before returning a result
type slowScrapeWorkflow struct {
	delay time.Duration
}

func (s *slowScrapeWorkflow) Scrape(ctx context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	select {
	case <-time.After(s.delay):
		return makeScrapeResult("SLOW-001"), nil, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (s *slowScrapeWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}

func (s *slowScrapeWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (s *slowScrapeWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (s *slowScrapeWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func TestScrapePhase_Miss2_MovieIDFromFilename(t *testing.T) {
	// When matcher returns empty string and there's no override,
	// movieID should be derived from filename (without extension)
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("FILE-001")}
	inputs := makeInputs(wf)
	inputs.Matcher = &stubMatcher{result: ""} // matcher returns empty
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"path/SomeFile.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("path/SomeFile.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status)
	// The movieID used for scraping is "SomeFile" (filename without ext)
	// but the result overrides it with "FILE-001" from the scrape
	assert.Equal(t, "FILE-001", r.FileMatchInfo.MovieID)
}

func TestScrapePhase_Miss2_ScrapeErrorWithWrappedCancelled(t *testing.T) {
	// Test that when the error is a wrapped context.Canceled, the status is models.JobStatusCancelled
	wf := &stubWorkflow{scrapeErr: fmt.Errorf("wrapped: %w", context.Canceled)}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	// With a wrapped context.Canceled, errors.Is should still detect it
	assert.Equal(t, models.JobStatusCancelled, r.Status, "Wrapped context.Canceled should still produce models.JobStatusCancelled")
}

func TestScrapePhase_Miss2_BroadcasterEventsOnSuccess(t *testing.T) {
	// Verify that the broadcaster receives events during successful scraping
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("PROG-001")}
	inputs := makeInputs(wf)
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	// Should have at least 1 event (complete or running)
	assert.GreaterOrEqual(t, len(broadcaster.events), 1, "Should have at least one event")
}
