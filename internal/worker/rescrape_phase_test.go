package worker

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRescrapeWorkflow implements workflow.WorkflowInterface for RescrapePhase tests.
type stubRescrapeWorkflow struct {
	scrapeResult *scrape.ScrapeResult
	scrapeErr    error
	mu           sync.Mutex
	scrapeCalled int
}

func (s *stubRescrapeWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	s.mu.Lock()
	s.scrapeCalled++
	s.mu.Unlock()
	return s.scrapeResult, nil, s.scrapeErr
}

func (s *stubRescrapeWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}

func (s *stubRescrapeWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (s *stubRescrapeWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (s *stubRescrapeWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

// stubResultMap implements ResultMapAccessor for testing CompleteRescrape.
type stubResultMap struct {
	results   map[string]*MovieResult
	matchInfo map[string]models.FileMatchInfo
	gone      bool
	commitErr error
	mu        sync.Mutex
	committed []string // tracks CommitResult calls
}

func newStubResultMap() *stubResultMap {
	return &stubResultMap{
		results:   make(map[string]*MovieResult),
		matchInfo: make(map[string]models.FileMatchInfo),
	}
}

func (s *stubResultMap) IsGone() bool { return s.gone }

func (s *stubResultMap) GetFileMatchInfo(filePath string) (models.FileMatchInfo, bool) {
	info, ok := s.matchInfo[filePath]
	return info, ok
}

func (s *stubResultMap) GetCurrentMovieID(filePath string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.results[filePath]; r != nil {
		if r.Movie != nil && r.Movie.ID != "" {
			return r.Movie.ID
		}
		return r.FileMatchInfo.MovieID
	}
	return ""
}

func (s *stubResultMap) GetRevision(filePath string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.results[filePath]; r != nil {
		return r.Revision
	}
	return 0
}

func (s *stubResultMap) CommitResult(filePath string, result *MovieResult, expectedRevision uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.commitErr != nil {
		return s.commitErr
	}
	current := s.results[filePath]
	currentRev := uint64(0)
	if current != nil {
		currentRev = current.Revision
	}
	if currentRev != expectedRevision {
		return fmt.Errorf("conflict: expected revision %d, got %d", expectedRevision, currentRev)
	}
	if info, ok := s.matchInfo[filePath]; ok {
		result.FileMatchInfo = info
	}
	result.Revision = expectedRevision + 1
	s.results[filePath] = result
	s.committed = append(s.committed, filePath)
	return nil
}

func (s *stubResultMap) OtherResultUsesMovieID(excludePath string, movieID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for fp, r := range s.results {
		if fp == excludePath {
			continue
		}
		if r != nil && r.FileMatchInfo.MovieID == movieID {
			return true
		}
	}
	return false
}

func (s *stubResultMap) getCommitted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.committed...)
}

func (s *stubResultMap) GetMovieResult(filePath string) (*MovieResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.results[filePath]
	if !ok || r == nil {
		return nil, fmt.Errorf("not found: %s", filePath)
	}
	return r, nil
}

func (s *stubResultMap) CloneFileMatchInfo() map[string]models.FileMatchInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := make(map[string]models.FileMatchInfo, len(s.matchInfo))
	for k, v := range s.matchInfo {
		clone[k] = v
	}
	return clone
}

func (s *stubResultMap) SnapshotData() ResultSnapshot {
	return ResultSnapshot{}
}

func (s *stubResultMap) IsAllExcluded() bool { return false }

func makeRescrapeInputs(wf workflow.WorkflowInterface) rescrapePhaseInputs {
	return rescrapePhaseInputs{
		JobID:       "test-rescrape-001",
		Concurrency: concurrencyConfig{MaxWorkers: 1, WorkerTimeout: 0},
		WF:          wf,
		ResultMap:   newStubResultMap(),
		Lifecycle:   &stubLifecycle{},
		persister:   nil,
	}
}

