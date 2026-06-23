package worker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRescrapePhase implements RescrapePhase for controlled testing of BatchJob.Rescrape.
type mockRescrapePhase struct {
	scrapeSingleFn     func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error)
	completeRescrapeFn func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error)
	rescrapeFn         func(ctx context.Context, inputs rescrapePhaseInputs, cmd RescrapeCmd) (*RescrapeResult, error)
}

func (m *mockRescrapePhase) ScrapeSingle(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	if m.scrapeSingleFn != nil {
		return m.scrapeSingleFn(ctx, inputs, filePath, cmd)
	}
	return nil, nil, nil
}

func (m *mockRescrapePhase) CompleteRescrape(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
	if m.completeRescrapeFn != nil {
		return m.completeRescrapeFn(inputs, filePath, result, capturedRevision, movieID, oldMovieID)
	}
	return &RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
}

func (m *mockRescrapePhase) Rescrape(ctx context.Context, inputs rescrapePhaseInputs, cmd RescrapeCmd) (*RescrapeResult, error) {
	if m.rescrapeFn != nil {
		return m.rescrapeFn(ctx, inputs, cmd)
	}
	// Default: delegate to ScrapeSingle + CompleteRescrape (matching real implementation)
	var filePath string
	if cmd.FilePath != "" {
		filePath = cmd.FilePath
	} else if inputs.Finder != nil {
		lookup, err := inputs.Finder.FindFileForMovieID(cmd.MovieID)
		if err != nil {
			return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: err.Error()}, nil
		}
		filePath = lookup.FilePath
	}
	if filePath == "" {
		return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "no file path"}, nil
	}
	var queryOverride string
	var rawInput string
	if cmd.ManualSearchInput != "" {
		rawInput = cmd.ManualSearchInput
		if strings.HasPrefix(strings.ToLower(cmd.ManualSearchInput), "http://") ||
			strings.HasPrefix(strings.ToLower(cmd.ManualSearchInput), "https://") {
			queryOverride = cmd.ManualSearchInput
		} else {
			queryOverride = strings.TrimSpace(cmd.ManualSearchInput)
		}
	} else {
		queryOverride = cmd.MovieID
	}
	scrapeResult, _, err := m.ScrapeSingle(ctx, inputs, filePath, scrape.ScrapeCmd{MovieID: queryOverride, RawInput: rawInput, ForceRefresh: cmd.Force, SelectedScrapers: cmd.SelectedScrapers})
	if err != nil {
		return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: err.Error()}, nil
	}
	// For mock simplicity, just return success from CompleteRescrape
	outcome, err := m.CompleteRescrape(inputs, filePath, nil, 0, cmd.MovieID, "")
	if err != nil {
		return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: err.Error()}, nil
	}
	// Propagate provenance from scrape result if available
	if scrapeResult != nil {
		if scrapeResult.Status == scrape.StatusFailed {
			return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: fmt.Sprintf("scrape failed for %s", cmd.MovieID)}, nil
		}
		outcome.FieldSources = scrapeResult.FieldSources
		outcome.ActressSources = scrapeResult.ActressSources
		if scrapeResult.Movie != nil {
			outcome.Movie = scrapeResult.Movie
		}
	} else {
		return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "scrape produced no result"}, nil
	}
	outcome.FilePath = filePath
	return outcome, nil
}

// newJobWithRescrapeMock creates a BatchJob with pre-populated results and a mock rescrape phase.
// The caller explicitly provides wf — pass &stubRescrapeWF{} for freshly-created jobs,
// or nil for reconstructed jobs (job.deps.WF == nil). This makes the WF state explicit
// and prevents accidentally forgetting to clear job.deps.WF for reconstructed scenarios.
func newJobWithRescrapeMock(results map[string]*MovieResult, fileMatchInfo map[string]models.FileMatchInfo, wf workflow.WorkflowInterface) (*BatchJob, *mockRescrapePhase) {
	jq := NewJobStore(nil, nil, nil, os.TempDir(), nil, nil)
	job := jq.CreateJobBatch([]string{})
	job.results.Results = results
	if fileMatchInfo != nil {
		job.results.FileMatchInfo = fileMatchInfo
	}

	// Rebuild movieID index after bulk-setting results directly.
	job.results.rebuildMovieIDIndexLocked()

	if wf != nil {
		job.mu.Lock()
		job.deps.WF = wf
		job.mu.Unlock()
	}

	mockPhase := &mockRescrapePhase{}
	job.rescrapePhase = mockPhase

	return job, mockPhase
}

func TestBatchJob_FindFileForMovieID_Found(t *testing.T) {
	job, _ := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      3,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, nil)

	result, err := job.resultIndex.FindFileForMovieID("ABC-001")
	require.NoError(t, err)
	assert.Equal(t, "/path/to/file1.mp4", result.FilePath)
	assert.Equal(t, "ABC-001", result.OldMovieID)
	assert.Equal(t, uint64(3), result.CapturedRevision)
}

