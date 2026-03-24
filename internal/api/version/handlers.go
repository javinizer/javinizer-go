package version

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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

// VersionCheckRequest represents the request body for force version check.
// Note: This struct is currently unused - the POST /api/v1/version/check endpoint
// accepts JSON but doesn't use any fields. Kept for potential future use.
type VersionCheckRequest struct {
	// Force refresh the cache
	ForceRefresh bool `json:"force_refresh,omitempty"`
}

// versionStatus godoc
// @Summary Get version status
// @Description Get the current version and check if an update is available. Returns cached status unless explicitly refreshed.
// @Tags system
// @Produce json
// @Success 200 {object} VersionStatusResponse
// @Router /api/v1/version [get]
func versionStatus(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create update service
		cfg := deps.GetConfig()
		service := update.NewService(cfg)

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
			Source:    "cached",
		}

		if err != nil {
			response.Error = err.Error()
			c.JSON(http.StatusOK, response)
			return
		}

		// Handle disabled state
		if state.Source == "disabled" {
			response.Latest = ""
			response.UpdateAvailable = false
			response.CheckedAt = ""
			response.Source = "disabled"
			c.JSON(http.StatusOK, response)
			return
		}

		// Handle none/empty state
		if state.Source == "none" || state.CheckedAt == "" {
			response.Latest = ""
			response.UpdateAvailable = false
			response.CheckedAt = ""
			response.Source = "none"
			c.JSON(http.StatusOK, response)
			return
		}

		// Fill in state data
		response.Latest = state.Version
		response.UpdateAvailable = state.Available
		response.Prerelease = state.Prerelease
		response.CheckedAt = state.CheckedAt
		response.Source = state.Source

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
func versionCheck(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create update service
		cfg := deps.GetConfig()
		service := update.NewService(cfg)

		// Perform the check (sync)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		state, err := service.ForceCheck(ctx)

		response := &VersionStatusResponse{
			Current:    version.Short(),
			Commit:     version.Commit,
			BuildDate:  version.BuildDate,
			Source:     state.Source,
			Error:      state.Error,
			Latest:     "",
			Prerelease: false,
		}

		if err != nil {
			response.Error = err.Error()
			response.Latest = ""
			response.UpdateAvailable = false
			c.JSON(http.StatusOK, response)
			return
		}

		response.Latest = state.Version
		response.Prerelease = state.Prerelease
		response.UpdateAvailable = state.Available
		response.CheckedAt = state.CheckedAt

		c.JSON(http.StatusOK, response)
	}
}
