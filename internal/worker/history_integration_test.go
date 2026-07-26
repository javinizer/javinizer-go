package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryIntegration_ScrapeSuccessWritesRow(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo
	movieRepo := repos.MovieRepo

	movie := &models.Movie{ID: "ABC-001", Title: "Test Movie"}
	_, err := movieRepo.Upsert(context.Background(), movie)
	require.NoError(t, err)

	o := scrapeFileOutcome{
		FilePath: "/input/ABC-001.mp4",
		MovieID:  "ABC-001",
		Success:  true,
		Result: &scrape.ScrapeResult{
			Movie:  movie,
			Status: scrape.StatusCompleted,
		},
	}
	inputs := scrapePhaseInputs{
		JobID:       "job-1",
		MovieRepo:   movieRepo,
		HistoryRepo: repo,
		Updater:     resultstore.New(1, []string{"/input/ABC-001.mp4"}),
	}
	persistScrapeOutcome(context.Background(), o, inputs, nil)

	records, err := repo.FindByBatchJobID(context.Background(), "job-1")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, models.HistoryOpScrape, records[0].Operation)
	assert.Equal(t, models.HistoryStatusSuccess, records[0].Status)
	assert.Equal(t, "ABC-001", records[0].MovieID)
	assert.Equal(t, "/input/ABC-001.mp4", records[0].OriginalPath)
}

func TestHistoryIntegration_ScrapeFailureWritesFailedRow(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo

	outcomes := []scrapeFileOutcome{
		{FilePath: "/input/FAIL-001.mp4", MovieID: "FAIL-001", Failed: true, ErrorMsg: "no results"},
		{FilePath: "/input/BOOM-001.mp4", MovieID: "BOOM-001", Panic: true, PanicMsg: "goroutine panic"},
	}
	trackScrapeResults(scrapePhaseInputs{JobID: "job-2", HistoryRepo: repo}, outcomes, nil)

	records, err := repo.FindByBatchJobID(context.Background(), "job-2")
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, r := range records {
		assert.Equal(t, models.HistoryOpScrape, r.Operation)
		assert.Equal(t, models.HistoryStatusFailed, r.Status)
	}
}

func TestHistoryIntegration_ScrapeCancelledNotWritten(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo

	outcomes := []scrapeFileOutcome{
		{FilePath: "/input/CAN-001.mp4", MovieID: "CAN-001", Failed: true, Cancelled: true, ErrorMsg: "canceled"},
		{FilePath: "/input/ERR-001.mp4", MovieID: "ERR-001", Failed: true, ErrorMsg: "real error"},
	}
	trackScrapeResults(scrapePhaseInputs{JobID: "job-3", HistoryRepo: repo}, outcomes, nil)

	records, err := repo.FindByBatchJobID(context.Background(), "job-3")
	require.NoError(t, err)
	require.Len(t, records, 1, "cancelled outcome must not produce a row")
	assert.Equal(t, "ERR-001", records[0].MovieID)
}

func TestHistoryIntegration_OrganizePanicWritesFromTrackApply(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo

	outcomes := []applyFileOutcome{
		{FilePath: "/input/ABC-001.mp4", MovieID: "ABC-001", Panic: true, Failed: true, PanicMsg: "boom"},
		{FilePath: "/input/ABC-002.mp4", MovieID: "ABC-002", Failed: true, ErrorMsg: "apply error"},
		{FilePath: "/input/ABC-003.mp4", MovieID: "ABC-003", Success: true},
	}
	var org, fail int64
	trackApplyResults(applyPhaseInputs{JobID: "job-4", HistoryRepo: repo}, outcomes, &org, &fail)

	records, err := repo.FindByBatchJobID(context.Background(), "job-4")
	require.NoError(t, err)
	require.Len(t, records, 1, "only panic outcome should be written, not failed or success")
	assert.Equal(t, "ABC-001", records[0].MovieID)
	assert.Equal(t, models.HistoryOpOrganize, records[0].Operation)
	assert.Equal(t, models.HistoryStatusFailed, records[0].Status)
}

func TestHistoryReconstruction_JobStoreRehydratesHistoryRepo(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()

	jq := NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, "", nil, nil,
		WithHistoryRepo(repos.HistoryRepo),
	)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	require.NotNil(t, job.deps.HistoryRepo)

	job.deps.HistoryRepo = nil
	jq.SetReconstructionDeps(nil, nil, BatchJobConfig{})
	assert.NotNil(t, job.deps.HistoryRepo, "SetReconstructionDeps must rehydrate HistoryRepo")
}

func TestHistoryConcurrency_MultiFileScrapeRace(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo

	const fileCount = 10
	outcomes := make([]scrapeFileOutcome, fileCount)
	for i := 0; i < fileCount; i++ {
		outcomes[i] = scrapeFileOutcome{
			FilePath: fmt.Sprintf("/input/file-%d.mp4", i),
			MovieID:  fmt.Sprintf("MOV-%03d", i),
			Failed:   true,
			ErrorMsg: "test error",
		}
	}
	trackScrapeResults(scrapePhaseInputs{JobID: "race-job", HistoryRepo: repo}, outcomes, nil)

	records, err := repo.FindByBatchJobID(context.Background(), "race-job")
	require.NoError(t, err)
	assert.Equal(t, fileCount, len(records), "all failed outcomes should produce rows")
}