func TestBatchJob_FindFileForMovieID_NotFound(t *testing.T) {
	job, _ := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
		},
	}, nil, nil)

	result, err := job.resultIndex.FindFileForMovieID("XYZ-999")
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBatchJob_FindFileForMovieID_MultipartSorting(t *testing.T) {
	job, _ := newJobWithRescrapeMock(
		map[string]*MovieResult{
			"/path/partC.mp4": {
				FileMatchInfo: models.FileMatchInfo{Path: "/path/partC.mp4", MovieID: "ABC-001"},
				Revision:      1,
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "ABC-001"},
			},
			"/path/partA.mp4": {
				FileMatchInfo: models.FileMatchInfo{Path: "/path/partA.mp4", MovieID: "ABC-001"},
				Revision:      1,
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "ABC-001"},
			},
			"/path/partB.mp4": {
				FileMatchInfo: models.FileMatchInfo{Path: "/path/partB.mp4", MovieID: "ABC-001"},
				Revision:      2,
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "ABC-001"},
			},
		},
		map[string]models.FileMatchInfo{
			"/path/partA.mp4": {PartSuffix: "A", PartNumber: 1},
			"/path/partB.mp4": {PartSuffix: "B", PartNumber: 2},
			"/path/partC.mp4": {PartSuffix: "C", PartNumber: 3},
		},
		nil,
	)

	result, err := job.resultIndex.FindFileForMovieID("ABC-001")
	require.NoError(t, err)
	// Should select the file with the lowest PartNumber (partA)
	assert.Equal(t, "/path/partA.mp4", result.FilePath)
}

func TestBatchJob_Rescrape_Success(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, &stubRescrapeWF{})

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return &scrape.ScrapeResult{
			Movie:          &models.Movie{ID: "ABC-001"},
			Status:         scrape.StatusCompleted,
			FieldSources:   map[string]string{"title": "r18dev"},
			ActressSources: map[string]string{"actresses": "r18dev"},
			StartedAt:      time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{OrphanedMovieIDs: nil, Status: models.RescrapeStatusSuccess}, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
	assert.NotNil(t, result.Movie)
	assert.Equal(t, map[string]string{"title": "r18dev"}, result.FieldSources)
	assert.Equal(t, map[string]string{"actresses": "r18dev"}, result.ActressSources)
}

func TestBatchJob_Rescrape_ScrapeFailed(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
		},
	}, nil, &stubRescrapeWF{})

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return nil, nil, fmt.Errorf("network timeout")
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err) // Rescrape returns result with Status="failed", not a Go error
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "network timeout")
}

func TestBatchJob_Rescrape_ScrapeStatusFailed(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
		},
	}, nil, &stubRescrapeWF{})

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return &scrape.ScrapeResult{
			Status: scrape.StatusFailed,
		}, nil, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "scrape failed")
}

func TestBatchJob_Rescrape_ScrapeNilResult(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
		},
	}, nil, &stubRescrapeWF{})

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return nil, nil, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "scrape produced no result")
}

func TestBatchJob_Rescrape_Gone(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
		},
	}, nil, &stubRescrapeWF{})

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusGone}, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusGone, result.Status)
}

func TestBatchJob_Rescrape_Conflict(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
		},
	}, nil, &stubRescrapeWF{})

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusConflict, result.Status)
}

func TestBatchJob_Rescrape_CompleteRescrapeError(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
		},
	}, nil, &stubRescrapeWF{})

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return nil, fmt.Errorf("database connection lost")
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "database connection lost")
}

func TestBatchJob_Rescrape_MovieIDNotFoundInResults(t *testing.T) {
	job, _ := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
		},
	}, nil, &stubRescrapeWF{})

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "XYZ-999",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "not found")
}

func TestBatchJob_Rescrape_WithManualSearchInput(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, &stubRescrapeWF{})

	var capturedCmd scrape.ScrapeCmd
	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		capturedCmd = cmd
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "CUSTOM-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID:           "ABC-001",
		ManualSearchInput: "custom search query",
	})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
	assert.Equal(t, "custom search query", capturedCmd.MovieID)
	assert.Equal(t, "custom search query", capturedCmd.RawInput)
}

func TestBatchJob_Rescrape_WithURLManualSearchInput(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, &stubRescrapeWF{})

	var capturedCmd scrape.ScrapeCmd
	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		capturedCmd = cmd
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID:           "ABC-001",
		ManualSearchInput: "https://example.com/ABC-001",
	})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
	assert.Equal(t, "https://example.com/ABC-001", capturedCmd.MovieID)
	assert.Equal(t, "https://example.com/ABC-001", capturedCmd.RawInput)
}

func TestBatchJob_Rescrape_WithForceRefresh(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, &stubRescrapeWF{})

	var capturedCmd scrape.ScrapeCmd
	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		capturedCmd = cmd
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
		Force:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
	assert.True(t, capturedCmd.ForceRefresh)
}

