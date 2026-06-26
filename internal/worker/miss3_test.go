package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// BatchJob accessors: Results(), ResultsWriter(), Lifecycle(), GetID()
// (batch_job.go lines 338-363 — 0% coverage)
// ---------------------------------------------------------------------------

func TestBatchJob_Results(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	accessor := job.Results()
	require.NotNil(t, accessor)
	assert.Same(t, job.results, accessor)
}

func TestBatchJob_ResultsWriter(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	writer := job.ResultsWriter()
	require.NotNil(t, writer)
	assert.Same(t, job.results, writer)
}

func TestBatchJob_Lifecycle(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	lc := job.Lifecycle()
	require.NotNil(t, lc)
	assert.Same(t, job.lifecycle, lc)
}

func TestBatchJob_GetID(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	id := job.GetID()
	assert.NotEmpty(t, id)
	assert.Equal(t, job.ID.String(), id)
}

// ---------------------------------------------------------------------------
// BatchJobFactory: NewBatchJobFactory, CreateJob, CreateStandaloneJob,
// NewScrapeConfig, NewApplyConfig, NewRescrapeCmd, buildJobConfig
// (batch_job_factory.go — 0% coverage)
// ---------------------------------------------------------------------------

func TestNewBatchJobFactory(t *testing.T) {
	wf := &miss3StubWF{}
	m := &miss3StubMatcher{}
	pg := &miss3StubPosterGen{}
	batchCfg := BatchJobConfig{MaxWorkers: 4}

	factory := NewBatchJobFactory(nil, wf, m, pg, batchCfg, nil)
	require.NotNil(t, factory)
}

func TestBatchJobFactory_CreateJob(t *testing.T) {
	wf := &miss3StubWF{}
	m := &miss3StubMatcher{}
	pg := &miss3StubPosterGen{}
	batchCfg := BatchJobConfig{MaxWorkers: 4}

	store := NewInMemoryJobStore()
	factory := NewBatchJobFactory(store, wf, m, pg, batchCfg, nil)

	job := factory.CreateJob([]string{"file1.mp4", "file2.mp4"}, BatchJobOptions{})
	require.NotNil(t, job)
	assert.NotEmpty(t, job.GetID())
	assert.Equal(t, models.JobStatusPending, job.GetJobStatus())
}

func TestBatchJobFactory_CreateJob_WithOpts(t *testing.T) {
	wf := &miss3StubWF{}
	m := &miss3StubMatcher{}
	pg := &miss3StubPosterGen{}
	batchCfg := BatchJobConfig{MaxWorkers: 4}

	store := NewInMemoryJobStore()
	factory := NewBatchJobFactory(store, wf, m, pg, batchCfg, nil)

	job := factory.CreateJob([]string{"file1.mp4"}, BatchJobOptions{
		ID:                    "custom-id",
		Destination:           "/output",
		OperationModeOverride: "organize",
	})
	require.NotNil(t, job)
	assert.Equal(t, "custom-id", job.GetID())
}

func TestBatchJobFactory_CreateJob_WFOverride(t *testing.T) {
	defaultWF := &miss3StubWF{}
	overrideWF := &miss3StubWF{}
	m := &miss3StubMatcher{}
	pg := &miss3StubPosterGen{}
	batchCfg := BatchJobConfig{MaxWorkers: 4}

	store := NewInMemoryJobStore()
	factory := NewBatchJobFactory(store, defaultWF, m, pg, batchCfg, nil)

	job := factory.CreateJob([]string{"file1.mp4"}, BatchJobOptions{
		WF: overrideWF,
	})
	require.NotNil(t, job)
}

func TestBatchJobFactory_CreateStandaloneJob(t *testing.T) {
	wf := &miss3StubWF{}
	m := &miss3StubMatcher{}
	pg := &miss3StubPosterGen{}
	batchCfg := BatchJobConfig{MaxWorkers: 4}

	store := NewInMemoryJobStore()
	factory := NewBatchJobFactory(store, wf, m, pg, batchCfg, nil)

	sj := factory.CreateStandaloneJob([]string{"file1.mp4"}, BatchJobOptions{})
	require.NotNil(t, sj)
	assert.NotEmpty(t, sj.GetID())
}

