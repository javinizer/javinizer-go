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

// --- ScrapePhase miss lines: context cancellation, worker timeout,
// MovieIDOverride, PriorityOverride, scrape error (cancelled vs failed),
// nil result, matcher nil, panic recovery ---

func TestScrapePhase_Run_ContextCancelled(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	lc := inputs.Lifecycle.(*stubLifecycle)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run

	NewScrapePhase().Run(ctx, inputs, []string{"file.mp4"}, ScrapePhaseConfig{})
	assert.True(t, lc.cancelled, "Lifecycle.MarkCancelled should be called when context is cancelled")
}

func TestScrapePhase_Run_WorkerTimeout(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.Concurrency.WorkerTimeout = 1 * time.Nanosecond

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	// With a 1ns timeout, the scrape should still succeed or fail gracefully
	// (depends on goroutine scheduling), but should not hang
}

func TestScrapePhase_Run_MovieIDOverride(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("OVERRIDE-001")}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	cfg := ScrapePhaseConfig{
		MovieIDOverride: map[string]string{"file.mp4": "OVERRIDE-001"},
	}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status)
}

func TestScrapePhase_Run_MatcherNil(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.Matcher = nil
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"vids/ABC-123.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("vids/ABC-123.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status)
	// When matcher is nil, movieID starts as filename without extension,
	// but the scrape result overwrites it with the actual movie ID
	assert.Equal(t, "TEST-001", r.FileMatchInfo.MovieID)
}

func TestScrapePhase_Run_ScrapeError(t *testing.T) {
	wf := &stubWorkflow{scrapeErr: fmt.Errorf("network error")}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)
	lc := inputs.Lifecycle.(*stubLifecycle)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Contains(t, r.Error, "network error")
	assert.True(t, lc.completed, "Lifecycle should be completed even on scrape errors")
}

func TestScrapePhase_Run_ScrapeErrorCancelled(t *testing.T) {
	wf := &stubWorkflow{scrapeErr: context.Canceled}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCancelled, r.Status, "Should use models.JobStatusCancelled when error is context.Canceled")
}

func TestScrapePhase_Run_NilResult(t *testing.T) {
	// scrapeResult is nil → Scrape returns (nil, nil, nil)
	wf := &stubWorkflow{scrapeResult: nil, scrapeErr: nil}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Equal(t, "no result", r.Error)
}

func TestScrapePhase_Run_NilResultMovie(t *testing.T) {
	// scrapeResult.Movie is nil
	wf := &stubWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: nil}, scrapeErr: nil}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Equal(t, "no result", r.Error)
}

func TestScrapePhase_Run_PriorityOverride(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)

	cfg := ScrapePhaseConfig{
		PriorityOverride: []string{"r18dev", "dmm"},
	}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)
	// Verify no panic and lifecycle is completed
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed)
}

func TestScrapePhase_Run_SelectedScrapers(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)

	cfg := ScrapePhaseConfig{
		SelectedScrapers: []string{"r18dev"},
	}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed)
}

func TestScrapePhase_Run_FileMatchInfo(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.FileMatchInfo = map[string]models.FileMatchInfo{
		"file.mp4": {Path: "file.mp4", MovieID: "TEST-001", IsMultiPart: true},
	}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed)
}

func TestScrapePhase_Run_MultipleFiles(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.Concurrency.MaxWorkers = 2

	NewScrapePhase().Run(context.Background(), inputs, []string{"file1.mp4", "file2.mp4"}, ScrapePhaseConfig{})
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed)
}

func TestScrapePhase_Run_BroadcasterEvents(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	assert.NotEmpty(t, broadcaster.events, "Broadcaster should have events")
}

func TestScrapePhase_Run_ScrapeErrorBroadcastEvent(t *testing.T) {
	wf := &stubWorkflow{scrapeErr: fmt.Errorf("fail")}
	inputs := makeInputs(wf)
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	found := false
	for _, evt := range broadcaster.events {
		if evt.Step == StepFailed {
			found = true
			break
		}
	}
	assert.True(t, found, "Should broadcast a StepFailed event on scrape error")
}

