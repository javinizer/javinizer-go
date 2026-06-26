package file

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/workflow"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// scanDirectory godoc
// @Summary Scan directory for video files
// @Description Scan a directory for video files and match JAV IDs
// @Tags web
// @Accept json
// @Produce json
// @Param request body contracts.ScanRequest true "Scan parameters"
// @Success 200 {object} contracts.ScanResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/scan [post]
func scanDirectory(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.ScanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		apiCfg := rt.GetAPIConfig()
		scanCfg := apiCfg.ScannerConfig()

		dirFile, validPath, err := core.ValidateAndOpenPath(req.Path, apiCfg.SecurityConfig())
		if err != nil {
			apperrors.WriteAPIError(c, err)
			return
		}
		defer func() { _ = dirFile.Close() }()

		wf, wfErr := rt.GetScanOnlyWorkflow()
		if wfErr != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: wfErr.Error()})
			return
		}

		// Execute scan + match via the seam
		scanResult, err := wf.ScanAndMatch(c.Request.Context(), workflow.ScanAndMatchCmd{
			Directory:      validPath,
			Recursive:      req.Recursive,
			TimeoutSeconds: scanCfg.ScanTimeoutSeconds,
			MaxFiles:       scanCfg.MaxFilesPerScan,
			Filter:         req.Filter,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// Check if scan was limited or timed out
		if scanResult.TimedOut {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "scan operation timed out"})
			return
		}

		// Build response from seam result
		files := make([]contracts.FileInfo, 0, len(scanResult.Files))
		for _, fmi := range scanResult.Files {
			apiFileInfo := contracts.FileInfo{
				Name:    fmi.Name,
				Path:    fmi.Path,
				IsDir:   false,
				Size:    fmi.Size,
				ModTime: fmi.ModTime.Format("2006-01-02T15:04:05Z07:00"),
				Matched: fmi.MovieID != "",
			}
			// Multipart metadata describes the file itself, not whether it matched
			// a movie — preserve IsMultiPart/PartNumber/PartSuffix for unmatched
			// files too, so scan-result metadata stays accurate. MovieID is gated
			// since it only applies when a match exists.
			apiFileInfo.IsMultiPart = fmi.IsMultiPart
			apiFileInfo.PartNumber = fmi.PartNumber
			apiFileInfo.PartSuffix = fmi.PartSuffix
			if fmi.MovieID != "" {
				apiFileInfo.MovieID = fmi.MovieID
			}
			files = append(files, apiFileInfo)
		}

		c.JSON(http.StatusOK, contracts.ScanResponse{
			Files:   files,
			Count:   len(files),
			Skipped: scanResult.SkippedPaths,
		})
	}
}

func getCurrentWorkingDirectory(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var defaultPath string

		apiCfg := rt.GetAPIConfig()
		scanCfg := apiCfg.ScannerConfig()

		if len(scanCfg.AllowedDirectories) > 0 {
			defaultPath = scanCfg.AllowedDirectories[0]
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
				return
			}
			defaultPath = cwd
		}

		c.JSON(http.StatusOK, gin.H{"path": defaultPath})
	}
}