func TestBatchJobFactory_NewScrapeConfig(t *testing.T) {
	factory := NewBatchJobFactory(nil, nil, nil, nil, BatchJobConfig{}, nil)

	cfg := factory.NewScrapeConfig([]string{"r18dev", "dmm"}, true, false)
	assert.Equal(t, []string{"r18dev", "dmm"}, cfg.SelectedScrapers)
	assert.True(t, cfg.Strict)
	assert.False(t, cfg.Force)
}

func TestBatchJobFactory_NewApplyConfig(t *testing.T) {
	factory := NewBatchJobFactory(nil, nil, nil, nil, BatchJobConfig{}, nil)

	cfg := factory.NewApplyConfig(
		workflow.OrganizeOptions{MoveFiles: true},
		workflow.MergeOptions{},
		"/output",
	)
	assert.Equal(t, "/output", cfg.Destination)
	assert.True(t, cfg.OrganizeOptions.MoveFiles)
}

func TestBatchJobFactory_NewRescrapeCmd(t *testing.T) {
	factory := NewBatchJobFactory(nil, nil, nil, nil, BatchJobConfig{}, nil)

	mergeOpts := workflow.MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true}
	cmd := factory.NewRescrapeCmd("ABC-001", "/path/file.mp4", "search input", []string{"r18dev"}, true, mergeOpts)
	assert.Equal(t, "ABC-001", cmd.MovieID)
	assert.Equal(t, "/path/file.mp4", cmd.FilePath)
	assert.Equal(t, "search input", cmd.ManualSearchInput)
	assert.Equal(t, []string{"r18dev"}, cmd.SelectedScrapers)
	assert.True(t, cmd.Force)
	assert.Equal(t, mergeOpts.ScalarStrategy, cmd.Merge.ScalarStrategy)
	assert.True(t, cmd.Merge.ArrayStrategy)
	// MergeEnabled is the caller's responsibility (set after building); the
	// factory does not infer it from a zero/non-zero MergeOptions.
	assert.False(t, cmd.MergeEnabled)
}

func TestBatchJobFactory_BuildJobConfig_DefaultWF(t *testing.T) {
	wf := &miss3StubWF{}
	m := &miss3StubMatcher{}
	pg := &miss3StubPosterGen{}
	batchCfg := BatchJobConfig{MaxWorkers: 2, WorkerTimeout: 30 * time.Second}

	store := NewInMemoryJobStore()
	factory := NewBatchJobFactory(store, wf, m, pg, batchCfg, nil)

	job := factory.CreateJob([]string{"file1.mp4"}, BatchJobOptions{})
	require.NotNil(t, job)
}

// ---------------------------------------------------------------------------
// StandaloneJob adapter methods: GetMovieResult, Subscribe, UpdateMovie,
// ExcludeFile, UpdatePosterCrop, UpdatePosterFromURL, Rescrape,
// SetWorkflow, SetBatchCfg, SetJobStatus, SetOperationModeOverride,
// SetPersistError
// (batch_job_interface.go — 0% coverage adapter methods)
// ---------------------------------------------------------------------------

func TestStandaloneJob_GetMovieResult(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "GMR-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "GMR-001"},
	})

	sj := newStandaloneJobFromBatchJob(job)
	result, err := sj.GetMovieResult("file1.mp4")
	require.NoError(t, err)
	assert.Equal(t, "GMR-001", result.Movie.ID)
}

func TestStandaloneJob_Subscribe(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	sj := newStandaloneJobFromBatchJob(job)
	sub := sj.Subscribe()
	defer sub.Close()
	require.NotNil(t, sub)
}

func TestStandaloneJob_UpdateMovie(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "OLD-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "OLD-001"},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	err := ej.UpdateMovie(context.Background(), "file1.mp4", &models.Movie{ID: "NEW-001", Title: "Updated"})
	require.NoError(t, err)

	result, _ := job.results.GetMovieResult("file1.mp4")
	assert.Equal(t, "NEW-001", result.Movie.ID)
}

func TestStandaloneJob_ExcludeFile(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "EXC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "EXC-001"},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	ej.ExcludeFile("file1.mp4")

	assert.True(t, job.results.Excluded["file1.mp4"])
}

