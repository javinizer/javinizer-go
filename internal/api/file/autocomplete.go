package file

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/api/core"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

const maxPathAutocompleteResults = 25

// autocompletePath godoc
// @Summary Autocomplete directory path
// @Description Returns directory suggestions for a partially typed path
// @Tags web
// @Accept json
// @Produce json
// @Param request body contracts.PathAutocompleteRequest true "Autocomplete parameters"
// @Success 200 {object} contracts.PathAutocompleteResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Router /api/v1/browse/autocomplete [post]
func autocompletePath(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.PathAutocompleteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		apiCfg := rt.GetAPIConfig()
		basePath, fragment, err := resolveAutocompleteBasePath(req.Path, apiCfg.SecurityConfig())
		if err != nil {
			apperrors.WriteAPIError(c, err)
			return
		}

		entries, err := os.ReadDir(basePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		fragmentLower := strings.ToLower(fragment)
		suggestions := make([]contracts.PathAutocompleteSuggestion, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			name := entry.Name()
			if fragmentLower != "" && !strings.HasPrefix(strings.ToLower(name), fragmentLower) {
				continue
			}

			suggestions = append(suggestions, contracts.PathAutocompleteSuggestion{
				Name:  name,
				Path:  filepath.Join(basePath, name),
				IsDir: true,
			})
		}

		sort.Slice(suggestions, func(i, j int) bool {
			return strings.ToLower(suggestions[i].Name) < strings.ToLower(suggestions[j].Name)
		})

		limit := req.Limit
		if limit <= 0 || limit > maxPathAutocompleteResults {
			limit = maxPathAutocompleteResults
		}
		if len(suggestions) > limit {
			suggestions = suggestions[:limit]
		}

		c.JSON(http.StatusOK, contracts.PathAutocompleteResponse{
			InputPath:   req.Path,
			BasePath:    basePath,
			Suggestions: suggestions,
		})
	}
}

func resolveAutocompleteBasePath(userPath string, cfg *core.SecurityNarrowConfig) (string, string, error) {
	trimmedPath := strings.TrimSpace(userPath)
	if trimmedPath == "" {
		return "", "", fmt.Errorf("path is required")
	}

	expandedPath := core.ExpandHomeDir(trimmedPath)
	trimmedPath = filepath.Clean(expandedPath)

	absPath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return "", "", apperrors.ErrPathInvalid
	}

	basePath := absPath
	fragment := ""
	if !hasTrailingPathSeparator(expandedPath) && trimmedPath != string(os.PathSeparator) {
		basePath = filepath.Dir(absPath)
		fragment = filepath.Base(trimmedPath)
		if fragment == "." {
			fragment = ""
		}
	}

	validBasePath, err := core.ValidateScanPath(basePath, cfg)
	if err != nil {
		return "", "", err
	}

	return validBasePath, fragment, nil
}

func hasTrailingPathSeparator(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\")
}
