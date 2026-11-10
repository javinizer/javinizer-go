//go:build !windows

package file

import (
	"encoding/json"
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
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScanDirectoryReportsDirectoryMovedAfterValidation(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	moved := filepath.Join(parent, "moved")
	require.NoError(t, os.Mkdir(root, 0o700))
	const filename = "ABC-123.mp4"
	require.NoError(t, os.WriteFile(filepath.Join(root, filename), []byte("source bytes"), 0o600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{parent}
	cfg.Metadata.GenreReplacement.Enabled = true
	deps := newTestDepsFromConfig(cfg)
	rt := testkit.GetTestRuntime(deps)
	t.Cleanup(func() { testkit.CleanupTestRuntime(t, deps) })
	deps.Repos.MovieRepo = mocks.NewMockMovieRepositoryInterface(t)
	genres := mocks.NewMockGenreReplacementRepositoryInterface(t)
	deps.Repos.GenreReplacementRepo = genres
	// Workflow initialization loads this cache after the handler validates and
	// opens the directory, but before ScanAndMatch reads its path. Rename at
	// that boundary to model a source moving during the request without races
	// or sleeps. POSIX permits renaming a directory with an open handle.
	genres.On("GetReplacementMap", mock.Anything).
		Run(func(mock.Arguments) { require.NoError(t, os.Rename(root, moved)) }).
		Return(map[string]string{}, nil).Once()

	payload, err := json.Marshal(contracts.ScanRequest{Path: root, Recursive: false})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	router := gin.New()
	router.POST("/scan", scanDirectory(rt))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var response contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Contains(t, response.Error, "scan failed:")
	require.Contains(t, response.Error, root)
	require.NotContains(t, rec.Body.String(), `"files":`, "a failed scan must not look like an empty successful scan")
	_, err = os.Stat(root)
	require.ErrorIs(t, err, os.ErrNotExist)
	data, err := os.ReadFile(filepath.Join(moved, filename))
	require.NoError(t, err)
	require.Equal(t, "source bytes", string(data), "reporting the failure must preserve the moved source")
}