func TestRescrapePhase_ScrapeSingle_Success(t *testing.T) {
	wf := &stubRescrapeWorkflow{
		scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: "IPX-777"},
		},
	}
	inputs := makeRescrapeInputs(wf)

	phase := NewRescrapePhase()
	result, _, err := phase.ScrapeSingle(context.Background(), inputs, "/source/IPX-777.mp4", scrape.ScrapeCmd{MovieID: "IPX-777"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-777", result.Movie.ID)
}

func TestRescrapePhase_ScrapeSingle_Error(t *testing.T) {
	wf := &stubRescrapeWorkflow{
		scrapeErr: fmt.Errorf("network timeout"),
	}
	inputs := makeRescrapeInputs(wf)

	phase := NewRescrapePhase()
	result, _, err := phase.ScrapeSingle(context.Background(), inputs, "/source/IPX-777.mp4", scrape.ScrapeCmd{MovieID: "IPX-777"})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestRescrapePhase_Rescrape_FailedStatusPropagatesVerboseError(t *testing.T) {
	// The scrape package populates ScrapeResult.Message with a verbose,
	// per-scraper failure summary via buildNoResultsError (e.g.
	// "No results from any scraper: fc2: movie PPV-2856053 not found on FC2").
	// RescrapePhase.Rescrape must surface that verbatim on the failed
	// RescrapeResult rather than the generic "scrape failed for <id>" —
	// otherwise rescrape callers (single + bulk) lose all per-scraper context.
	// Mirrors the ScrapePhase fix from commit 42d89e65.
	verboseMsg := "No results from any scraper: fc2: movie PPV-2856053 not found on FC2"
	wf := &stubRescrapeWorkflow{
		scrapeResult: &scrape.ScrapeResult{
			Movie:   nil,
			Status:  scrape.StatusFailed,
			Message: verboseMsg,
		},
	}
	rt := NewResultTracker(1, []string{"f1.mp4"})
	inputs := rescrapePhaseInputs{
		WF:        wf,
		ResultMap: rt,
		Finder:    rt,
		JobID:     models.NewJobID(),
	}

	phase := NewRescrapePhase()
	cmd := RescrapeCmd{MovieID: "PPV-2856053", FilePath: "f1.mp4"}
	result, err := phase.Rescrape(context.Background(), inputs, cmd)
	require.NoError(t, err, "StatusFailed is not returned as a Go error")
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Equal(t, verboseMsg, result.Error,
		"rescrape failure must surface the scrape package's verbose per-scraper message")
	assert.Contains(t, result.Error, "fc2")
	assert.Contains(t, result.Error, "not found on FC2")
}

func TestRescrapePhase_Rescrape_BackfillsNameAndExtensionOnMapMiss(t *testing.T) {
	// Regression: rescrape_phase.go previously constructed a partial
	// `models.FileMatchInfo{Path: lookup.FilePath}` for the post-rescrape
	// MovieResult — missing Name + Extension. CompleteRescrape.CommitResult
	// restores from the tracker when the tracker has an entry, but on a
	// tracker map-miss (nil map or path-normalization mismatch) the partial
	// struct persisted, leaving the subsequent organize preview with empty
	// Extension → video row rendered as `ABF-346` (no `.mp4`).
	//
	// With the fix, the fallback fmi carries Name + Extension derived from
	// lookup.FilePath (mirroring scanner.go and scrape_phase.go).
	wf := &stubRescrapeWorkflow{
		scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "ABF-346"}},
	}
	rt := NewResultTracker(1, []string{"f1.mp4"})
	// Empty tracker — file is registered so CommitResult runs, but the
	// FileMatchInfo map has no entry for this path. CommitResult's restore
	// guard `if info, ok := ru.FileMatchInfo[filePath]; ok` returns false,
	// so the partial fmi persists into the stored MovieResult.
	inputs := rescrapePhaseInputs{
		WF:        wf,
		ResultMap: rt,
		Finder:    rt,
		JobID:     models.NewJobID(),
	}

	phase := NewRescrapePhase()
	cmd := RescrapeCmd{MovieID: "ABF-346", FilePath: "source/ABF-346.mp4"}
	result, err := phase.Rescrape(context.Background(), inputs, cmd)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)

	// Tracker's FileMatchInfo map misses this path (map-miss scenario). The
	// fallback fmi from the rescrape phase is what persists into the stored
	// MovieResult. Read it back via Results and verify Name + Extension were
	// backfilled (i.e. the fallback wasn't just {Path: lookup.FilePath}).
	results := rt.GetMovieResultsForMovieID("ABF-346")
	require.NotEmpty(t, results)
	r := results[0]
	assert.Equal(t, "source/ABF-346.mp4", r.FileMatchInfo.Path)
	assert.Equal(t, "ABF-346.mp4", r.FileMatchInfo.Name,
		"rescrape fallback fmi.Name must be backfilled from filepath.Base(lookup.FilePath)")
	assert.Equal(t, ".mp4", r.FileMatchInfo.Extension,
		"rescrape fallback fmi.Extension must be backfilled from filepath.Ext(lookup.FilePath) — "+
			"without it the organize preview renders the video row as 'ABF-346' (no extension)")
}

func TestRescrapePhase_ScrapeSingle_NilWorkflow(t *testing.T) {
	inputs := makeRescrapeInputs(nil) // nil WF

	phase := NewRescrapePhase()
	result, _, err := phase.ScrapeSingle(context.Background(), inputs, "/source/IPX-777.mp4", scrape.ScrapeCmd{MovieID: "IPX-777"})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "workflow not configured")
}

