package file

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

// Bare drive-relative paths retain their existing prefix-completion semantics.
// The dot-only fix must not change their empty fragment into a literal dot filter.
func TestAutocompleteDriveRelativePreservesEmptyFragment(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cwd := filepath.Join(root, "allowed")
	for _, path := range []string{cwd, filepath.Join(root, "sibling")} {
		require.NoError(t, os.Mkdir(path, 0700))
	}
	t.Chdir(cwd)
	volume := filepath.VolumeName(cwd)
	if len(volume) != 2 || volume[1] != ':' {
		t.Skip("drive-relative fixture requires a local drive, not a UNC temp root")
	}
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), testkit.GetTestRuntime(newTestDepsFromConfig(cfg)))
	for _, input := range []string{volume, volume + "."} {
		t.Run(input, func(t *testing.T) {
			resolved, err := filepath.Abs(input)
			require.NoError(t, err)
			require.Equal(t, cwd, resolved)
			response := requestCurrentDirectoryAutocomplete(t, router, input, "")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var result contracts.PathAutocompleteResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, root, result.BasePath)
			require.Equal(t, []contracts.PathAutocompleteSuggestion{
				{Name: "allowed", Path: cwd, IsDir: true},
				{Name: "sibling", Path: filepath.Join(root, "sibling"), IsDir: true},
			}, result.Suggestions)
		})
	}
}
