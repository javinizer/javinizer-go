package version

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/javinizer/javinizer-go/internal/version"
)

// VersionStatusResponse represents the response for version status endpoints.
type VersionStatusResponse struct {
	Current         string `json:"current"`          // Current installed version
	Commit          string `json:"commit"`           // Current commit hash
	BuildDate       string `json:"build_date"`       // Build timestamp
	Latest          string `json:"latest"`           // Latest available version
	UpdateAvailable bool   `json:"update_available"` // Whether an update is available
	Prerelease      bool   `json:"prerelease"`       // Whether latest is a prerelease
	CheckedAt       string `json:"checked_at"`       // When the check was performed
	Source          string `json:"source"`           // "cached" or "fresh"
	Error           string `json:"error,omitempty"`  // Error message if any
}

// versionStatus godoc
// @Summary Get version status
// @Description Get the current version and check if an update is available. Returns cached status unless explicitly refreshed.
// @Tags system
// @Produce json
// @Success 200 {object} VersionStatusResponse
// @Router /api/v1/version [get]
func versionStatus(deps commandutil.CoreDepsReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create update service from narrow config.
		cfg := deps.GetConfig()
		service := update.NewService(update.UpdateConfig{
			Enabled:                   cfg.System.VersionCheckEnabled,
			VersionCheckIntervalHours: cfg.System.VersionCheckIntervalHours,
		})

		// Get current version info
		currentVer := version.Short()
		commit := version.Commit
		buildDate := version.BuildDate

		// Load cached state using service
		state, err := service.GetStatus(c.Request.Context())

		response := &VersionStatusResponse{
			Current:   currentVer,
			Commit:    commit,
			BuildDate: buildDate,
			Source:    string(update.UpdateSourceCached),
		}

		if err != nil {
			response.Error = err.Error()
			c.JSON(http.StatusOK, response)
			return
		}

		// Handle disabled state
		if state.Source == update.UpdateSourceDisabled {
			response.Latest = ""
			response.UpdateAvailable = false
			response.CheckedAt = ""
			response.Source = string(update.UpdateSourceDisabled)
			c.JSON(http.StatusOK, response)
			return
		}

		// Handle none/empty state
		if state.Source == update.UpdateSourceNone || state.CheckedAt == "" {
			response.Latest = ""
			response.UpdateAvailable = false
			response.CheckedAt = ""
			response.Source = string(update.UpdateSourceNone)
			c.JSON(http.StatusOK, response)
			return
		}

		// Fill in state data
		response.Latest = state.Version
		response.UpdateAvailable = state.Available
		response.Prerelease = state.Prerelease
		response.CheckedAt = state.CheckedAt
		response.Source = string(state.Source)

		if state.Error != "" {
			response.Error = state.Error
		}

		c.JSON(http.StatusOK, response)
	}
}

// versionCheck godoc
// @Summary Force version check
// @Description Force a check for the latest version and update the cache.
// @Tags system
// @Produce json
// @Success 200 {object} VersionStatusResponse
// @Router /api/v1/version/check [post]
func versionCheck(deps commandutil.CoreDepsReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create update service from narrow config.
		cfg := deps.GetConfig()
		service := update.NewService(update.UpdateConfig{
			Enabled:                   cfg.System.VersionCheckEnabled,
			VersionCheckIntervalHours: cfg.System.VersionCheckIntervalHours,
		})

		// Perform the check (sync)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		state, err := service.ForceCheck(ctx)

		response := &VersionStatusResponse{
			Current:    version.Short(),
			Commit:     version.Commit,
			BuildDate:  version.BuildDate,
			Latest:     "",
			Prerelease: false,
		}

		if err != nil {
			response.Source = string(update.UpdateSourceError)
			response.Error = err.Error()
			response.Latest = ""
			response.UpdateAvailable = false
			c.JSON(http.StatusOK, response)
			return
		}

		if state == nil {
			response.Source = string(update.UpdateSourceError)
			response.Error = "update check returned no state"
			response.Latest = ""
			response.UpdateAvailable = false
			c.JSON(http.StatusOK, response)
			return
		}

		response.Source = string(state.Source)
		response.Error = state.Error
		response.Latest = state.Version
		response.Prerelease = state.Prerelease
		response.UpdateAvailable = state.Available
		response.CheckedAt = state.CheckedAt

		c.JSON(http.StatusOK, response)
	}
}
