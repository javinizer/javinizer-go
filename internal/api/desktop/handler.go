package desktop

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/system"
	"github.com/javinizer/javinizer-go/internal/updater"
)

type upgradeRequest struct {
	Force bool `json:"force"`
}

type upgradeResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// upgrade godoc
// @Summary Upgrade the desktop bundle
// @Description Download, verify, and stage the latest desktop bundle, then relaunch the app. Desktop builds only.
// @Tags desktop
// @Accept json
// @Produce json
// @Param body body upgradeRequest false "Upgrade options (force re-download even when up to date)"
// @Success 200 {object} upgradeResponse
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/desktop/upgrade [post]
func upgrade(deps commandutil.CoreDepsReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.InstallEnvironment() != system.EnvironmentDesktop {
			c.JSON(http.StatusNotFound, gin.H{"error": "desktop self-upgrade is not available in this environment"})
			return
		}
		u := deps.BundleUpdater()
		if u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "desktop self-upgrade is not available"})
			return
		}

		status := u.Status()
		if status.State != updater.StateIdle && status.State != updater.StateFailed {
			c.JSON(http.StatusConflict, gin.H{"error": "a bundle upgrade is already in progress", "state": status.State})
			return
		}

		var req upgradeRequest
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}

		result, err := u.Upgrade(context.Background(), updater.UpgradeOptions{Force: req.Force})
		if err != nil {
			if errors.Is(err, updater.ErrAlreadyInProgress) {
				c.JSON(http.StatusConflict, gin.H{"error": "a bundle upgrade is already in progress"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if result != nil && result.UpToDate {
			c.JSON(http.StatusOK, upgradeResponse{Status: "up-to-date", Version: result.LatestVersion})
			return
		}

		version := ""
		if result != nil {
			version = result.LatestVersion
		}
		c.JSON(http.StatusOK, upgradeResponse{Status: "relaunching", Version: version})
		c.Writer.Flush()
		go func() {
			_ = u.Relaunch(context.Background())
		}()
	}
}

// upgradeStatus godoc
// @Summary Get desktop upgrade status
// @Description Return the current desktop bundle upgrade state (idle/downloading/verifying/staging/swapping/relaunching/failed). Desktop builds only.
// @Tags desktop
// @Produce json
// @Success 200 {object} updater.Status
// @Failure 404 {object} map[string]string
// @Router /api/v1/desktop/upgrade/status [get]
func upgradeStatus(deps commandutil.CoreDepsReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.InstallEnvironment() != system.EnvironmentDesktop {
			c.JSON(http.StatusNotFound, gin.H{"error": "desktop self-upgrade is not available in this environment"})
			return
		}
		u := deps.BundleUpdater()
		if u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "desktop self-upgrade is not available"})
			return
		}
		c.JSON(http.StatusOK, u.Status())
	}
}