func TestScrapePhase_Run_ProvenanceSet(t *testing.T) {
	result := makeScrapeResult("TEST-001")
	result.FieldSources = map[string]string{"title": "r18dev"}
	result.ActressSources = map[string]string{"actress_0": "dmm"}

	wf := &stubWorkflow{scrapeResult: result}
	inputs := makeInputs(wf)
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := updater.getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status)

	prov := updater.provenance["file.mp4"]
	require.NotNil(t, prov, "Provenance should be set when FieldSources/ActressSources are present")
	assert.Equal(t, "r18dev", prov.FieldSources["title"])
}

func TestScrapePhase_Run_PanicRecovery(t *testing.T) {
	// Create a workflow that panics inside Scrape
	panicWF := &panicScrapeWorkflow{}
	inputs := makeInputs(nil)
	inputs.WF = panicWF
	lc := inputs.Lifecycle.(*stubLifecycle)

	// This should NOT panic — the phase should recover
	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	// The outer defer recovers panics in the main function, not in goroutines.
	// The goroutine has its own panic recovery which should update the result.
	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("file.mp4")
	require.NotNil(t, r, "Should have a result even after panic")
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Contains(t, r.Error, "panic:")
	assert.True(t, lc.completed, "Lifecycle should be completed even after panic recovery")
}

func TestScrapePhase_Run_PanicRecoveryPreservesTimestampsAndBroadcasts(t *testing.T) {
	// A panicked scrape goroutine must (1) keep StartedAt / EndedAt on the
	// failed MovieResult so the failed-files card timestamps stay populated,
	// and (2) emit a StepFailed JobEvent so the progress UI updates in real
	// time. Both were dropped on the refactor's panic path because
	// recoveryContext.startTime was left zero and rc.broadcast was nil.
	panicWF := &panicScrapeWorkflow{}
	inputs := makeInputs(nil)
	inputs.WF = panicWF
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, ScrapePhaseConfig{})

	r := inputs.Updater.(*stubUpdater).getResult("file.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.False(t, r.StartedAt.IsZero(), "panic-recovered MovieResult must keep StartedAt for the UI timeline")
	require.NotNil(t, r.EndedAt, "panic-recovered MovieResult must keep EndedAt for the UI timeline")
	assert.False(t, r.EndedAt.IsZero())

	// A StepFailed scrape JobEvent must be broadcast on panic, mirroring the
	// adjacent err != nil and no-result branches in interpretScrapeResult.
	var sawScrapeFailed bool
	for _, ev := range broadcaster.events {
		if ev.Phase == JobEventPhaseScrape && ev.Step == StepFailed {
			sawScrapeFailed = true
			break
		}
	}
	assert.True(t, sawScrapeFailed, "scrape panic must broadcast a StepFailed JobEvent")
}

// panicScrapeWorkflow panics when Scrape is called
type panicScrapeWorkflow struct{}

func (p *panicScrapeWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	panic("intentional test panic")
}
func (p *panicScrapeWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (p *panicScrapeWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (p *panicScrapeWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (p *panicScrapeWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func TestScrapePhase_Run_ForceRefresh(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)

	cfg := ScrapePhaseConfig{Force: true}
	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed)
}

func TestScrapePhase_Run_EmptyFiles(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)

	NewScrapePhase().Run(context.Background(), inputs, []string{}, ScrapePhaseConfig{})
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed)
}

func TestScrapePhase_Run_NoExtensionFile(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.Matcher = nil
	updater := inputs.Updater.(*stubUpdater)

	NewScrapePhase().Run(context.Background(), inputs, []string{"path/ABC-123"}, ScrapePhaseConfig{})

	r := updater.getResult("path/ABC-123")
	require.NotNil(t, r)
	// The scrape result overrides the movieID
	assert.Equal(t, "TEST-001", r.FileMatchInfo.MovieID)
}

