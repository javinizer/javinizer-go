package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- job_store.go uncovered ---

func TestJobStore_CreateJobBatch_ReturnsConcreteTypeUncovered(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	require.NotNil(t, job)
	assert.Equal(t, models.JobStatusPending, job.lifecycle.GetJobStatus())
}

func TestJobStore_DeleteJob_NotFoundUncovered(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	err := jq.DeleteJob("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestJobStore_PersistJobByID_ExistingJobUncovered(t *testing.T) {
	mockRepo := &mockJobRepoForUncoveredTests{}
	jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	mockRepo.reset()
	jq.PersistJobByID(job.ID.String())
	assert.Equal(t, 1, int(mockRepo.upsertCalled))
}

// --- batch_job.go uncovered ---

func TestBatchJob_Run_WithScrapeAndApplyConfigUncovered(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: wf,
			BatchCfg: BatchJobConfig{
				MaxWorkers:    1,
				WorkerTimeout: 5 * time.Second,
			},
		},
	})
	// Per DEEP-1: SetRunOptions moved to StandaloneJob/JobRunner.
	sj := newStandaloneJobFromBatchJob(job)
	sj.SetRunOptions(
		ScrapePhaseConfig{},
		ApplyPhaseConfig{Destination: "/out"},
	)
	runner := sj.(*standaloneJobAdapter).runner
	assert.NotNil(t, runner.scrapeCfg)
	assert.NotNil(t, runner.applyCfg)
}

func TestBatchJob_StartScrape_NoWorkflowUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	err := job.Controller().StartScrape(context.Background(), []string{"file1.mp4"}, ScrapePhaseConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not configured")
}

func TestBatchJob_StartApply_NoWorkflowUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not configured")
}

func TestBatchJob_MarkOrganized_FromCompletedUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()
	job.lifecycle.MarkOrganized()
	assert.Equal(t, models.JobStatusOrganized, job.lifecycle.GetJobStatus())
	require.NotNil(t, job.lifecycle.OrganizedAt)
}

func TestBatchJob_ExcludeFile_CancelsWhenAllExcludedUncovered(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	excludeFile(job, "file1.mp4")
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
}

func TestBatchJob_Cancel_AlreadyCompletedUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()
	job.lifecycle.Cancel()
	assert.Equal(t, models.JobStatusCompleted, job.lifecycle.GetJobStatus(), "cancel should not change completed status")
}

func TestBatchJob_IsGone_DeletedUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.lifecycle.deleted = true
	assert.True(t, job.resultIndex.IsGone())
}

func TestBatchJob_IsGone_RunningUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.lifecycle.Status = models.JobStatusRunning
	assert.True(t, job.resultIndex.IsGone())
}

func TestBatchJob_IsGone_PendingUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	assert.False(t, job.resultIndex.IsGone())
}

func TestBatchJob_CommitResult_NewFileUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	result := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
	}
	err := job.resultIndex.CommitResult("file1.mp4", result, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), job.results.Results["file1.mp4"].Revision)
}

func TestBatchJob_CommitResult_ConflictUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusCompleted,
	})
	result := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusCompleted,
	}
	err := job.resultIndex.CommitResult("file1.mp4", result, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestBatchJob_OtherResultUsesMovieID_Uncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4", "file2.mp4"})
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "SHARED-001"},
		Movie:         &models.Movie{ID: "SHARED-001"},
	})
	job.SetResultDirect("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "SHARED-001"},
		Movie:         &models.Movie{ID: "SHARED-001"},
	})
	assert.True(t, job.resultIndex.OtherResultUsesMovieID("file1.mp4", "SHARED-001"))
	assert.False(t, job.resultIndex.OtherResultUsesMovieID("file1.mp4", "NONEXISTENT"))
}

func TestBatchJob_FindFileForMovieID_Uncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Movie:         &models.Movie{ID: "ABC-001"},
	})
	result, err := job.resultIndex.FindFileForMovieID("ABC-001")
	require.NoError(t, err)
	assert.Equal(t, "file1.mp4", result.FilePath)
}

func TestBatchJob_FindFileForMovieID_NotFoundUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	_, err := job.resultIndex.FindFileForMovieID("NONEXISTENT")
	assert.Error(t, err)
}

