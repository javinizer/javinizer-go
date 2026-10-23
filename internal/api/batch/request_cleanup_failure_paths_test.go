package batch

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPrepareBatchRequestRejectsMalformedAndInvalidSeams(t *testing.T) {
	cases := []struct{ name, body, detail string }{
		{"malformed JSON", `{"operation_mode":`, "invalid request body"},
		{"non-string operation", `{"operation_mode":42}`, "must be a string"},
		{"non-string link mode", `{"link_mode":false}`, "must be a string"},
		{"non-string scalar strategy", `{"scalar_strategy":[]}`, "must be a string"},
		{"non-string array strategy", `{"array_strategy":{}}`, "must be a string"},
		{"unknown operation", `{"operation_mode":"not-a-mode"}`, "operation"},
	}
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	snapshot := testkit.GetTestRuntime(deps).Snapshot()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/batch/missing", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: "missing"}}
			job, err := prepareBatchRequest(snapshot, c)
			require.Nil(t, job)
			require.ErrorContains(t, err, tc.detail)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.NotContains(t, rec.Body.String(), "Job not found", "validation must reject before looking up a job")
		})
	}
}
func TestCleanupJobTempPostersInvalidIDAndRemovalFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	const root = "/temporary"
	require.NoError(t, base.MkdirAll(root, 0700))
	sentinel := filepath.Join(root, "keep.txt")
	require.NoError(t, afero.WriteFile(base, sentinel, []byte("preserve"), 0600))
	cleanupJobTempPosters(base, "../..", root)
	data, err := afero.ReadFile(base, sentinel)
	require.NoError(t, err)
	require.Equal(t, "preserve", string(data))
	const id = "12345678-1234-1234-1234-123456789012"
	poster := filepath.Join(root, "posters", id, "poster.jpg")
	require.NoError(t, base.MkdirAll(filepath.Dir(poster), 0700))
	require.NoError(t, afero.WriteFile(base, poster, []byte("poster"), 0600))
	cleanupJobTempPosters(afero.NewReadOnlyFs(base), id, root)
	data, err = afero.ReadFile(base, poster)
	require.NoError(t, err)
	require.Equal(t, "poster", string(data))
}
