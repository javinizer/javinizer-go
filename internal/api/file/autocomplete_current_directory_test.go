package file

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
	"github.com/stretchr/testify/require"
)

func TestAutocompleteCurrentDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cwd := filepath.Join(root, "allowed")
	for _, path := range []string{filepath.Join(cwd, "inside"), filepath.Join(root, "sibling")} {
		require.NoError(t, os.MkdirAll(path, 0700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "file.txt"), []byte("not a directory"), 0600))
	t.Chdir(cwd)
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name   string
		scope  string
		allow  []string
		deny   []string
		status int
	}{
		{"operation narrow allowlist", "", []string{cwd}, nil, http.StatusOK},
		{"operation broad allowlist", "", []string{root}, nil, http.StatusOK},
		{"operation empty allowlist", "", nil, nil, http.StatusForbidden},
		{"operation outside allowlist", "", []string{filepath.Join(root, "sibling")}, nil, http.StatusForbidden},
		{"configure empty allowlist", "configure", nil, nil, http.StatusOK},
		{"configure outside allowlist", "configure", []string{filepath.Join(root, "sibling")}, nil, http.StatusOK},
		{"operation denied directory", "", []string{cwd}, []string{cwd}, http.StatusForbidden},
		{"configure denied directory", "configure", nil, []string{cwd}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			cfg.API.Security.AllowedDirectories = tc.allow
			cfg.API.Security.DeniedDirectories = tc.deny
			router := gin.New()
			RegisterRoutes(router.Group("/api/v1"), testkit.GetTestRuntime(newTestDepsFromConfig(cfg)))
			for _, input := range []string{".", "./", "./.", "inside/..", " ./inside/../. "} {
				t.Run(input, func(t *testing.T) {
					response := requestCurrentDirectoryAutocomplete(t, router, input, tc.scope)
					require.Equal(t, tc.status, response.Code, response.Body.String())
					if tc.status != http.StatusOK {
						var failure contracts.ErrorResponse
						require.NoError(t, json.Unmarshal(response.Body.Bytes(), &failure))
						require.NotEmpty(t, failure.Error)
						return
					}
					var result contracts.PathAutocompleteResponse
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
					require.Equal(t, input, result.InputPath)
					require.Equal(t, cwd, result.BasePath)
					require.Equal(t, []contracts.PathAutocompleteSuggestion{{Name: "inside", Path: filepath.Join(cwd, "inside"), IsDir: true}}, result.Suggestions)
				})
			}
		})
	}
}

func TestAutocompleteCurrentDirectoryPreservesPrefixCompletion(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, name := range []string{"inside", "incoming", "other"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0700))
	}
	t.Chdir(root)
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), testkit.GetTestRuntime(newTestDepsFromConfig(cfg)))
	for _, input := range []string{"in", "./in", "inside/../in", filepath.Join(root, "in")} {
		t.Run(input, func(t *testing.T) {
			response := requestCurrentDirectoryAutocomplete(t, router, input, "")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var result contracts.PathAutocompleteResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, root, result.BasePath)
			require.Equal(t, []contracts.PathAutocompleteSuggestion{
				{Name: "incoming", Path: filepath.Join(root, "incoming"), IsDir: true},
				{Name: "inside", Path: filepath.Join(root, "inside"), IsDir: true},
			}, result.Suggestions)
		})
	}
	for _, input := range []string{"", "   "} {
		t.Run("invalid "+input, func(t *testing.T) {
			response := requestCurrentDirectoryAutocomplete(t, router, input, "")
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/browse/autocomplete", bytes.NewBufferString("{"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
}

func requestCurrentDirectoryAutocomplete(t *testing.T, router *gin.Engine, path, scope string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(contracts.PathAutocompleteRequest{Path: path, Scope: scope})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/browse/autocomplete", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