func TestHistoryTimeoutContext_WriteSucceedsWithAuditContext(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo

	auditCtx, auditCancel := historyAuditContext()
	defer auditCancel()

	recordHistory(auditCtx, repo, models.History{
		MovieID:      "TIMEOUT-001",
		BatchJobID:   strPtrHist("timeout-job"),
		Operation:    models.HistoryOpOrganize,
		OriginalPath: "/input/TIMEOUT-001.mp4",
		Status:       models.HistoryStatusFailed,
		ErrorMessage: "apply timed out",
	})

	records, err := repo.FindByMovieID(context.Background(), "TIMEOUT-001")
	require.NoError(t, err)
	require.Len(t, records, 1, "audit context must succeed even when the worker ctx would be expired")
	assert.Equal(t, models.HistoryStatusFailed, records[0].Status)
}

func TestHistoryMultipart_TwoPartMovieWritesTwoRowsPerOperation(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo
	movieRepo := repos.MovieRepo

	movie := &models.Movie{ID: "MULTI-001", Title: "Multipart Movie"}
	_, err := movieRepo.Upsert(context.Background(), movie)
	require.NoError(t, err)

	inputs := scrapePhaseInputs{
		JobID:       "multi-job",
		MovieRepo:   movieRepo,
		HistoryRepo: repo,
		Updater:     resultstore.New(2, []string{"/input/MULTI-001-pt1.mp4", "/input/MULTI-001-pt2.mp4"}),
	}
	for _, part := range []string{"pt1", "pt2"} {
		o := scrapeFileOutcome{
			FilePath: fmt.Sprintf("/input/MULTI-001-%s.mp4", part),
			MovieID:  "MULTI-001",
			Success:  true,
			Result: &scrape.ScrapeResult{
				Movie:  movie.Clone(),
				Status: scrape.StatusCompleted,
			},
		}
		persistScrapeOutcome(context.Background(), o, inputs, nil)
	}

	records, err := repo.FindByBatchJobID(context.Background(), "multi-job")
	require.NoError(t, err)
	require.Len(t, records, 2, "two-part movie should produce two scrape rows")
	for _, r := range records {
		assert.Equal(t, "MULTI-001", r.MovieID, "both rows share the same MovieID")
		assert.NotEqual(t, "", r.OriginalPath, "each row has a distinct OriginalPath")
	}
	paths := []string{records[0].OriginalPath, records[1].OriginalPath}
	assert.NotEqual(t, paths[0], paths[1], "paths must be distinct")
}

func TestHistoryOrganizeMetadata_IncludesOperationMode(t *testing.T) {
	result := &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/movie.mp4"},
	}
	meta := organizeMetadata("in-place", result)
	assert.Contains(t, meta, "in-place")
	assert.Contains(t, meta, "operation_mode")
}

func TestHistoryCardinality_PersistedSuccessExactlyOneRow(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo
	movieRepo := repos.MovieRepo

	movie := &models.Movie{ID: "CARD-001", Title: "Cardinality Test"}
	_, err := movieRepo.Upsert(context.Background(), movie)
	require.NoError(t, err)

	inputs := scrapePhaseInputs{
		JobID:       "card-job",
		MovieRepo:   movieRepo,
		HistoryRepo: repo,
		Updater:     resultstore.New(1, []string{"/input/CARD-001.mp4"}),
	}

	o := scrapeFileOutcome{
		FilePath: "/input/CARD-001.mp4",
		MovieID:  "CARD-001",
		Success:  true,
		Result: &scrape.ScrapeResult{
			Movie:  movie.Clone(),
			Status: scrape.StatusCompleted,
		},
	}

	recorded := persistScrapeOutcomePool(context.Background(), []scrapeFileOutcome{o}, inputs, nil)
	trackScrapeResults(inputs, []scrapeFileOutcome{o}, recorded)

	records, err := repo.FindByBatchJobID(context.Background(), "card-job")
	require.NoError(t, err)
	assert.Len(t, records, 1, "persist pool + track must produce exactly one success row")
	if len(records) == 1 {
		assert.Equal(t, models.HistoryStatusSuccess, records[0].Status)
	}
}

func TestHistoryCardinality_PrePersistCancelAuditsSuccess(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	repo := repos.HistoryRepo

	inputs := scrapePhaseInputs{
		JobID:       "card-cancel-job",
		HistoryRepo: repo,
	}

	o := scrapeFileOutcome{
		FilePath: "/input/CARD-002.mp4",
		MovieID:  "CARD-002",
		Success:  true,
		Result:   &scrape.ScrapeResult{Movie: &models.Movie{ID: "CARD-002"}, Status: scrape.StatusCompleted},
	}

	trackScrapeResults(inputs, []scrapeFileOutcome{o}, nil)

	records, err := repo.FindByBatchJobID(context.Background(), "card-cancel-job")
	require.NoError(t, err)
	assert.Len(t, records, 1, "pre-persist cancel must audit one success row via trackScrapeResults(nil)")
	if len(records) == 1 {
		assert.Equal(t, models.HistoryStatusSuccess, records[0].Status)
	}
}

func strPtrHist(s string) *string { return &s }

var _ = time.Second
