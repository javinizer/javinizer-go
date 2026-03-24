package file

import (
	"context"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
)

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
		validPath, err := core.ValidateScanPath(req.Path, &cfg.API.Security)
		if err != nil {
			// Return 403 for access denied, 400 for other validation errors
			statusCode := 400
			if core.Contains(err.Error(), "access denied") {
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
// @Description Returns the server's default browse directory. For Docker deployments, returns the first allowed directory (typically /media). For manual deployments, returns current working directory if no allowed directories configured.
// @Tags web
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 500 {object} ErrorResponse
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
