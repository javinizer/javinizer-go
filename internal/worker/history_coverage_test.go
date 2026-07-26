package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
)

func TestAuditCoverage_OrganizeFailureWithURLInError(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	inputs := applyPhaseInputs{JobID: "cov-job", HistoryRepo: repos.HistoryRepo, OperationMode: "organize"}
	movie := &models.Movie{ID: "COV-001"}
	result := &workflow.ApplyResult{}
	auditOrganizeFailure(inputs, movie, "/in/cov.mp4", result, context.DeadlineExceeded, ApplyPhaseConfig{})
	records, err := repos.HistoryRepo.FindByMovieID(context.Background(), "COV-001")
	assert.NoError(t, err)
	assert.NotEmpty(t, records)
}

func TestAuditCoverage_OrganizeFailureWithURLErrorMessage(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	inputs := applyPhaseInputs{JobID: "cov-url-job", HistoryRepo: repos.HistoryRepo, OperationMode: "organize"}
	movie := &models.Movie{ID: "COV-002"}
	result := &workflow.ApplyResult{}
	auditOrganizeFailure(inputs, movie, "/in/cov.mp4", result, newURLError("https://alice:secret@example.com/path"), ApplyPhaseConfig{})
	records, err := repos.HistoryRepo.FindByMovieID(context.Background(), "COV-002")
	assert.NoError(t, err)
	if len(records) > 0 {
		assert.NotContains(t, records[0].ErrorMessage, "alice")
		assert.NotContains(t, records[0].ErrorMessage, "secret")
	}
}

func TestAuditCoverage_ScrapeFailureEmptyErrMsg(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	inputs := scrapePhaseInputs{JobID: "cov-empty-job", HistoryRepo: repos.HistoryRepo}
	o := scrapeFileOutcome{FilePath: "/in/empty.mp4", MovieID: "EMPTY-001", Failed: true, ErrorMsg: ""}
	auditScrapeFailure(inputs, o)
	records, err := repos.HistoryRepo.FindByBatchJobID(context.Background(), "cov-empty-job")
	assert.NoError(t, err)
	assert.Empty(t, records, "empty errMsg with Failed but no Panic should not write a row")
}

func TestAuditCoverage_RescrapeFailureCancellation(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	inputs := rescrapePhaseInputs{JobID: "cov-rescrape-job", HistoryRepo: repos.HistoryRepo}
	auditRescrapeFailure(inputs, "R-001", "/in/r.mp4", context.Canceled)
	records, err := repos.HistoryRepo.FindByBatchJobID(context.Background(), "cov-rescrape-job")
	assert.NoError(t, err)
	assert.Empty(t, records, "cancellation should not write a row")
}

func TestAuditCoverage_OrganizeMetadataMarshalError(t *testing.T) {
	result := &workflow.ApplyResult{FailedStep: "download"}
	meta := organizeMetadata("organize", result)
	assert.Contains(t, meta, "organize")
	assert.Contains(t, meta, "operation_mode")
}

type urlError struct {
	msg string
}

func (e *urlError) Error() string { return e.msg }

func newURLError(msg string) error { return &urlError{msg: msg} }
