package batch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRescrapeRejectsUnreadMalformedTrailingPayloadAndReleasesAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = t.TempDir()
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch(nil)
	setJobStatus(job, models.JobStatusCompleted)
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), testkit.GetTestRuntime(deps))
	path := "/api/v1/batch/" + job.GetID() + "/results/missing/rescrape"
	valid := `{"manual_search_input":"IPX-001"}`

	// A large legal whitespace gap leaves the malformed suffix beyond the
	// first JSON decoder's read-ahead. The shared request preparation must
	// reject it rather than silently proceeding with defaults or a scrape.
	payload := valid + strings.Repeat(" ", 64*1024) + `{"operation_mode":`
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"error":"invalid request body"}`, rec.Body.String())
	require.Equal(t, models.JobStatusCompleted, job.GetStatus().Status)

	// The failed request acquired rescrape admission before preparation.
	// Verify that its early return releases the real store lease.
	_, release, err := deps.JobStore.AcquireRescrapeAccess(job.GetID())
	require.NoError(t, err)
	require.NotNil(t, release)
	release()

	// The same ordinary request without the malformed suffix gets past
	// parsing and admission to the expected missing-result response.
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(valid))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"error":"Result missing not found in job"}`, rec.Body.String())
	require.Equal(t, models.JobStatusCompleted, job.GetStatus().Status)
}