func TestRescrapePhase_CompleteRescrape_Success(t *testing.T) {
	rm := newStubResultMap()
	rm.results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Revision:      1,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	inputs := makeRescrapeInputs(nil)
	inputs.ResultMap = rm

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-778"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-778"},
	}

	phase := NewRescrapePhase()
	outcome, err := phase.CompleteRescrape(inputs, "/source/IPX-777.mp4", newResult, 1, "IPX-778", "IPX-777")
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusSuccess, outcome.Status, "Should be success")
	assert.Contains(t, outcome.OrphanedMovieIDs, "IPX-777", "Old movie ID should be orphaned")

	committed := rm.getCommitted()
	assert.Contains(t, committed, "/source/IPX-777.mp4")
}

func TestRescrapePhase_CompleteRescrape_Conflict(t *testing.T) {
	rm := newStubResultMap()
	rm.results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Revision:      5, // Revision is 5, but we'll pass 1 as capturedRevision
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	inputs := makeRescrapeInputs(nil)
	inputs.ResultMap = rm

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-778"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-778"},
	}

	phase := NewRescrapePhase()
	outcome, err := phase.CompleteRescrape(inputs, "/source/IPX-777.mp4", newResult, 1, "IPX-778", "IPX-777")
	require.NoError(t, err) // CompleteRescrape doesn't return error for conflicts, just sets flag
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusConflict, outcome.Status, "Should detect conflict when revision mismatches")
}

func TestRescrapePhase_CompleteRescrape_JobGone(t *testing.T) {
	rm := newStubResultMap()
	rm.gone = true // Job is gone

	inputs := makeRescrapeInputs(nil)
	inputs.ResultMap = rm

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-778"},
		Status:        models.JobStatusCompleted,
	}

	phase := NewRescrapePhase()
	outcome, err := phase.CompleteRescrape(inputs, "/source/IPX-777.mp4", newResult, 0, "IPX-778", "IPX-777")
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusGone, outcome.Status, "Should report job is gone")
}

func TestRescrapePhase_CompleteRescrape_MultipartMetadata(t *testing.T) {
	rm := newStubResultMap()
	rm.results["/source/IPX-777-pt1.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777-pt1.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Revision:      1,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	rm.matchInfo["/source/IPX-777-pt1.mp4"] = models.FileMatchInfo{
		MovieID:     "IPX-777",
		IsMultiPart: true,
		PartNumber:  1,
		PartSuffix:  "pt1",
	}

	inputs := makeRescrapeInputs(nil)
	inputs.ResultMap = rm

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777-pt1.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	phase := NewRescrapePhase()
	outcome, err := phase.CompleteRescrape(inputs, "/source/IPX-777-pt1.mp4", newResult, 1, "IPX-777", "IPX-777")
	require.NoError(t, err)
	require.NotNil(t, outcome)

	// Verify that multipart metadata was applied
	committed := rm.results["/source/IPX-777-pt1.mp4"]
	require.NotNil(t, committed)
	assert.True(t, committed.FileMatchInfo.IsMultiPart, "IsMultiPart should be set from models.FileMatchInfo")
	assert.Equal(t, 1, committed.FileMatchInfo.PartNumber, "PartNumber should be set from models.FileMatchInfo")
	assert.Equal(t, "pt1", committed.FileMatchInfo.PartSuffix, "PartSuffix should be set from models.FileMatchInfo")
}

func TestRescrapePhase_CompleteRescrape_SameMovieID(t *testing.T) {
	rm := newStubResultMap()
	rm.results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Revision:      1,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	inputs := makeRescrapeInputs(nil)
	inputs.ResultMap = rm

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"}, // Same movie ID
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	phase := NewRescrapePhase()
	outcome, err := phase.CompleteRescrape(inputs, "/source/IPX-777.mp4", newResult, 1, "IPX-777", "IPX-777")
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Empty(t, outcome.OrphanedMovieIDs, "No orphaned IDs when movie ID stays the same")
}

func TestRescrapePhase_CompleteRescrape_CommitResultError(t *testing.T) {
	rm := newStubResultMap()
	rm.results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Revision:      1,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	rm.commitErr = fmt.Errorf("database error")

	inputs := makeRescrapeInputs(nil)
	inputs.ResultMap = rm

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-778"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-778"},
	}

	phase := NewRescrapePhase()
	outcome, err := phase.CompleteRescrape(inputs, "/source/IPX-777.mp4", newResult, 1, "IPX-778", "IPX-777")
	require.Error(t, err, "Should return error when CommitResult fails")
	require.Nil(t, outcome, "Should return nil outcome for non-conflict errors")
}