// --- Regression: failed scrapes must propagate a MovieID label (file-basename
// fallback) to the batch progress UI. Previously, when the matcher missed and
// the scrape failed, fmi.MovieID stayed empty, so the frontend rendered
// `{movie_id || 'Unknown'}` in the failed-files section. This mirrors main's
// newFailedFileResult(filePath, query.MovieID, ...) behavior. ---

func TestScrapePhase_Run_FailedScrapePropagatesFallbackMovieID_NoResult(t *testing.T) {
	// Matcher misses (returns "") AND scrape returns no movie → failed result
	// must still carry the file-basename fallback as MovieID, matching main.
	wf := &stubWorkflow{scrapeResult: nil, scrapeErr: nil}
	inputs := makeInputs(wf)
	inputs.Matcher = &stubMatcher{result: ""} // simulate matcher miss
	updater := inputs.Updater.(*stubUpdater)

	filePath := "vids/unmatched-file.mp4"
	NewScrapePhase().Run(context.Background(), inputs, []string{filePath}, ScrapePhaseConfig{})

	r := updater.getResult(filePath)
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Equal(t, "no result", r.Error)
	// Without the fix, MovieID would be "" → frontend shows "Unknown".
	assert.Equal(t, "unmatched-file", r.FileMatchInfo.MovieID,
		"failed scrape must fall back to file basename for the progress-UI movie_id label")
}

func TestScrapePhase_Run_FailedScrapePropagatesFilePath(t *testing.T) {
	// Matcher misses (returns "") AND scrape returns no movie → failed result
	// must still carry the file path so the API response's file_path field is
	// non-empty and the frontend's UnidentifiedFilesCard renders basename(...).
	// Mirrors main's newFailedFileResult(filePath, ...) which set FilePath
	// directly from the scrape argument.
	wf := &stubWorkflow{scrapeResult: nil, scrapeErr: nil}
	inputs := makeInputs(wf)
	inputs.Matcher = &stubMatcher{result: ""} // simulate matcher miss
	updater := inputs.Updater.(*stubUpdater)

	filePath := "vids/unmatched-file.mp4"
	NewScrapePhase().Run(context.Background(), inputs, []string{filePath}, ScrapePhaseConfig{})

	r := updater.getResult(filePath)
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Equal(t, filePath, r.FileMatchInfo.Path,
		"failed scrape must carry the file path so the API response file_path is non-empty")
}

func TestScrapePhase_Run_BackfillsNameAndExtensionOnMapMiss(t *testing.T) {
	// Regression: when the in-memory FileMatchInfo map lacks an entry for
	// this filePath (scanner miss, nil map, or path-normalization mismatch
	// between the API request's submitted path and what the scanner returned),
	// `inputs.FileMatchInfo[filePath]` returns the ZERO value. Previously only
	// fmi.Path was backfilled (commit d9106a96); the resulting MovieResult
	// carried an empty Extension, so the organizer's resolveFileName built a
	// target path of `<templateOutput>` WITHOUT the `.mp4` suffix. The /review
	// organize preview then rendered the video row as `ABF-346` (no extension)
	// while NFO/poster/fanart rows looked fine (they derive from movie.ID).
	//
	// Backfills Name + Extension alongside Path, mirroring scanner.go's
	// FileMatchInfo construction (Name: filepath.Base(path);
	// Extension: filepath.Ext(path)).
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("ABF-346")}
	inputs := makeInputs(wf)
	// inputs.FileMatchInfo is intentionally nil — simulates the map-miss path.
	updater := inputs.Updater.(*stubUpdater)

	filePath := "vids/ABF-346.mp4"
	NewScrapePhase().Run(context.Background(), inputs, []string{filePath}, ScrapePhaseConfig{})

	r := updater.getResult(filePath)
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusCompleted, r.Status,
		"happy-path scrape must still complete successfully on a map-miss")
	assert.Equal(t, filePath, r.FileMatchInfo.Path,
		"fmi.Path must be backfilled from the scrape argument on map-miss")
	assert.Equal(t, "ABF-346.mp4", r.FileMatchInfo.Name,
		"fmi.Name must be backfilled from filepath.Base(filePath) on map-miss — "+
			"the organizer reads it for fallback filename resolution")
	assert.Equal(t, ".mp4", r.FileMatchInfo.Extension,
		"fmi.Extension must be backfilled from filepath.Ext(filePath) on map-miss — "+
			"without it the organize preview renders the video row as 'ABF-346' (no extension)")
}

