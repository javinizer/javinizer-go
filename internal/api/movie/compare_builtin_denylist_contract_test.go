//go:build !windows

package movie_test

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
	"github.com/javinizer/javinizer-go/internal/api/movie"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestCompareNFOBuiltInDenylistPrecedesWorkflowEvenWhenExplicitlyAllowed(t *testing.T) {
	if _, err := os.Stat("/dev/null"); os.IsNotExist(err) {
		t.Skip("platform has no /dev/null")
	} else {
		require.NoError(t, err)
	}
	link := filepath.Join(t.TempDir(), "device.nfo")
	require.NoError(t, os.Symlink("/dev/null", link))
	workflowCalls := 0
	router := gin.New()
	movie.RegisterRoutes(router.Group("/api/v1"), movie.NewMovieDeps(nil,
		movie.WithAllowedDirs([]string{"/dev"}),
		movie.WithWorkflow(func() workflow.WorkflowInterface { workflowCalls++; return nil }),
	))
	for _, path := range []string{"/dev/null", link} {
		t.Run(path, func(t *testing.T) {
			body, err := json.Marshal(contracts.NFOComparisonRequest{NFOPath: path})
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/movies/CHECK-001/compare-nfo", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			var result contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, movie.ErrNFOAccessDenied.Error(), result.Error)
			require.Zero(t, workflowCalls, "denied devices must not reach NFO reading or scraper work")
		})
	}
}
