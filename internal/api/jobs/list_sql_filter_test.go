package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func (sqlErrRepo) ListByStatus(context.Context, string, int) ([]models.Job, error) {
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

	running, err := svc.ListJobsWithStatsByStatus(context.Background(), "running", 0)
	require.NoError(t, err)
	require.Len(t, running, 2)

	all, err := svc.ListJobsWithStatsByStatus(context.Background(), "", 0)
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
	_, err := svc.ListJobsWithStatsByStatus(context.Background(), "running", 0)
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

// TestListJobs_HandlerHonorsLimit: the restore probe requests limit=1 — the
// endpoint must bound the SQL fetch instead of decoding/aggregating every
// running row (codex P2, PR #255), and invalid values fall back to the
// unbounded default.
func TestListJobs_HandlerHonorsLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)

	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(newTestJobDeps(deps)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running&limit=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 1)
	assert.Equal(t, models.JobStatusRunning, resp.Jobs[0].Status)

	bogus := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running&limit=bogus", nil)
	bogusW := httptest.NewRecorder()
	router.ServeHTTP(bogusW, bogus)
	require.Equal(t, http.StatusOK, bogusW.Code)
	var bogusResp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(bogusW.Body.Bytes(), &bogusResp))
	assert.Len(t, bogusResp.Jobs, 2)
}

// TestListJobsWithStatsByStatus_FallbackLimit: legacy repos (no seam) must be
// bounded after in-memory filtering, so envelope decode and aggregate counts
// only run for the requested rows (codex P2, PR #255).
func TestListJobsWithStatsByStatus_FallbackLimit(t *testing.T) {
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)

	svc := newTestJobDeps(deps)
	svc.JobRepo = legacyRepo{svc.JobRepo}

	limited, err := svc.ListJobsWithStatsByStatus(context.Background(), "running", 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, models.JobStatusRunning, limited[0].Job.Status)
}

// seedPhaseApplyJob adds a running apply-phase job whose StartedAt is NEWER
// than the scrape fixture so an unfiltered LIMIT 1 would return it — proving
// the phase predicate lives in SQL, ahead of the row cap (codex P2, PR #255).
func seedPhaseApplyJob(t *testing.T, deps *core.APIDeps) {
	t.Helper()
	apply := &models.Job{ID: "run-apply-1", Status: models.JobStatusRunning, Progress: 0.9, Files: "[]", Excluded: "{}", Results: `{"domain":{},"current_phase":"apply"}`, FileMatchInfo: "{}", StartedAt: time.Now().Add(time.Hour)}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), apply))
}

// phaseErrRepo exposes the phase seam but errors, exercising the SQL-filter
// error branch of ListJobsWithStatsByStatusAndPhase.
type phaseErrRepo struct{ legacyRepo }

func (phaseErrRepo) ListByStatusAndPhase(context.Context, string, string, int) ([]models.Job, error) {
	return nil, errors.New("phase sql boom")
}

// TestListJobs_SQLFilteredPhasePath: status+phase+limit all reach SQL — the
// apply-phase job is newer than the scrape job yet LIMIT 1 still returns the
// scrape row.
func TestListJobs_SQLFilteredPhasePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)
	seedPhaseApplyJob(t, deps)

	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(newTestJobDeps(deps)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running&phase=scrape&limit=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 1)
	// run-apply-1 is the NEWEST running job yet must lose to any scrape-
	// eligible row; both fixtures carry zero StartedAt, so the id-DESC
	// tie-break makes run-legacy-1 the deterministic winner.
	assert.Equal(t, "run-legacy-1", resp.Jobs[0].ID)
	assert.NotEqual(t, "run-apply-1", resp.Jobs[0].ID)

	unbounded := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running&phase=scrape", nil)
	unboundedW := httptest.NewRecorder()
	router.ServeHTTP(unboundedW, unbounded)
	require.Equal(t, http.StatusOK, unboundedW.Code)
	var ub contracts.JobListResponse
	require.NoError(t, json.Unmarshal(unboundedW.Body.Bytes(), &ub))
	require.Len(t, ub.Jobs, 2)
	for _, item := range ub.Jobs {
		assert.NotEqual(t, "run-apply-1", item.ID)
	}
}

