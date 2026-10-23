package file

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestScanDirectoryReportsWorkflowConstructionFailure(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "["
	deps := newTestDepsFromConfig(cfg)
	payload, err := json.Marshal(contracts.ScanRequest{Path: root, Recursive: true})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/scan", scanDirectory(testkit.GetTestRuntime(deps)))
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "workflow factory not available")
	require.NotContains(t, rec.Body.String(), `"files":`, "failed scan must not return a misleading partial success list")
}

func TestScanDirectoryCancelledWalkReturnsUnavailable(t *testing.T) {
	root := t.TempDir()
	// The scanner checks cancellation every 100 entries. Populate enough entries
	// to exercise that checkpoint deterministically, with no sleeps or deadlines.
	for i := 0; i < 105; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("ABC-%03d.mp4", i)), nil, 0600))
	}
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	deps := newTestDepsFromConfig(cfg)
	payload, err := json.Marshal(contracts.ScanRequest{Path: root, Recursive: true})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(string(payload))).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	router := gin.New()
	router.POST("/scan", scanDirectory(testkit.GetTestRuntime(deps)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "scan operation timed out")
	require.NotContains(t, rec.Body.String(), `"files":`)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 105, "cancelled scanning must not mutate source files")
}