func TestStandaloneJob_ExcludeFile_TriggersCancel(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "EXC-002"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "EXC-002"},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	ej.ExcludeFile("file1.mp4")

	// All files excluded — should trigger cancel
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
}

func TestStandaloneJob_UpdatePosterCrop(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "UPC-001",
			Poster: models.PosterState{
				PosterURL:        "https://example.com/poster.jpg",
				ShouldCropPoster: true,
			},
		},
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "UPC-001"},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	err := ej.UpdatePosterCrop("UPC-001", "https://example.com/cropped.jpg")
	require.NoError(t, err)

	result, _ := job.results.GetMovieResult("file1.mp4")
	assert.Equal(t, "https://example.com/cropped.jpg", result.Movie.Poster.CroppedPosterURL)
}

func TestStandaloneJob_UpdatePosterFromURL(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "UPU-001",
			Poster: models.PosterState{
				PosterURL:        "https://example.com/old.jpg",
				ShouldCropPoster: true,
			},
		},
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "UPU-001"},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	err := ej.UpdatePosterFromURL(context.TODO(), "UPU-001", "https://example.com/new.jpg", "https://example.com/new-cropped.jpg")
	require.NoError(t, err)

	result, _ := job.results.GetMovieResult("file1.mp4")
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Poster.PosterURL)
}

func TestBatchJobInterface_Rescrape(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	bj, ok := jq.GetBatchJob(job.ID.String())
	require.True(t, ok)

	result, err := bj.Rescrape(context.Background(), RescrapeCmd{MovieID: "RSC-001"})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
}

func TestBatchJobInterface_SetWorkflow(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	bj, ok := jq.GetBatchJob(job.ID.String())
	require.True(t, ok)

	wf := &miss3StubWF{}
	bj.SetWorkflow(wf)
	assert.Equal(t, wf, job.deps.WF)
}

func TestBatchJobInterface_SetBatchCfg(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	bj, ok := jq.GetBatchJob(job.ID.String())
	require.True(t, ok)

	newCfg := BatchJobConfig{MaxWorkers: 10, WorkerTimeout: 5 * time.Second}
	bj.SetBatchCfg(newCfg)
	assert.Equal(t, 10, job.deps.BatchCfg.MaxWorkers)
	assert.Equal(t, 5*time.Second, job.deps.BatchCfg.WorkerTimeout)
}

func TestBatchJobInterface_SetJobStatus(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	bj, ok := jq.GetBatchJob(job.ID.String())
	require.True(t, ok)

	bj.SetJobStatus(models.JobStatusRunning)
	assert.Equal(t, models.JobStatusRunning, job.lifecycle.GetJobStatus())
}

func TestBatchJobInterface_SetOperationModeOverride(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	bj, ok := jq.GetBatchJob(job.ID.String())
	require.True(t, ok)

	err := bj.SetOperationModeOverride("organize")
	require.NoError(t, err)
	assert.Equal(t, "organize", string(job.GetOperationModeOverride()))
}

func TestBatchJobInterface_SetPersistError(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	bj, ok := jq.GetBatchJob(job.ID.String())
	require.True(t, ok)

	bj.SetPersistError("test error")
	assert.Equal(t, "test error", job.GetPersistError())
}

// ---------------------------------------------------------------------------
// JobLifecycle: Done() and StatusSnapshot()
// (job_lifecycle.go lines 94, 198 — 0% coverage)
// ---------------------------------------------------------------------------

func TestJobLifecycle_Done_ChannelClosedOnCompleted(t *testing.T) {
	lc := &JobLifecycle{
		Status: models.JobStatusPending,
		done:   make(chan struct{}),
	}

	select {
	case <-lc.Done():
		t.Fatal("Done channel should not be closed before completion")
	default:
	}

	lc.MarkCompleted()

	select {
	case <-lc.Done():
		// Expected
	default:
		t.Fatal("Done channel should be closed after MarkCompleted")
	}
}

func TestJobLifecycle_Done_ChannelClosedOnFailed(t *testing.T) {
	lc := &JobLifecycle{
		Status: models.JobStatusPending,
		done:   make(chan struct{}),
	}

	lc.MarkFailed()
	select {
	case <-lc.Done():
		// Expected
	default:
		t.Fatal("Done channel should be closed after MarkFailed")
	}
}