func TestScrapePhase_Run_BackfillsNameAndExtensionOnMapMiss_FailedPath(t *testing.T) {
	// Same regression as the success-path test, but on the failed/no-result
	// path. The fmi that flows into the failure / no-result / panic-recovered
	// MovieResults is the same zero-value-then-backfilled struct, so Extension
	// must be populated here too for a future re-scrape + organize preview
	// of a never-successfully-scraped file to render its video row correctly.
	wf := &stubWorkflow{scrapeResult: nil, scrapeErr: nil}
	inputs := makeInputs(wf)
	inputs.Matcher = &stubMatcher{result: ""} // matcher miss
	updater := inputs.Updater.(*stubUpdater)

	filePath := "vids/unmatched-file.mkv"
	NewScrapePhase().Run(context.Background(), inputs, []string{filePath}, ScrapePhaseConfig{})

	r := updater.getResult(filePath)
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Equal(t, "unmatched-file.mkv", r.FileMatchInfo.Name)
	assert.Equal(t, ".mkv", r.FileMatchInfo.Extension)
}

func TestScrapePhase_Run_NoResultPropagatesVerboseErrorMessage(t *testing.T) {
	// The scrape package populates ScrapeResult.Message with a verbose,
	// per-scraper failure summary via buildNoResultsError, e.g.
	// "No results from any scraper: fc2: movie PPV-2856053 not found on FC2".
	// The worker phase must surface that message verbatim on the failed
	// MovieResult rather than the generic "no result" literal, so the
	// /jobs UI can show why a scrape failed.
	verboseMsg := "No results from any scraper: fc2: movie PPV-2856053 not found on FC2"
	wf := &stubWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie:   nil,
		Message: verboseMsg,
		Status:  scrape.StatusFailed,
	}}
	inputFields := makeInputs(wf)
	inputFields.Matcher = &stubMatcher{result: "PPV-2856053"}
	updater := inputFields.Updater.(*stubUpdater)
	lc := inputFields.Lifecycle.(*stubLifecycle)

	filePath := "vids/PPV-2856053.mp4"
	NewScrapePhase().Run(context.Background(), inputFields, []string{filePath}, ScrapePhaseConfig{})

	r := updater.getResult(filePath)
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Equal(t, verboseMsg, r.Error,
		"failed-no-result path must surface the scrape package's verbose per-scraper message")
	assert.Contains(t, r.Error, "fc2")
	assert.Contains(t, r.Error, "not found on FC2")
	assert.True(t, lc.completed, "Lifecycle should be completed even on no-result scrapes")
}

func TestScrapePhase_Run_FailedScrapePropagatesFallbackMovieID_ScrapeError(t *testing.T) {
	// Matcher misses AND scrape returns an error → failed/cancelled result
	// must still carry the file-basename fallback as MovieID.
	wf := &stubWorkflow{scrapeErr: fmt.Errorf("network error")}
	inputs := makeInputs(wf)
	inputs.Matcher = &stubMatcher{result: ""} // simulate matcher miss
	updater := inputs.Updater.(*stubUpdater)

	filePath := "vids/unmatched-file.mp4"
	NewScrapePhase().Run(context.Background(), inputs, []string{filePath}, ScrapePhaseConfig{})

	r := updater.getResult(filePath)
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Contains(t, r.Error, "network error")
	assert.Equal(t, "unmatched-file", r.FileMatchInfo.MovieID,
		"failed scrape must fall back to file basename for the progress-UI movie_id label")
}
