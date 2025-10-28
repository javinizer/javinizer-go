package api

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
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
func scanDirectory(mat *matcher.Matcher, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ScanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		// Verify path exists
		if _, err := os.Stat(req.Path); os.IsNotExist(err) {
			c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Path does not exist: %s", req.Path)})
			return
		}

		// Scan directory
		scan := scanner.NewScanner(&cfg.Matching)
		result, err := scan.Scan(req.Path)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		// Match IDs
		matchResults := mat.Match(result.Files)

		// Build response
		files := make([]FileInfo, 0, len(result.Files))
		matchMap := make(map[string]*matcher.MatchResult)
		for i, match := range matchResults {
			matchMap[match.File.Path] = &matchResults[i]
		}

		for _, fileInfo := range result.Files {
			match, found := matchMap[fileInfo.Path]
			info, _ := os.Stat(fileInfo.Path)

			apiFileInfo := FileInfo{
				Name:    fileInfo.Name,
				Path:    fileInfo.Path,
				IsDir:   false,
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
				Matched: found,
			}
			if found {
				apiFileInfo.MovieID = match.ID
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
// @Description Returns the server's current working directory
// @Tags web
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/cwd [get]
func getCurrentWorkingDirectory() gin.HandlerFunc {
	return func(c *gin.Context) {
		cwd, err := os.Getwd()
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(200, gin.H{"path": cwd})
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
func browseDirectory() gin.HandlerFunc {
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

		// Verify path exists and is a directory
		info, err := os.Stat(req.Path)
		if os.IsNotExist(err) {
			c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Path does not exist: %s", req.Path)})
			return
		}
		if !info.IsDir() {
			c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Path is not a directory: %s", req.Path)})
			return
		}

		// Read directory contents
		entries, err := os.ReadDir(req.Path)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		// Build response
		items := make([]FileInfo, 0, len(entries))
		for _, entry := range entries {
			fullPath := filepath.Join(req.Path, entry.Name())
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