func TestJobLifecycle_Done_ChannelClosedOnCancelled(t *testing.T) {
	lc := &JobLifecycle{
		Status: models.JobStatusPending,
		done:   make(chan struct{}),
	}

	lc.MarkCancelled()
	select {
	case <-lc.Done():
		// Expected
	default:
		t.Fatal("Done channel should be closed after MarkCancelled")
	}
}

func TestJobLifecycle_StatusSnapshot(t *testing.T) {
	lc := &JobLifecycle{
		Status: models.JobStatusCompleted,
		done:   make(chan struct{}),
	}
	lc.CompletedAt = nowTimePtr()

	snap := lc.StatusSnapshot()
	assert.Equal(t, models.JobStatusCompleted, snap.Status)
	assert.NotNil(t, snap.CompletedAt)
	assert.False(t, snap.IsDeleted)
}

func TestJobLifecycle_StatusSnapshot_WithDeleted(t *testing.T) {
	lc := &JobLifecycle{
		Status:  models.JobStatusPending,
		done:    make(chan struct{}),
		deleted: true,
	}

	snap := lc.StatusSnapshot()
	assert.True(t, snap.IsDeleted)
}

func TestJobLifecycle_StatusSnapshot_ClonesTimestamps(t *testing.T) {
	lc := &JobLifecycle{
		Status:      models.JobStatusOrganized,
		done:        make(chan struct{}),
		OrganizedAt: nowTimePtr(),
	}

	snap := lc.StatusSnapshot()
	require.NotNil(t, snap.OrganizedAt)

	originalTime := *lc.OrganizedAt
	newTime := originalTime.Add(time.Hour)
	snap.OrganizedAt = &newTime

	assert.Equal(t, originalTime, *lc.OrganizedAt, "snapshot should not share pointer with lifecycle")
}

// ---------------------------------------------------------------------------
// NoopJobPersistence: all methods
// (job_persistencer.go lines 51-76 — 0% coverage)
// ---------------------------------------------------------------------------

func TestNewNoopJobPersistence(t *testing.T) {
	p := NewNoopJobPersistence()
	require.NotNil(t, p)
}

func TestNoopJobPersistence_PersistJob(t *testing.T) {
	p := NewNoopJobPersistence()
	job := newBatchJob([]string{"file1.mp4"})
	p.PersistJob(job)
}

func TestNoopJobPersistence_PersistJobByID(t *testing.T) {
	p := NewNoopJobPersistence()
	p.PersistJobByID("test-id")
}

func TestNoopJobPersistence_DeleteJobFromDB(t *testing.T) {
	p := NewNoopJobPersistence()
	err := p.DeleteJobFromDB("job-1")
	assert.NoError(t, err)
}

func TestNoopJobPersistence_LoadJobs(t *testing.T) {
	p := NewNoopJobPersistence()
	jobs, err := p.LoadJobs(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, jobs)
}

