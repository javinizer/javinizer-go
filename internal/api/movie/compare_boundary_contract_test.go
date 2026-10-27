package movie

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestCompareNFORejectsInvalidBodyBeforeWorkflow(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"malformed JSON", `{"nfo_path":`, "invalid request body: "},
		{"wrong field type", `{"nfo_path":42}`, "invalid request body: "},
		{"empty body uses defaults", "", "nfo_path is required for comparison"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflowCalls := 0
			deps := MovieDeps{WorkflowFn: func() workflow.WorkflowInterface {
				workflowCalls++
				return nil
			}}
			router := gin.New()
			router.POST("/movies/:id/compare-nfo", compareNFO(deps))
			req := httptest.NewRequest(http.MethodPost, "/movies/IPX-535/compare-nfo", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var response contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			if tc.body == "" {
				require.Equal(t, tc.want, response.Error)
			} else {
				require.Contains(t, response.Error, tc.want)
				require.NotContains(t, response.Error, "nfo_path is required")
			}
			require.Zero(t, workflowCalls, "invalid input must not acquire a workflow")
		})
	}
}

func TestCompareNFORejectsInvalidPathBeforeWorkflow(t *testing.T) {
	root := t.TempDir()
	invalidPath := filepath.Join(root, "invalid\x00.nfo")
	validated, err := validateNFOPath(invalidPath, []string{root})
	require.ErrorIs(t, err, ErrNFOInvalidPath)
	require.Empty(t, validated)

	workflowCalls := 0
	deps := MovieDeps{
		AllowedDirs: []string{root},
		WorkflowFn: func() workflow.WorkflowInterface {
			workflowCalls++
			return nil
		},
	}
	router := gin.New()
	router.POST("/movies/:id/compare-nfo", compareNFO(deps))
	body, err := json.Marshal(contracts.NFOComparisonRequest{NFOPath: invalidPath})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/movies/IPX-535/compare-nfo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var response contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, ErrNFOInvalidPath.Error(), response.Error)
	require.Zero(t, workflowCalls)
}

func TestCompareNFOReportsUnsupportedWorkflow(t *testing.T) {
	root := t.TempDir()
	nfoPath := filepath.Join(root, "IPX-535.nfo")
	nfoBytes := []byte(`<movie><id>IPX-535</id><title>Existing title</title></movie>`)
	require.NoError(t, os.WriteFile(nfoPath, nfoBytes, 0o644))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	deps := createTestDeps(t, cfg, "")
	// A real scan-only workflow rejects Compare without scraping or parsing.
	wf, err := testkit.GetTestRuntime(deps).GetScanOnlyWorkflow()
	require.NoError(t, err)
	workflowCalls := 0
	movieDeps := MovieDeps{
		AllowedDirs: []string{root},
		WorkflowFn: func() workflow.WorkflowInterface {
			workflowCalls++
			return wf
		},
	}
	router := gin.New()
	router.POST("/movies/:id/compare-nfo", compareNFO(movieDeps))
	body, err := json.Marshal(contracts.NFOComparisonRequest{NFOPath: nfoPath})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/movies/IPX-535/compare-nfo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	var response contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "compare not configured", response.Error)
	require.Equal(t, 1, workflowCalls)
	got, err := os.ReadFile(nfoPath)
	require.NoError(t, err)
	require.Equal(t, nfoBytes, got)
}
