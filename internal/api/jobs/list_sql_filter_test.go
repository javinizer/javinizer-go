package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	mockpkg "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
)

func seedStatusMixedJobs(t *testing.T, deps *core.APIDeps) {
	t.Helper()
	running := &models.Job{ID: "run-fx-1", Status: models.JobStatusRunning, Progress: 0.5, Files: "[]", Excluded: "{}", Results: `{"domain":{},"current_phase":"scrape"}`, FileMatchInfo: "{}"}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), running))
	organized := &models.Job{ID: "org-fx-1", Status: models.JobStatusOrganized, Progress: 1, Files: "[]", Excluded: "{}", Results: `{"domain":{}}`, FileMatchInfo: "{}"}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), organized))

	// legacy envelope with no current_phase exercises the decode-miss path in
	// listJobs (phase stays empty; the frontend treats missing as scrape-eligible)
	legacy := &models.Job{ID: "run-legacy-1", Status: models.JobStatusRunning, Progress: 0.1, Files: "[]", Excluded: "{}", Results: `{"domain":{}}`, FileMatchInfo: "{}"}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), legacy))
}

// legacyRepo embeds the interface, hiding the concrete ListByStatus from the
// static method set — exercises the in-memory fallback inside the filtered
// service path.
type legacyRepo struct {
	database.JobRepositoryInterface
}

// sqlErrRepo exposes the seam ListByStatus but errors, exercising the
// SQL-filter error branch.
type sqlErrRepo struct{ legacyRepo }

func (sqlErrRepo) ListByStatus(context.Context, string) ([]models.Job, error) {
	return nil, errors.New("sql boom")
}

// TestListJobs_SQLFilteredPath: status=running queries go through SQL when the
// repo exposes the seam — only running rows are returned and the durable
// current_phase marker is decoded (codex P2, PR #253).
func TestListJobs_SQLFilteredPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)

	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(newTestJobDeps(deps)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 2)
	byID := make(map[string]contracts.JobListItem, len(resp.Jobs))
	for _, item := range resp.Jobs {
		byID[item.ID] = item
	}
	require.Contains(t, byID, "run-fx-1")
	assert.Equal(t, "scrape", byID["run-fx-1"].CurrentPhase)
	require.Contains(t, byID, "run-legacy-1")
	assert.Empty(t, byID["run-legacy-1"].CurrentPhase)

	allReq := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	allW := httptest.NewRecorder()
	router.ServeHTTP(allW, allReq)
	var all contracts.JobListResponse
	require.NoError(t, json.Unmarshal(allW.Body.Bytes(), &all))
	assert.Len(t, all.Jobs, 3)
}

// TestListJobsWithStatsByStatus_FallbackPath: repos without the seam still
// filter correctly via the in-memory fallback.
func TestListJobsWithStatsByStatus_FallbackPath(t *testing.T) {
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)

	svc := newTestJobDeps(deps)
	svc.JobRepo = legacyRepo{svc.JobRepo}

	running, err := svc.ListJobsWithStatsByStatus(context.Background(), "running")
	require.NoError(t, err)
	require.Len(t, running, 2)

	all, err := svc.ListJobsWithStatsByStatus(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

// TestListJobsWithStatsByStatus_SQLFilterErr: a failing seam propagates its
// error instead of falling back silently.
func TestListJobsWithStatsByStatus_SQLFilterErr(t *testing.T) {
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	svc.JobRepo = sqlErrRepo{}
	_, err := svc.ListJobsWithStatsByStatus(context.Background(), "running")
	require.Error(t, err)
	require.Contains(t, err.Error(), "sql boom")
}

// TestListJobs_HandlerInMemoryFallbackFilter: handler with a legacy repo
// (interface-only, no seam) queries with a status filter and exercises the
// in-memory status mismatch `continue` at list.go:43.
func TestListJobs_HandlerInMemoryFallbackFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)

	svc := newTestJobDeps(deps)
	svc.JobRepo = legacyRepo{svc.JobRepo}
	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 2)
	for _, item := range resp.Jobs {
		assert.Equal(t, models.JobStatusRunning, item.Status)
	}
}

// TestListJobs_FallbackListErr500: mock JobRepositoryInterface (no seam) so
// the in-memory fallback runs; List returning an error maps the handler to
// 500.
func TestListJobs_FallbackListErr500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.EXPECT().List(mockpkg.Anything).Return(nil, errors.New("legacy list fail"))

	svc := newTestJobDeps(deps)
	svc.JobRepo = mockRepo
	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}