func TestNoopJobPersistence_UpsertJob(t *testing.T) {
	p := NewNoopJobPersistence()
	err := p.UpsertJob(&models.Job{ID: "test"})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// NewDBJobPersistence, PersistJobByID, UpsertJob
// (job_persistencer.go lines 81, 98, 149 — 0% coverage)
// ---------------------------------------------------------------------------

func TestNewDBJobPersistence(t *testing.T) {
	p := NewDBJobPersistence(nil)
	require.NotNil(t, p)
}

func TestDBJobPersistence_PersistJobByID(t *testing.T) {
	p := NewDBJobPersistence(nil)
	p.PersistJobByID("some-id") // no-op without store
}

func TestDBJobPersistence_UpsertJob_NilRepo(t *testing.T) {
	p := NewDBJobPersistence(nil)
	err := p.UpsertJob(&models.Job{ID: "test"})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// WithPersistence, NewInMemoryJobStore
// (job_store.go lines 46, 62 — 0% coverage)
// ---------------------------------------------------------------------------

func TestWithPersistence(t *testing.T) {
	custom := NewNoopJobPersistence()
	opt := WithPersistence(custom)

	s := &JobStore{}
	opt(s)

	assert.Equal(t, custom, s.persistence)
}

func TestNewInMemoryJobStore(t *testing.T) {
	store := NewInMemoryJobStore()
	require.NotNil(t, store)
	assert.Empty(t, store.ListJobs())
}

func TestNewInMemoryJobStore_WithPersistence(t *testing.T) {
	custom := NewNoopJobPersistence()
	store := NewInMemoryJobStore(WithPersistence(custom))
	require.NotNil(t, store)
	assert.Equal(t, custom, store.persistence)
}

func TestNewInMemoryJobStore_CreateJob(t *testing.T) {
	store := NewInMemoryJobStore()
	job := store.CreateJobBatch([]string{"file1.mp4"})
	require.NotNil(t, job)
	assert.NotEmpty(t, job.ID.String())
}

// ---------------------------------------------------------------------------
// jobController.SetBatchCfg
// (job_controller.go line 375 — 0% coverage)
// ---------------------------------------------------------------------------

func TestJobController_SetBatchCfg(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	newCfg := BatchJobConfig{MaxWorkers: 8, WorkerTimeout: 15 * time.Second}
	job.controller.SetBatchCfg(newCfg)

	assert.Equal(t, 8, job.deps.BatchCfg.MaxWorkers)
	assert.Equal(t, 15*time.Second, job.deps.BatchCfg.WorkerTimeout)
}

// ---------------------------------------------------------------------------
// ResultTracker: Updater(), ReadStore()
// (result_tracker.go lines 56, 62 — 0% coverage)
// ---------------------------------------------------------------------------

func TestResultTracker_Updater(t *testing.T) {
	rt := NewResultTracker(2, []string{"f1.mp4", "f2.mp4"})
	updater := rt.Updater()
	require.NotNil(t, updater)
	assert.Same(t, rt.resultUpdater, updater)
}

func TestResultTracker_ReadStore(t *testing.T) {
	rt := NewResultTracker(2, []string{"f1.mp4", "f2.mp4"})
	rs := rt.ReadStore()
	require.NotNil(t, rs)
	assert.Same(t, rt.resultReadStore, rs)
}

// ---------------------------------------------------------------------------
// resultReadStore: CloneResults(), SnapshotForStatus()
// (result_read_store.go lines 135, 302 — 0% coverage)
// ---------------------------------------------------------------------------

func TestResultReadStore_CloneResults(t *testing.T) {
	rt := NewResultTracker(1, []string{"f1.mp4"})
	rt.UpdateFileResult("f1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "f1.mp4", MovieID: "CR-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CR-001"},
	})

	cloned := rt.CloneResults()
	assert.Len(t, cloned, 1)
	assert.NotSame(t, rt.Results["f1.mp4"], cloned["f1.mp4"])

	cloned["f1.mp4"].Movie.ID = "MODIFIED"
	assert.Equal(t, "CR-001", rt.Results["f1.mp4"].Movie.ID, "mutation of clone should not affect original")
}

func TestResultReadStore_CloneResults_NilEntries(t *testing.T) {
	rt := newResultTrackerFromState(&resultTrackerState{
		Results: map[string]*MovieResult{
			"f1": {Status: models.JobStatusCompleted},
			"f2": nil,
		},
	})
	cloned := rt.CloneResults()
	assert.Len(t, cloned, 1, "nil entries should be skipped")
	_, hasF1 := cloned["f1"]
	assert.True(t, hasF1)
}

func TestResultReadStore_SnapshotForStatus(t *testing.T) {
	rt := NewResultTracker(2, []string{"f1.mp4", "f2.mp4"})
	rt.UpdateFileResult("f1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "f1.mp4", MovieID: "SFS-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SFS-001"},
	})

	resultSnap, progressSnap := rt.SnapshotForStatus()
	assert.Len(t, resultSnap.Results, 1)
	assert.Equal(t, 2, progressSnap.TotalFiles)
	assert.Equal(t, 1, progressSnap.Completed)
	assert.Greater(t, progressSnap.Progress, 0.0)
}

// ---------------------------------------------------------------------------
// resultUpdater: SetFileMatchInfo
// (result_updater.go line 147 — 0% coverage)
// ---------------------------------------------------------------------------

