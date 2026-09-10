package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
)

// TestListJobs_CurrentPhase pins the jobs-list exposure of the durable phase
// marker: a running job carries scrape or apply from its persisted results
// envelope, letting reload-restore flows pick out in-flight scrapes
// (codex P2, PR #253).
func TestListJobs_CurrentPhase(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	seedJobsData(t, deps)

	runScrape := models.Job{
		ID:       "run-scrape",
		Status:   models.JobStatusRunning,
		Progress: 0.5,
		Results:  `{"domain":{},"current_phase":"scrape"}`,
		Files:    "[]", Excluded: "{}", FileMatchInfo: "{}",
	}
	runApply := models.Job{
		ID:       "run-apply",
		Status:   models.JobStatusRunning,
		Progress: 0.5,
		Results:  `{"domain":{},"current_phase":"apply"}`,
		Files:    "[]", Excluded: "{}", FileMatchInfo: "{}",
	}
	require.NoError(t, db.Create(&runScrape).Error)
	require.NoError(t, db.Create(&runApply).Error)

	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(newTestJobDeps(deps)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=running", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	phases := map[string]string{}
	for _, job := range resp.Jobs {
		phases[job.ID] = job.CurrentPhase
	}
	require.Len(t, resp.Jobs, 2)
	assert.Equal(t, "scrape", phases["run-scrape"])
	assert.Equal(t, "apply", phases["run-apply"])

	// Non-running jobs omit the marker entirely. The seeded data (organized/
	// reverted/etc.) is historical, so the unfiltered listing must carry an
	// empty CurrentPhase on every seeded row.
	reqAll := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	wAll := httptest.NewRecorder()
	router.ServeHTTP(wAll, reqAll)
	require.Equal(t, http.StatusOK, wAll.Code)
	var respAll contracts.JobListResponse
	require.NoError(t, json.Unmarshal(wAll.Body.Bytes(), &respAll))
	for _, job := range respAll.Jobs {
		if job.ID != "run-scrape" && job.ID != "run-apply" {
			assert.Empty(t, job.CurrentPhase, "non-running job %s must not carry a phase marker", job.ID)
		}
	}
}
