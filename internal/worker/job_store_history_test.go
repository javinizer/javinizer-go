package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHistoryTestDB(t *testing.T) *database.DB {
	t.Helper()
	cfg := &database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return db
}

func TestJobStore_HistoryRepo_WiredViaCreateJob(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	jq := NewJobStore(nil, nil, nil, "", nil, nil, WithHistoryRepo(repos.HistoryRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	require.NotNil(t, job.deps.HistoryRepo)
	assert.Equal(t, repos.HistoryRepo, job.deps.HistoryRepo)
}

func TestJobStore_HistoryRepo_NilWithoutOption(t *testing.T) {
	jq := NewInMemoryJobStore()
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	assert.Nil(t, job.deps.HistoryRepo)
}

func TestJobStore_HistoryRepo_RehydratedInSetReconstructionDeps(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	jq := NewJobStore(nil, nil, nil, "", nil, nil, WithHistoryRepo(repos.HistoryRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.deps.HistoryRepo = nil
	jq.SetReconstructionDeps(nil, nil, BatchJobConfig{})
	assert.NotNil(t, job.deps.HistoryRepo)
	assert.Equal(t, repos.HistoryRepo, job.deps.HistoryRepo)
}

func TestRecordHistory_NilRepo_NoOp(t *testing.T) {
	assert.NotPanics(t, func() {
		recordHistory(context.Background(), nil, models.History{
			MovieID:   "TEST-001",
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
		})
	})
}

func TestRecordHistory_CreateError_LoggedAndContinued(t *testing.T) {
	repo := &failingHistoryRepo{err: errors.New("db down")}
	assert.NotPanics(t, func() {
		recordHistory(context.Background(), repo, models.History{
			MovieID:   "TEST-001",
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
		})
	})
	assert.Equal(t, 1, repo.callCount)
}

func TestOrganizeMetadata_NilResult(t *testing.T) {
	assert.Contains(t, organizeMetadata("organize", nil), "operation_mode")
	assert.Contains(t, organizeMetadata("organize", nil), "organize")
}

func TestOrganizeMetadata_NonNilResult(t *testing.T) {
	result := &workflow.ApplyResult{FailedStep: "download"}
	meta := organizeMetadata("in-place", result)
	assert.Contains(t, meta, "in-place")
	assert.Contains(t, meta, "operation_mode")
}

func TestNilGuardOrganizeNewPath_NilResult(t *testing.T) {
	assert.Equal(t, "", nilGuardOrganizeNewPath(nil))
}

func TestNilGuardOrganizeNewPath_NilOrganizeResult(t *testing.T) {
	result := &workflow.ApplyResult{OrganizeResult: nil}
	assert.Equal(t, "", nilGuardOrganizeNewPath(result))
}

func TestNilGuardOrganizeNewPath_WithNewPath(t *testing.T) {
	result := &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/movie.mp4"}}
	assert.Equal(t, "/dest/movie.mp4", nilGuardOrganizeNewPath(result))
}

func TestJobIDPtr_EmptyID(t *testing.T) {
	ptr := jobIDPtr("")
	assert.Nil(t, ptr)
}

func TestJobIDPtr_NonEmptyID(t *testing.T) {
	ptr := jobIDPtr("job-123")
	require.NotNil(t, ptr)
	assert.Equal(t, "job-123", *ptr)
}

func TestTrackScrapeResults_CancelledNotWritten(t *testing.T) {
	repo := &countingHistoryRepo{}
	outcomes := []scrapeFileOutcome{
		{FilePath: "a.mp4", MovieID: "A-001", Failed: true, Cancelled: true, ErrorMsg: "canceled"},
		{FilePath: "b.mp4", MovieID: "B-001", Failed: true, ErrorMsg: "real error"},
		{FilePath: "c.mp4", MovieID: "C-001", Panic: true, PanicMsg: "boom"},
	}
	trackScrapeResults(scrapePhaseInputs{HistoryRepo: repo}, outcomes, nil)
	assert.Equal(t, 2, repo.createCount(), "cancelled outcome should NOT be written; failed+panic should")
}

func TestTrackApplyResults_PanicOnlyNotDoubleWritten(t *testing.T) {
	repo := &countingHistoryRepo{}
	outcomes := []applyFileOutcome{
		{FilePath: "a.mp4", MovieID: "A-001", Failed: true, ErrorMsg: "apply error", Success: false},
		{FilePath: "b.mp4", MovieID: "B-001", Panic: true, Failed: true, PanicMsg: "boom"},
		{FilePath: "c.mp4", MovieID: "C-001", Success: true},
	}
	var org, fail int64
	trackApplyResults(applyPhaseInputs{HistoryRepo: repo}, outcomes, &org, &fail)
	assert.Equal(t, int64(1), org)
	assert.Equal(t, int64(2), fail)
	assert.Equal(t, 1, repo.createCount(), "only panic outcome should be written from trackApplyResults")
}

type failingHistoryRepo struct {
	err       error
	callCount int
}

func (r *failingHistoryRepo) Create(_ context.Context, _ *models.History) error {
	r.callCount++
	return r.err
}
func (r *failingHistoryRepo) FindByID(_ context.Context, _ uint) (*models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) FindByMovieID(_ context.Context, _ string) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) ListByMovieID(_ context.Context, _ string, _, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) CountByMovieID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (r *failingHistoryRepo) FindByBatchJobID(_ context.Context, _ string) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) FindByOperation(_ context.Context, _ models.HistoryOperation, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) ListByOperation(_ context.Context, _ models.HistoryOperation, _, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) FindByStatus(_ context.Context, _ models.HistoryStatus, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) ListByStatus(_ context.Context, _ models.HistoryStatus, _, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) FindRecent(_ context.Context, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) FindByDateRange(_ context.Context, _, _ time.Time) ([]models.History, error) {
	return nil, nil
}
func (r *failingHistoryRepo) Count(_ context.Context) (int64, error) {
	return 0, nil
}
func (r *failingHistoryRepo) CountByStatus(_ context.Context, _ models.HistoryStatus) (int64, error) {
	return 0, nil
}
func (r *failingHistoryRepo) CountByOperation(_ context.Context, _ models.HistoryOperation) (int64, error) {
	return 0, nil
}
func (r *failingHistoryRepo) Delete(_ context.Context, _ uint) error               { return nil }
func (r *failingHistoryRepo) DeleteByMovieID(_ context.Context, _ string) error    { return nil }
func (r *failingHistoryRepo) DeleteOlderThan(_ context.Context, _ time.Time) error { return nil }
func (r *failingHistoryRepo) List(_ context.Context, _, _ int) ([]models.History, error) {
	return nil, nil
}

type countingHistoryRepo struct {
	mu    sync.Mutex
	count int
}

func (r *countingHistoryRepo) createCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func (r *countingHistoryRepo) Create(_ context.Context, _ *models.History) error {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	return nil
}
func (r *countingHistoryRepo) FindByID(_ context.Context, _ uint) (*models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) FindByMovieID(_ context.Context, _ string) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) ListByMovieID(_ context.Context, _ string, _, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) CountByMovieID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (r *countingHistoryRepo) FindByBatchJobID(_ context.Context, _ string) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) FindByOperation(_ context.Context, _ models.HistoryOperation, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) ListByOperation(_ context.Context, _ models.HistoryOperation, _, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) FindByStatus(_ context.Context, _ models.HistoryStatus, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) ListByStatus(_ context.Context, _ models.HistoryStatus, _, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) FindRecent(_ context.Context, _ int) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) FindByDateRange(_ context.Context, _, _ time.Time) ([]models.History, error) {
	return nil, nil
}
func (r *countingHistoryRepo) Count(_ context.Context) (int64, error) {
	return 0, nil
}
func (r *countingHistoryRepo) CountByStatus(_ context.Context, _ models.HistoryStatus) (int64, error) {
	return 0, nil
}
func (r *countingHistoryRepo) CountByOperation(_ context.Context, _ models.HistoryOperation) (int64, error) {
	return 0, nil
}
func (r *countingHistoryRepo) Delete(_ context.Context, _ uint) error               { return nil }
func (r *countingHistoryRepo) DeleteByMovieID(_ context.Context, _ string) error    { return nil }
func (r *countingHistoryRepo) DeleteOlderThan(_ context.Context, _ time.Time) error { return nil }
func (r *countingHistoryRepo) List(_ context.Context, _, _ int) ([]models.History, error) {
	return nil, nil
}