func TestResultUpdater_SetFileMatchInfo(t *testing.T) {
	rt := NewResultTracker(1, []string{"f1.mp4"})
	info := models.FileMatchInfo{MovieID: "SFMI-001", IsMultiPart: true, PartNumber: 2}
	rt.SetFileMatchInfo("f1.mp4", info)

	retrieved, ok := rt.GetFileMatchInfo("f1.mp4")
	assert.True(t, ok)
	assert.Equal(t, "SFMI-001", retrieved.MovieID)
	assert.True(t, retrieved.IsMultiPart)
	assert.Equal(t, 2, retrieved.PartNumber)
}

// ---------------------------------------------------------------------------
// scrape_phase: trackScrapeResults (0% coverage — no-op seam)
// ---------------------------------------------------------------------------

func TestTrackScrapeResults(t *testing.T) {
	assert.NotPanics(t, func() {
		trackScrapeResults(nil)
	})
}

// ---------------------------------------------------------------------------
// rescrape_phase: withRescrapeStatus, replaceRescrapeResult, Rescrape
// (rescrape_phase.go lines 121, 152, 165 — 0% coverage)
// ---------------------------------------------------------------------------

func TestRescrapePhase_Rescrape_NoFilePath(t *testing.T) {
	phase := NewRescrapePhase()
	inputs := rescrapePhaseInputs{
		ResultMap: NewResultTracker(0, nil),
		Finder:    NewResultTracker(0, nil),
	}

	cmd := RescrapeCmd{MovieID: "NFP-001"}
	_, err := phase.Rescrape(context.Background(), inputs, cmd)
	assert.Error(t, err)
}

func TestRescrapePhase_Rescrape_WithManualSearchURL(t *testing.T) {
	phase := NewRescrapePhase()
	rt := NewResultTracker(1, []string{"f1.mp4"})
	inputs := rescrapePhaseInputs{
		ResultMap: rt,
		Finder:    rt,
		JobID:     models.NewJobID(),
	}

	cmd := RescrapeCmd{
		MovieID:           "MSU-001",
		ManualSearchInput: "https://example.com/video/MSU-001",
		FilePath:          "f1.mp4",
	}
	_, err := phase.Rescrape(context.Background(), inputs, cmd)
	// No WF configured — returns error from ScrapeSingle
	assert.Error(t, err)
}

func TestRescrapePhase_Rescrape_WithManualSearchNonURL(t *testing.T) {
	phase := NewRescrapePhase()
	rt := NewResultTracker(1, []string{"f1.mp4"})
	inputs := rescrapePhaseInputs{
		ResultMap: rt,
		Finder:    rt,
		JobID:     models.NewJobID(),
	}

	cmd := RescrapeCmd{
		MovieID:           "MSN-001",
		ManualSearchInput: "some search query",
		FilePath:          "f1.mp4",
	}
	_, err := phase.Rescrape(context.Background(), inputs, cmd)
	// No WF configured — returns error
	assert.Error(t, err)
}

func TestRescrapePhase_Rescrape_SelectedScrapers(t *testing.T) {
	phase := NewRescrapePhase()
	rt := NewResultTracker(1, []string{"f1.mp4"})
	inputs := rescrapePhaseInputs{
		ResultMap: rt,
		Finder:    rt,
		JobID:     models.NewJobID(),
	}

	cmd := RescrapeCmd{
		MovieID:          "SS-001",
		FilePath:         "f1.mp4",
		SelectedScrapers: []string{"r18dev", "dmm"},
	}
	_, err := phase.Rescrape(context.Background(), inputs, cmd)
	// No WF configured — returns error
	assert.Error(t, err)
}

func TestReplaceRescrapeResult_WithProvenance(t *testing.T) {
	outcome := &RescrapeResult{Status: models.RescrapeStatusSuccess}
	movieResult := &MovieResult{
		Movie: &models.Movie{ID: "PRR-001", Title: "Test"},
	}
	prov := &ProvenanceData{
		FieldSources:   map[string]string{"title": "r18dev"},
		ActressSources: map[string]string{"actress_0": "dmm"},
	}

	replaceRescrapeResult(outcome, "/path/file.mp4", movieResult, prov)

	assert.Equal(t, "/path/file.mp4", outcome.FilePath)
	assert.Equal(t, "PRR-001", outcome.Movie.ID)
	assert.Equal(t, "r18dev", outcome.FieldSources["title"])
	assert.Equal(t, "dmm", outcome.ActressSources["actress_0"])
}

