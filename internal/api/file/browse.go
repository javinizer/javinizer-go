package file

import (
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

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