// TestListJobsWithStatsByStatusAndPhase_FallbackPath: repos without the
// phase seam decode the envelope in memory over the unbounded status-filtered
// set, then cap — apply-phase rows are excluded without hiding scrape rows.
func TestListJobsWithStatsByStatusAndPhase_FallbackPath(t *testing.T) {
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)
	seedPhaseApplyJob(t, deps)

	svc := newTestJobDeps(deps)
	svc.JobRepo = legacyRepo{svc.JobRepo}

	unbounded, err := svc.ListJobsWithStatsByStatusAndPhase(context.Background(), "running", "scrape", 0)
	require.NoError(t, err)
	require.Len(t, unbounded, 2)
	for _, stat := range unbounded {
		assert.NotEqual(t, "run-apply-1", stat.Job.ID)
	}

	limited, err := svc.ListJobsWithStatsByStatusAndPhase(context.Background(), "running", "scrape", 1)
	require.NoError(t, err)
	assert.Len(t, limited, 1)

	// Exact-match rule for non-scrape phases: run-apply-1 alone qualifies;
	// the two legacy/scrape fixture rows are excluded.
	applyOnly, err := svc.ListJobsWithStatsByStatusAndPhase(context.Background(), "running", "apply", 0)
	require.NoError(t, err)
	require.Len(t, applyOnly, 1)
	assert.Equal(t, "run-apply-1", applyOnly[0].Job.ID)
}

// TestListJobsWithStatsByStatusAndPhase_SQLFilterErr: a failing phase seam
// propagates its error instead of falling back silently.
func TestListJobsWithStatsByStatusAndPhase_SQLFilterErr(t *testing.T) {
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	svc.JobRepo = phaseErrRepo{}
	_, err := svc.ListJobsWithStatsByStatusAndPhase(context.Background(), "running", "scrape", 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "phase sql boom")
}

// TestListJobsWithStatsByStatusAndPhase_FallbackListErr: with no phase seam
// on the repo, the fallback's unbounded status listing error propagates.
func TestListJobsWithStatsByStatusAndPhase_FallbackListErr(t *testing.T) {
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.EXPECT().List(mockpkg.Anything).Return(nil, errors.New("legacy list fail"))

	svc := newTestJobDeps(deps)
	svc.JobRepo = mockRepo
	_, err := svc.ListJobsWithStatsByStatusAndPhase(context.Background(), "running", "scrape", 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "legacy list fail")
}

// TestListJobs_SQLBoundedLimitOnlyPath: ?limit without a status must grade
// into ListByStatus("", limit) so the SQL query itself is bounded, keeping
// full-history materialization off the wire even when no status/phase
// predicate applies (codex P2, PR #255).
func TestListJobs_SQLBoundedLimitOnlyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)
	seedPhaseApplyJob(t, deps)

	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(newTestJobDeps(deps)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?limit=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 1)
	// newest first: the apply fixture is started in the future
	assert.Equal(t, "run-apply-1", resp.Jobs[0].ID)
}

// TestListJobsWithStatsByStatus_LimitWithoutStatusFallback: legacy doubles
// without the seam still honor limit-only caps via in-memory truncation.
func TestListJobsWithStatsByStatus_LimitOnlyFallback(t *testing.T) {
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()
	seedStatusMixedJobs(t, deps)

	svc := newTestJobDeps(deps)
	svc.JobRepo = legacyRepo{svc.JobRepo}

	limited, err := svc.ListJobsWithStatsByStatus(context.Background(), "", 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)

	unbounded, err := svc.ListJobsWithStatsByStatus(context.Background(), "", 0)
	require.NoError(t, err)
	assert.Len(t, unbounded, 3)
}

// TestListJobs_HandlerPhaseSQLFilterErr500: handler surfaces a failing phase
// seam as 500 when the phase param is present.
func TestListJobs_HandlerPhaseSQLFilterErr500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	svc.JobRepo = phaseErrRepo{}
	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running&phase=scrape&limit=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}
