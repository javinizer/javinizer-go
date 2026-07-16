package movie

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/timeout/stalltest"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deadlineCapturingWorkflow struct {
	gotDeadline bool
	deadline    time.Time
}

func (w *deadlineCapturingWorkflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	dl, ok := ctx.Deadline()
	w.gotDeadline = ok
	w.deadline = dl
	return &scrape.ScrapeResult{Status: scrape.StatusCompleted, Movie: &models.Movie{ID: cmd.MovieID}}, nil, nil
}

func (w *deadlineCapturingWorkflow) Apply(ctx context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	return nil, nil
}

func (w *deadlineCapturingWorkflow) Preview(ctx context.Context, cmd workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (w *deadlineCapturingWorkflow) Compare(ctx context.Context, cmd workflow.CompareCmd) (*workflow.CompareResult, error) {
	dl, ok := ctx.Deadline()
	w.gotDeadline = ok
	w.deadline = dl
	return &workflow.CompareResult{}, nil
}

func (w *deadlineCapturingWorkflow) ScanAndMatch(ctx context.Context, cmd workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func TestScrapeMovie_RequestTimeoutApplied(t *testing.T) {
	wf := &deadlineCapturingWorkflow{}
	deps := MovieDeps{
		WorkflowFn:       func() workflow.WorkflowInterface { return wf },
		RequestTimeoutFn: func() time.Duration { return 5 * time.Second },
	}

	router := gin.New()
	router.POST("/scrape", scrapeMovie(deps))

	body := `{"id":"TEST-001"}`
	req := httptest.NewRequest(http.MethodPost, "/scrape", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, wf.gotDeadline, "scrape context should have a deadline")
	if wf.gotDeadline {
		remaining := time.Until(wf.deadline)
		assert.LessOrEqual(t, remaining, 5*time.Second, "deadline should be <= 5s")
		assert.Greater(t, remaining, 3*time.Second, "deadline should not have expired")
	}
}

func TestScrapeMovie_RequestTimeoutZeroSkipsDeadline(t *testing.T) {
	wf := &deadlineCapturingWorkflow{}
	deps := MovieDeps{
		WorkflowFn:       func() workflow.WorkflowInterface { return wf },
		RequestTimeoutFn: func() time.Duration { return 0 },
	}

	router := gin.New()
	router.POST("/scrape", scrapeMovie(deps))

	body := `{"id":"TEST-001"}`
	req := httptest.NewRequest(http.MethodPost, "/scrape", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, wf.gotDeadline, "scrape context should NOT have a deadline when timeout is zero")
}

func TestScrapeMovie_RequestTimeoutNilSkipsDeadline(t *testing.T) {
	wf := &deadlineCapturingWorkflow{}
	deps := MovieDeps{
		WorkflowFn: func() workflow.WorkflowInterface { return wf },
	}

	router := gin.New()
	router.POST("/scrape", scrapeMovie(deps))

	body := `{"id":"TEST-001"}`
	req := httptest.NewRequest(http.MethodPost, "/scrape", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, wf.gotDeadline, "scrape context should NOT have a deadline when RequestTimeoutFn is nil")
}

func TestRescrapeMovie_RequestTimeoutApplied(t *testing.T) {
	wf := &deadlineCapturingWorkflow{}
	deps := MovieDeps{
		WorkflowFn:       func() workflow.WorkflowInterface { return wf },
		RequestTimeoutFn: func() time.Duration { return 5 * time.Second },
	}

	router := gin.New()
	router.POST("/movies/:id/rescrape", rescrapeMovie(deps))

	body := `{"selected_scrapers":["r18dev"],"force":false}`
	req := httptest.NewRequest(http.MethodPost, "/movies/TEST-001/rescrape", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, wf.gotDeadline, "rescrape context should have a deadline")
}

func TestCompareNFO_RequestTimeoutApplied(t *testing.T) {
	tmpDir := t.TempDir()
	nfoPath := filepath.Join(tmpDir, "test.nfo")
	err := os.WriteFile(nfoPath, []byte("<movie></movie>"), 0644)
	require.NoError(t, err)

	wf := &deadlineCapturingWorkflow{}
	deps := MovieDeps{
		WorkflowFn:       func() workflow.WorkflowInterface { return wf },
		RequestTimeoutFn: func() time.Duration { return 5 * time.Second },
		AllowedDirs:      []string{tmpDir},
	}

	router := gin.New()
	router.POST("/movies/:id/compare-nfo", compareNFO(deps))

	bodyBytes, err := json.Marshal(map[string]interface{}{
		"nfo_path":          nfoPath,
		"selected_scrapers": []string{"r18dev"},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/movies/TEST-001/compare-nfo", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, wf.gotDeadline, "compare context should have a deadline")
}

func TestStallServer_Requests(t *testing.T) {
	srv := stalltest.New(10 * time.Millisecond)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	resp2, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.Equal(t, 2, srv.Requests())
}
