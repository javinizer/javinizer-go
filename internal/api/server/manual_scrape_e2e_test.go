package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraper/e2emock"
)

type batchScrapeResponse struct {
	JobID string `json:"job_id"`
}

type batchJobResponse struct {
	ID      string                                `json:"id"`
	Status  string                                `json:"status"`
	Results map[string]*contracts.BatchFileResult `json:"results"`
}

func setupManualScrapeE2E(t *testing.T) (*gin.Engine, string) {
	t.Helper()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "ABC-123.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("fake"), 0o644))

	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "error"},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{e2emock.Name},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
			},
		},
		Output: config.OutputConfig{
			Download: config.OutputDownloadConfig{DownloadTimeout: 2},
		},
	}

	deps := testkit.CreateTestDeps(t, cfg, "")
	deps.CoreDeps.ScraperRegistry.RegisterInstance(&e2emock.Scraper{})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	batch.RegisterRoutes(router.Group(""), testkit.GetTestRuntime(deps))
	return router, filePath
}

func TestE2E_ManualScrapeInputFlowsAsMovieID(t *testing.T) {
	router, filePath := setupManualScrapeE2E(t)

	reqBody, _ := json.Marshal(map[string]any{
		"files":         []string{filePath},
		"manual_inputs": map[string]string{filePath: "GOOD-999"},
	})
	req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, "batch scrape with valid manual input should be accepted: %s", w.Body.String())

	var scrapeResp batchScrapeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &scrapeResp))
	require.NotEmpty(t, scrapeResp.JobID, "job should be created")

	var jobResp batchJobResponse
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		gReq := httptest.NewRequest("GET", "/batch/"+scrapeResp.JobID+"?include_data=true", nil)
		gW := httptest.NewRecorder()
		router.ServeHTTP(gW, gReq)
		if gW.Code != 200 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		require.NoError(t, json.Unmarshal(gW.Body.Bytes(), &jobResp))
		if r, ok := jobResp.Results[filePath]; ok && r.MovieID != "" {
			assert.Equal(t, "GOOD-999", r.MovieID, "manual input must flow end-to-end as the scrape MovieID, bypassing the matcher")
			assert.NotEqual(t, "ABC-123", r.MovieID, "the filename matcher (ABC-123) must be bypassed when a manual input is present")
			if r.Movie != nil {
				assert.Equal(t, "GOOD-999", r.Movie.ID, "the mock scraper must be queried with the manual input ID")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("scrape result for %s never materialized within 10s (last status=%s, results=%v)", filePath, jobResp.Status, jobResp.Results)
}

func TestE2E_ManualScrapeOrphanKeyRejected(t *testing.T) {
	router, filePath := setupManualScrapeE2E(t)

	orphanPath := filepath.Join(filepath.Dir(filePath), "NONEXISTENT-999.mp4")
	reqBody, _ := json.Marshal(map[string]any{
		"files":         []string{filePath},
		"manual_inputs": map[string]string{orphanPath: "GOOD-999"},
	})
	req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code, "manual_inputs key not present in files must be rejected (task-30 cross-check)")
	assert.Contains(t, w.Body.String(), "is not in files")
}