// --- result_tracker.go uncovered ---

func TestResultTracker_MovieIDIndexUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-001"},
	})

	// Test lookup via movieID index
	paths := job.resultIndex.FindFilePathsForMovieID("ABC-001")
	assert.Contains(t, paths, "file1.mp4")

	// Test case-insensitive
	pathsLower := job.resultIndex.FindFilePathsForMovieID("abc-001")
	assert.Contains(t, pathsLower, "file1.mp4")
}

func TestResultTracker_GetFilesReturnsCopyUncovered(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4", "file2.mkv"})
	files := job.results.GetFiles()
	files[0] = "modified"
	original := job.results.GetFiles()
	assert.Equal(t, "file1.mp4", original[0])
}

func TestResultTracker_CloneProvenanceLockedUncovered(t *testing.T) {
	rt := newResultTrackerFromState(&resultTrackerState{
		Provenance: map[string]*ProvenanceData{
			"f1": {FieldSources: map[string]string{"title": "src1"}},
		},
	})
	cloned := rt.SnapshotData().Provenance
	assert.Equal(t, "src1", cloned["f1"].FieldSources["title"])
	cloned["f1"].FieldSources["title"] = "modified"
	assert.Equal(t, "src1", rt.Provenance["f1"].FieldSources["title"], "clone should be independent")
}

func TestResultTracker_CloneFileMatchInfoUncovered(t *testing.T) {
	rt := newResultTrackerFromState(&resultTrackerState{
		FileMatchInfo: map[string]models.FileMatchInfo{
			"f1": {MovieID: "ABC-001"},
		},
	})
	cloned := rt.CloneFileMatchInfo()
	assert.Equal(t, "ABC-001", cloned["f1"].MovieID)
}

// --- job_event.go uncovered ---

func TestJobEventBroadcaster_SubscribeAfterCloseUncovered(t *testing.T) {
	b := newJobEventBroadcaster()
	b.Close()
	sub := b.Subscribe()
	_, ok := <-sub.Events()
	assert.False(t, ok, "subscriber should have closed channel after broadcaster close")
}

func TestJobEventBroadcaster_SendAfterCloseUncovered(t *testing.T) {
	b := newJobEventBroadcaster()
	b.Close()
	assert.NotPanics(t, func() {
		b.Send(JobEvent{JobID: "test"})
	})
}

func TestChannelSubscriber_CloseIdempotentUncovered(t *testing.T) {
	b := newJobEventBroadcaster()
	sub := b.Subscribe()
	sub.Close()
	sub.Close() // second close should not panic
}

// --- scrape_phase.go uncovered ---

func TestNewScrapePhaseUncovered(t *testing.T) {
	p := NewScrapePhase()
	require.NotNil(t, p)
}

// --- apply_phase.go uncovered ---

func TestNewApplyPhaseUncovered(t *testing.T) {
	p := NewApplyPhase()
	require.NotNil(t, p)
}

// --- fs_case.go uncovered ---

func TestFSCaseCache_ResetUncovered(t *testing.T) {
	cache := NewFSCaseCache(nil)
	cache.cache["/test"] = true
	cache.Reset()
	assert.Empty(t, cache.cache)
}

func TestFSCaseCache_AcquireProbeLockUncovered(t *testing.T) {
	cache := NewFSCaseCache(nil)
	mu := cache.acquireProbeLock("/test")
	require.NotNil(t, mu)
	mu2 := cache.acquireProbeLock("/test")
	assert.Same(t, mu, mu2, "same path should return same mutex")
}

func TestFSCaseCache_IsCaseInsensitive_CachedUncovered(t *testing.T) {
	cache := NewFSCaseCache(nil)
	cache.cache["/cached_path"] = true
	result := cache.IsCaseInsensitive("/cached_path")
	assert.True(t, result)
}

// --- phase_interfaces.go uncovered ---

func TestPersistFunc_NilUncovered(t *testing.T) {
	var p persistFunc
	assert.NotPanics(t, func() {
		p.Persist()
	})
}

func TestPersistFunc_NonNilUncovered(t *testing.T) {
	called := false
	p := persistFunc(func() { called = true })
	p.Persist()
	assert.True(t, called)
}