func TestBatchJob_Rescrape_WithSelectedScrapers(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, &stubRescrapeWF{})

	var capturedCmd scrape.ScrapeCmd
	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		capturedCmd = cmd
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
	}

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID:          "ABC-001",
		SelectedScrapers: []string{"r18dev", "dmm"},
	})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
	assert.Equal(t, []string{"r18dev", "dmm"}, capturedCmd.SelectedScrapers)
}

// -- WF override tests for reconstructed jobs (half-alive rescrape fix) --

// stubRescrapeWF is a minimal WorkflowInterface stub for Rescrape WF override tests.
type stubRescrapeWF struct{}

func (s *stubRescrapeWF) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}
func (s *stubRescrapeWF) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (s *stubRescrapeWF) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (s *stubRescrapeWF) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (s *stubRescrapeWF) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

// TestBatchJob_Rescrape_ReconstructedWithWF succeeds when SetWorkflow is called
// on a reconstructed job (job.deps.WF == nil) before Rescrape. Per DEEP-6: WF is
// set on job.deps via SetWorkflow, not as a RescrapeCmd override.
func TestBatchJob_Rescrape_ReconstructedWithWF(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, nil)

	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return &scrape.ScrapeResult{
			Movie:          &models.Movie{ID: "ABC-001"},
			Status:         scrape.StatusCompleted,
			FieldSources:   map[string]string{"title": "r18dev"},
			ActressSources: map[string]string{"actresses": "r18dev"},
			StartedAt:      time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
	}

	// Per DEEP-6: set WF on job.deps via SetWorkflow instead of RescrapeCmd.WF override
	job.controller.SetWorkflow(&stubRescrapeWF{})
	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
}

// TestBatchJob_Rescrape_ReconstructedWithoutWF returns a domain-level error
// when a reconstructed job (job.deps.WF == nil) calls Rescrape without providing
// RescrapeCmd.WF. No panic, just a graceful "workflow not configured" result.
func TestBatchJob_Rescrape_ReconstructedWithoutWF(t *testing.T) {
	job, _ := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, nil)

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err) // Domain error, not Go error
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "workflow not configured")
}

// TestBatchJob_Rescrape_SetWorkflowUpdatesDeps verifies that SetWorkflow
// updates job.deps.WF and the new WF is used by Rescrape. Per DEEP-6: replaces
// the old TestBatchJob_Rescrape_WFOverrideTakesPrecedence which tested RescrapeCmd.WF
// override (now removed). SetWorkflow is the new mechanism for WF injection.
func TestBatchJob_Rescrape_SetWorkflowUpdatesDeps(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, &stubRescrapeWF{})

	var capturedInputs rescrapePhaseInputs
	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		capturedInputs = inputs
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	mockPhase.completeRescrapeFn = func(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
	}

	// Per DEEP-6: set a new WF on job.deps via SetWorkflow
	newWF := &stubRescrapeWF{}
	job.controller.SetWorkflow(newWF)

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{
		MovieID: "ABC-001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
	// Verify the SetWorkflow WF was used
	assert.Same(t, newWF, capturedInputs.WF, "SetWorkflow WF should be passed to ScrapeSingle")
}

// TestBatchJob_ScrapeSingle_BackwardCompatibility verifies that ScrapeSingle
// (the ControlledJob interface method) still works with job.deps.WF on freshly-created
// jobs — backward compatibility after the scrapeSingleWithWF refactor.
func TestBatchJob_ScrapeSingle_BackwardCompatibility(t *testing.T) {
	job, mockPhase := newJobWithRescrapeMock(map[string]*MovieResult{
		"/path/to/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	}, nil, nil)

	// Simulate a freshly-created job with job.deps.WF set
	jobWF := &stubRescrapeWF{}
	job.mu.Lock()
	job.deps.WF = jobWF
	job.mu.Unlock()

	var capturedInputs rescrapePhaseInputs
	mockPhase.scrapeSingleFn = func(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		capturedInputs = inputs
		return &scrape.ScrapeResult{
			Movie:     &models.Movie{ID: "ABC-001"},
			Status:    scrape.StatusCompleted,
			StartedAt: time.Now(),
		}, nil, nil
	}

	// ScrapeSingle reads job.deps.WF directly — no override
	result, _, err := job.rescrapePhase.ScrapeSingle(context.Background(), rescrapePhaseInputs{JobID: job.ID, WF: job.deps.WF, ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/path/to/file1.mp4", scrape.ScrapeCmd{MovieID: "ABC-001"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-001", result.Movie.ID)
	// Verify that job.deps.WF was passed through (not nil)
	assert.Same(t, jobWF, capturedInputs.WF, "job.deps.WF should be passed to ScrapeSingle via scrapeSingleWithWF")
}
