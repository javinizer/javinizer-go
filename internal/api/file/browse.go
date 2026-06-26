package file

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/api/core"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// browseDirectory godoc
// @Summary Browse directory
// @Description Browse a directory and list its contents
// @Tags web
// @Accept json
// @Produce json
// @Param request body contracts.BrowseRequest true "Browse parameters"
// @Success 200 {object} contracts.BrowseResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Router /api/v1/browse [post]
func browseDirectory(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.BrowseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if req.Path == "" {
			req.Path, _ = os.Getwd()
		}

		apiCfg := rt.GetAPIConfig()

		dirFile, validPath, err := core.ValidateAndOpenPath(req.Path, apiCfg.SecurityConfig())
		if err != nil {
			apperrors.WriteAPIError(c, err)
			return
		}
		defer func() { _ = dirFile.Close() }()

		// Read directory contents using the open file handle (TOCTOU-safe)
		entries, err := dirFile.ReadDir(-1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// Build response
		items := make([]contracts.FileInfo, 0, len(entries))
		for _, entry := range entries {
			fullPath := filepath.Join(validPath, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}

			items = append(items, contracts.FileInfo{
				Name:    entry.Name(),
				Path:    fullPath,
				IsDir:   entry.IsDir(),
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
			})
		}

		// Get parent path. Derive it from validPath (the directory actually
		// opened) rather than the raw req.Path so current/parent/item paths in
		// the response are all consistent and never mix resolved with stale inputs.
		parentPath := filepath.Dir(validPath)
		if parentPath == validPath {
			parentPath = "" // Root directory
		}

		c.JSON(http.StatusOK, contracts.BrowseResponse{
			CurrentPath: validPath,
			ParentPath:  parentPath,
			Items:       items,
		})
	}
}