func TestNewConcurrencyConfigUncovered(t *testing.T) {
	cc := newConcurrencyConfig(0, 0, 5, 10*time.Second)
	assert.Equal(t, 5, cc.MaxWorkers)
	assert.Equal(t, 10*time.Second, cc.WorkerTimeout)

	cc2 := newConcurrencyConfig(3, 30*time.Second, 5, 10*time.Second)
	assert.Equal(t, 3, cc2.MaxWorkers)
	assert.Equal(t, 30*time.Second, cc2.WorkerTimeout)
}

// --- result_parse.go uncovered ---

func TestParseJobResultsJSON_Empty(t *testing.T) {
	parsed, err := ParseJobResultsJSON(nil)
	require.NoError(t, err)
	assert.Empty(t, parsed.Results)
}

func TestParseJobResultsJSON_LegacyWithDataUncovered(t *testing.T) {
	legacyResults := map[string]any{
		"file1.mp4": map[string]any{
			"file_path":           "file1.mp4",
			"movie_id":            "ABC-001",
			"revision":            1,
			"status":              "completed",
			"translation_warning": "partial",
			"data_type":           "movie",
			"data":                map[string]any{"id": "ABC-001", "title": "Test"},
			"started_at":          "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseJobResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "file1.mp4")
	mr := parsed.Results["file1.mp4"]
	assert.Equal(t, "file1.mp4", mr.FileMatchInfo.Path)
	assert.Equal(t, "ABC-001", mr.FileMatchInfo.MovieID)
	assert.Equal(t, uint64(1), mr.Revision)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "ABC-001", mr.Movie.ID)
	require.NotNil(t, mr.TranslationWarning)
	assert.Equal(t, "partial", *mr.TranslationWarning)
}

// --- job_lifecycle.go uncovered ---

func TestJobLifecycle_SetCancelFuncUncovered(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusPending, done: make(chan struct{})}
	called := false
	lc.setCancelFunc(func() { called = true })
	lc.cancelAndMarkCancelled()
	assert.True(t, called)
}

func TestJobLifecycle_CancelAndMarkCancelled_AlreadyCompletedUncovered(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusCompleted, done: make(chan struct{})}
	called := false
	lc.setCancelFunc(func() { called = true })
	lc.cancelAndMarkCancelled()
	assert.True(t, called, "cancelFunc should still be called for terminal states")
	assert.Equal(t, models.JobStatusCompleted, lc.Status, "status should not change from completed to cancelled")
}

func TestJobLifecycle_MarkDeletedUncovered(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusPending, done: make(chan struct{})}
	lc.markDeleted()
	assert.True(t, lc.deleted)
}

func TestJobLifecycle_CloseDoneLocked_DoubleCloseSafeUncovered(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusPending, done: make(chan struct{})}
	lc.closeDoneLocked()
	assert.NotPanics(t, func() { lc.closeDoneLocked() })
}

// --- mock job repo for uncovered tests ---

type mockJobRepoForUncoveredTests struct {
	upsertCalled int32
}

func (m *mockJobRepoForUncoveredTests) Upsert(_ context.Context, _ *models.Job) error {
	m.upsertCalled++
	return nil
}
func (m *mockJobRepoForUncoveredTests) reset()                                        { m.upsertCalled = 0 }
func (m *mockJobRepoForUncoveredTests) Create(_ context.Context, _ *models.Job) error { return nil }
func (m *mockJobRepoForUncoveredTests) FindByID(_ context.Context, _ string) (*models.Job, error) {
	return nil, nil
}
func (m *mockJobRepoForUncoveredTests) List(_ context.Context) ([]models.Job, error) { return nil, nil }
func (m *mockJobRepoForUncoveredTests) Delete(_ context.Context, _ string) error     { return nil }
func (m *mockJobRepoForUncoveredTests) DeleteOrganizedOlderThan(_ context.Context, _ time.Time) error {
	return nil
}
func (m *mockJobRepoForUncoveredTests) Update(_ context.Context, _ *models.Job) error { return nil }

// Atomic access helper
func (m *mockJobRepoForUncoveredTests) getUpsertCalled() int32 { return m.upsertCalled }
