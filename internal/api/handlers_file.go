package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
)

const maxPathAutocompleteResults = 25

// scanDirectory godoc
// @Summary Scan directory for video files
// @Description Scan a directory for video files and match JAV IDs
// @Tags web
// @Accept json
// @Produce json
// @Param request body ScanRequest true "Scan parameters"
// @Success 200 {object} ScanResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/scan [post]
func scanDirectory(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ScanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		// Read current config (respects config reloads)
		cfg := deps.GetConfig()

		// Validate and sanitize the path for security
		validPath, err := validateScanPath(req.Path, &cfg.API.Security)
		if err != nil {
			// Return 403 for access denied, 400 for other validation errors
			statusCode := 400
			if contains(err.Error(), "access denied") {
				statusCode = 403
			}
			c.JSON(statusCode, ErrorResponse{Error: err.Error()})
			return
		}

		// Create context with timeout from config
		timeout := time.Duration(cfg.API.Security.ScanTimeoutSeconds) * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Scan directory - recursive or non-recursive based on request
		scan := scanner.NewScanner(afero.NewOsFs(), &cfg.Matching)
		var result *scanner.ScanResult

		if req.Recursive {
			// Recursive scan with resource limits and optional filter
			// Filter skips directories that don't match, improving performance
			result, err = scan.ScanWithFilter(ctx, validPath, cfg.API.Security.MaxFilesPerScan, req.Filter)
		} else {
			// Non-recursive scan (immediate children only)
			result, err = scan.ScanSingle(validPath)
		}

		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		// Check if scan was limited or timed out (only applicable to recursive scan)
		if result.TimedOut {
			c.JSON(503, ErrorResponse{Error: "scan operation timed out"})
			return
		}

		// Match IDs - use getter for thread-safe access
		matchResults := deps.GetMatcher().Match(result.Files)

		// Validate letter-based multipart patterns using directory context
		// This prevents false positives like ABW-121-C.mp4 (Chinese subtitles) being marked as multipart
		matchResults = matcher.ValidateMultipartInDirectory(matchResults)

		// Build response
		files := make([]FileInfo, 0, len(result.Files))
		matchMap := make(map[string]*matcher.MatchResult)
		for i, match := range matchResults {
			matchMap[match.File.Path] = &matchResults[i]
		}

		for _, fileInfo := range result.Files {
			match, found := matchMap[fileInfo.Path]

			apiFileInfo := FileInfo{
				Name:    fileInfo.Name,
				Path:    fileInfo.Path,
				IsDir:   false,
				Size:    fileInfo.Size,
				ModTime: fileInfo.ModTime.Format("2006-01-02T15:04:05Z07:00"),
				Matched: found,
			}
			if found {
				apiFileInfo.MovieID = match.ID
				apiFileInfo.IsMultiPart = match.IsMultiPart
				apiFileInfo.PartNumber = match.PartNumber
				apiFileInfo.PartSuffix = match.PartSuffix
			}
			files = append(files, apiFileInfo)
		}

		c.JSON(200, ScanResponse{
			Files:   files,
			Count:   len(files),
			Skipped: result.Skipped,
		})
	}
}

// getCurrentWorkingDirectory godoc
// @Summary Get current working directory
// @Description Returns the server's default browse directory (first allowed directory if configured, otherwise current working directory)
// @Tags web
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/cwd [get]
func getCurrentWorkingDirectory(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var defaultPath string

		// Read current config (respects config reloads)
		cfg := deps.GetConfig()

		// Prefer first allowed directory if configured (for Docker environments)
		if len(cfg.API.Security.AllowedDirectories) > 0 {
			defaultPath = cfg.API.Security.AllowedDirectories[0]
		} else {
			// Fall back to current working directory
			cwd, err := os.Getwd()
			if err != nil {
				c.JSON(500, ErrorResponse{Error: err.Error()})
				return
			}
			defaultPath = cwd
		}

		c.JSON(200, gin.H{"path": defaultPath})
	}
}

// browseDirectory godoc
// @Summary Browse directory
// @Description Browse a directory and list its contents
// @Tags web
// @Accept json
// @Produce json
// @Param request body BrowseRequest true "Browse parameters"
// @Success 200 {object} BrowseResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/browse [post]
func browseDirectory(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req BrowseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		// Default to current directory if not specified
		if req.Path == "" {
			req.Path, _ = os.Getwd()
		}

		// Read current config (respects config reloads)
		cfg := deps.GetConfig()

		// Validate and sanitize the path for security
		validPath, err := validateScanPath(req.Path, &cfg.API.Security)
		if err != nil {
			// Return 403 for access denied, 400 for other validation errors
			statusCode := 400
			if contains(err.Error(), "access denied") {
				statusCode = 403
			}
			c.JSON(statusCode, ErrorResponse{Error: err.Error()})
			return
		}

		// Read directory contents
		entries, err := os.ReadDir(validPath)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		// Build response
		items := make([]FileInfo, 0, len(entries))
		for _, entry := range entries {
			fullPath := filepath.Join(validPath, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}

			items = append(items, FileInfo{
				Name:    entry.Name(),
				Path:    fullPath,
				IsDir:   entry.IsDir(),
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
			})
		}

		// Get parent path
		parentPath := filepath.Dir(req.Path)
		if parentPath == req.Path {
			parentPath = "" // Root directory
		}

		c.JSON(200, BrowseResponse{
			CurrentPath: req.Path,
			ParentPath:  parentPath,
			Items:       items,
		})
	}
}

// autocompletePath godoc
// @Summary Autocomplete directory path
// @Description Returns directory suggestions for a partially typed path
// @Tags web
// @Accept json
// @Produce json
// @Param request body PathAutocompleteRequest true "Autocomplete parameters"
// @Success 200 {object} PathAutocompleteResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/browse/autocomplete [post]
func autocompletePath(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req PathAutocompleteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		cfg := deps.GetConfig()
		basePath, fragment, err := resolveAutocompleteBasePath(req.Path, &cfg.API.Security)
		if err != nil {
			statusCode := 400
			if contains(err.Error(), "access denied") {
				statusCode = 403
			}
			c.JSON(statusCode, ErrorResponse{Error: err.Error()})
			return
		}

		entries, err := os.ReadDir(basePath)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		fragmentLower := strings.ToLower(fragment)
		suggestions := make([]PathAutocompleteSuggestion, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			name := entry.Name()
			if fragmentLower != "" && !strings.HasPrefix(strings.ToLower(name), fragmentLower) {
				continue
			}

			suggestions = append(suggestions, PathAutocompleteSuggestion{
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

		c.JSON(200, PathAutocompleteResponse{
			InputPath:   req.Path,
			BasePath:    basePath,
			Suggestions: suggestions,
		})
	}
}

func resolveAutocompleteBasePath(userPath string, cfg *config.SecurityConfig) (string, string, error) {
	trimmedPath := strings.TrimSpace(userPath)
	if trimmedPath == "" {
		return "", "", fmt.Errorf("path is required")
	}

	expandedPath := expandHomeDir(trimmedPath)
	trimmedPath = filepath.Clean(expandedPath)

	absPath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid path")
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

	validBasePath, err := validateScanPath(basePath, cfg)
	if err != nil {
		return "", "", err
	}

	return validBasePath, fragment, nil
}

func hasTrailingPathSeparator(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\")
}