func TestReplaceRescrapeResult_WithoutProvenance(t *testing.T) {
	outcome := &RescrapeResult{Status: models.RescrapeStatusSuccess}
	movieResult := &MovieResult{
		Movie: &models.Movie{ID: "PRR-002", Title: "Test No Prov"},
	}

	replaceRescrapeResult(outcome, "/path/file2.mp4", movieResult, nil)

	assert.Equal(t, "/path/file2.mp4", outcome.FilePath)
	assert.Equal(t, "PRR-002", outcome.Movie.ID)
	assert.Nil(t, outcome.FieldSources)
	assert.Nil(t, outcome.ActressSources)
}

// ---------------------------------------------------------------------------
// result_tracker_state: stateUpdateProgressFromCounters with TotalFiles=0
// stateLookupFilePathForResultIDLocked with nil resultIDIndex
// (result_tracker_state.go — low coverage)
// ---------------------------------------------------------------------------

func TestStateUpdateProgressFromCounters_ZeroTotal(t *testing.T) {
	s := &resultTrackerState{TotalFiles: 0, Completed: 0, Failed: 0}
	stateUpdateProgressFromCounters(s)
	assert.Equal(t, 100.0, s.Progress)
}

func TestStateUpdateProgressFromCounters_NonZeroTotal(t *testing.T) {
	s := &resultTrackerState{TotalFiles: 10, Completed: 3, Failed: 2}
	stateUpdateProgressFromCounters(s)
	assert.InDelta(t, 50.0, s.Progress, 0.1)
}

func TestStateLookupFilePathForResultIDLocked_NilIndex(t *testing.T) {
	s := &resultTrackerState{resultIDIndex: nil}
	fp, ok := stateLookupFilePathForResultIDLocked(s, "some-id")
	assert.False(t, ok)
	assert.Empty(t, fp)
}

func TestStateLookupFilePathForResultIDLocked_Found(t *testing.T) {
	s := &resultTrackerState{
		resultIDIndex: map[string]string{"result-1": "file1.mp4"},
	}
	fp, ok := stateLookupFilePathForResultIDLocked(s, "result-1")
	assert.True(t, ok)
	assert.Equal(t, "file1.mp4", fp)
}

// ---------------------------------------------------------------------------
// resultReadStore: IsGone without goneChecker
// (result_read_store.go:45 — 66.7% coverage)
// ---------------------------------------------------------------------------

func TestResultReadStore_IsGone_NoChecker(t *testing.T) {
	rt := NewResultTracker(0, nil)
	assert.False(t, rt.IsGone())
}

func TestResultReadStore_IsGone_WithChecker(t *testing.T) {
	rt := NewResultTracker(0, nil)
	rt.goneChecker = func() bool { return true }
	assert.True(t, rt.IsGone())
}

// ---------------------------------------------------------------------------
// resultReadStore: GetFileResultByResultID
// (result_read_store.go:284 — 62.5% coverage)
// ---------------------------------------------------------------------------

func TestResultReadStore_GetFileResultByResultID_Found(t *testing.T) {
	rt := NewResultTracker(1, []string{"f1.mp4"})
	rt.UpdateFileResult("f1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "f1.mp4", MovieID: "GFR-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "GFR-001"},
	})

	var resultID string
	for _, r := range rt.Results {
		if r != nil && r.ResultID != "" {
			resultID = r.ResultID
			break
		}
	}
	if resultID == "" {
		t.Skip("ResultID not set by UpdateFileResult")
	}

	mr, filePath, ok := rt.GetFileResultByResultID(resultID)
	require.True(t, ok)
	assert.Equal(t, "f1.mp4", filePath)
	assert.Equal(t, "f1.mp4", mr.FileMatchInfo.Path)
}

