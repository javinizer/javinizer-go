package file

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/workflow"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// osGetwd is a seam over os.Getwd so the root-CWD branch in defaultPath can
// be exercised without actually chdir-ing the test process into "/".
var (
	osGetwd       = os.Getwd
	osUserHomeDir = os.UserHomeDir
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

		// Take one consistent snapshot so the APIConfig (security/scanner settings)
		// and the scan-only workflow come from the same reload epoch. Reading them
		// via separate accessors could mix old/new state if a config reload lands
		// between the calls (issue #44).
		snap := rt.Snapshot()
		apiCfg := snap.APIConfig()
		scanCfg := apiCfg.ScannerConfig()

		dirFile, validPath, err := core.ValidateAndOpenPath(req.Path, apiCfg.SecurityConfig())
		if err != nil {
			apperrors.WriteAPIError(c, err)
			return
		}
		defer func() { _ = dirFile.Close() }()

		wf, wfErr := snap.ScanOnlyWorkflow()
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
		path, err := defaultPath(rt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"path": path})
	}
}

// defaultPath resolves a sensible absolute default path for the UI to
// pre-fill browse/scan inputs with. It prefers the first configured allowed
// directory; otherwise it uses the process working directory. When the CWD
// is root-like ("/" or a Windows drive root) — as it is for the desktop app
// launched from Finder/Explorer — it returns an empty string so the UI does
// not pre-fill a useless "/".
func defaultPath(rt *core.APIRuntime) (string, error) {
	apiCfg := rt.GetAPIConfig()
	scanCfg := apiCfg.ScannerConfig()

	if len(scanCfg.AllowedDirectories) > 0 {
		return scanCfg.AllowedDirectories[0], nil
	}

	cwd, err := osGetwd()
	if err != nil {
		return "", err
	}
	if isRootPath(cwd) {
		return "", nil
	}
	if isHomeDirectory(cwd) {
		return "", nil
	}
	return cwd, nil
}

// isRootPath reports whether path is a filesystem root ("/" on Unix, "C:\"
// style drive roots on Windows). Such paths are useless as a library default.
func isRootPath(path string) bool {
	if path == "/" || path == string(filepath.Separator) {
		return true
	}
	if len(path) == 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return false
}

// isHomeDirectory reports whether path equals the user's home directory.
// Prefilling the home directory grants file-operation scope over the entire
// profile, so it is treated as unusable (same fail-closed behavior as root).
func isHomeDirectory(path string) bool {
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		return true
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		resolvedHome = home
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolvedPath = path
	}
	return resolvedPath == resolvedHome
}