func TestResultReadStore_GetFileResultByResultID_NotFound(t *testing.T) {
	rt := NewResultTracker(0, nil)
	_, _, ok := rt.GetFileResultByResultID("nonexistent-id")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// TempDirCleaner: CleanJobTempDir, StartStaleTempCleanup
// (temp_dir_cleaner.go — low coverage)
// ---------------------------------------------------------------------------

func TestTempDirCleaner_CleanJobTempDir_InvalidJobID(t *testing.T) {
	cleaner := NewTempDirCleaner(nil, "/tmp", nil)
	cleaner.CleanJobTempDir("../../../etc/passwd")
}

func TestTempDirCleaner_CleanJobTempDir_NilFS(t *testing.T) {
	cleaner := NewTempDirCleaner(nil, "/tmp", nil)
	cleaner.CleanJobTempDir("valid-job-id")
}

func TestTempDirCleaner_CleanJobTempDir_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	fs.MkdirAll("/tmp/posters/test-job-id/subdir", 0755)

	cleaner := NewTempDirCleaner(fs, "/tmp", nil)
	cleaner.CleanJobTempDir("test-job-id")

	exists, _ := afero.Exists(fs, "/tmp/posters/test-job-id")
	assert.False(t, exists, "poster directory should be removed")
}

func TestTempDirCleaner_StartStaleTempCleanup(t *testing.T) {
	fs := afero.NewMemMapFs()
	cleaner := NewTempDirCleaner(fs, "/tmp", nil)

	stop := cleaner.StartStaleTempCleanup()
	require.NotNil(t, stop)
	close(stop)
	time.Sleep(100 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// snapshotForPersist: coverage of field-missing branches
// (job_store_persist.go:204 — 63.6% coverage)
// ---------------------------------------------------------------------------

func TestSnapshotForPersist_MinimalJob(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{})

	dbJob, ok := snapshotForPersist(job)
	require.True(t, ok)
	require.NotNil(t, dbJob)
	assert.NotNil(t, dbJob.Files) // empty JSON array, not nil
	assert.NotEmpty(t, dbJob.Results)
}

// ---------------------------------------------------------------------------
// BatchJob: newBatchJob with partial JobConfig
// (batch_job.go:143 — 78.6% coverage)
// ---------------------------------------------------------------------------

func TestNewBatchJob_WithPartialConfig(t *testing.T) {
	update := true
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		Destination:           "/output",
		OperationModeOverride: "organize",
		Update:                &update,
	})
	assert.Equal(t, "/output", job.cfg.destination)
	assert.Equal(t, "organize", string(job.cfg.operationMode))
	assert.True(t, job.cfg.update)
}

func TestNewBatchJob_WithPreGeneratedID(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		ID: "pre-gen-id",
	})
	assert.Equal(t, models.JobID("pre-gen-id"), job.ID)
}

// ---------------------------------------------------------------------------
// jobRunner.Run: context cancellation during run
// (job_runner.go:75 — 84.8% coverage)
// ---------------------------------------------------------------------------

func TestJobRunner_Run_ContextCancelled(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	sj := newStandaloneJobFromBatchJob(job)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sj.Run(ctx)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// StandaloneJob phase controller access
// ---------------------------------------------------------------------------

func TestStandaloneJob_PhaseController_SetWorkflow(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	controlled, ok := jq.GetJobForControl(job.ID.String())
	require.True(t, ok)

	wf := &miss3StubWF{}
	controlled.SetWorkflow(wf)
	assert.Equal(t, wf, job.deps.WF)
}

// ---------------------------------------------------------------------------
// Stubs for miss3 tests
// ---------------------------------------------------------------------------

type miss3StubWF struct {
	scrapeResult *scrape.ScrapeResult
	scrapeErr    error
}

func (s *miss3StubWF) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return s.scrapeResult, nil, s.scrapeErr
}
func (s *miss3StubWF) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (s *miss3StubWF) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (s *miss3StubWF) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (s *miss3StubWF) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

type miss3StubMatcher struct{}

func (s *miss3StubMatcher) MatchString(_ string) string                           { return "" }
func (s *miss3StubMatcher) Match(_ []models.FileMatchInfo) []matcher.MatchResult  { return nil }
func (s *miss3StubMatcher) MatchFile(_ models.FileMatchInfo) *matcher.MatchResult { return nil }

type miss3StubPosterGen struct{}

func (s *miss3StubPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	return nil
}
