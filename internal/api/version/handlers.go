package version

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/system"
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
	// InstallEnvironment reports how javinizer is running ("docker", "desktop",
	// or "cli") so the UI can render the right upgrade path: docker images can't
	// self-swap (read-only image), desktop apps need a new bundle, only cli
	// builds self-upgrade in place.
	InstallEnvironment string `json:"install_environment"`
	// UpgradeInstructions carries environment-specific guidance verbatim (e.g.
	// the `docker pull` command for docker, the releases URL for desktop, the
	// `javinizer upgrade` command for cli) so the frontend doesn't have to
	// hardcode the image ref or rebuild steps per environment.
	UpgradeInstructions string `json:"upgrade_instructions,omitempty"`
	// UpgradeCommands breaks the guidance into discrete, paste-ready command
	// lines per install method (cli_binary / homebrew / scoop / docker_pull /
	// docker_compose) so the UI can render one copy button per command instead
	// of copying the prose blob. Homebrew is only listed on darwin/linux hosts
	// and Scoop on windows hosts (no OS offers both). Empty for desktop
	// (in-app upgrade only).
	UpgradeCommands []system.UpgradeCommand `json:"upgrade_commands,omitempty"`
	Error           string                  `json:"error,omitempty"` // Error message if any
}

// Test seams: os.Executable's failure mode is unreachable without them, and
// EvalSymlinks' error fallback likewise — leaving those branches uncovered
// costs codecov/patch points on every version-handler change.
var (
	osExecutable = os.Executable
	evalSymlinks = filepath.EvalSymlinks
)

// detectInstallMethod resolves HOW this binary was installed (the resolved
// executable path lands inside Homebrew's Cellar / Scoop's apps dir for
// package-managed installs). The prerelease hand-off to brew/scoop cannot
// deliver prereleases, so command generation needs the method, not just the
// OS.
func detectInstallMethod() string {
	exe, err := osExecutable()
	if err != nil {
		return "manual"
	}
	if resolved, rerr := evalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	return update.DetectInstallMethod(exe).String()
}

// applyEnvironment stamps the response with the detected install environment
// and its upgrade instructions. Called on every response path so the UI always
// knows whether to offer `docker pull`, a releases link, or `javinizer upgrade`.
// The environment is computed once at bootstrap (where desktop.IsDesktopBuild()
// is reachable without an import cycle) and injected via CoreDeps.
func applyEnvironment(response *VersionStatusResponse, env system.Environment) {
	response.InstallEnvironment = string(env)
	response.UpgradeInstructions = system.UpgradeInstructions(env)
	// Baseline command set (stable release); handlers whose state path learns
	// the announced release re-stamp this with the real prerelease flag.
	response.UpgradeCommands = system.UpgradeCommands(env, runtime.GOOS, false, "", detectInstallMethod())
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
			StableOnly:                cfg.System.VersionCheckStableOnly,
		})

		// Get current version info
		currentVer := version.Short()
		commit := version.Commit
		buildDate := version.BuildDate

		// Load cached state using service
		state, err := service.GetStatus(c.Request.Context())

		env := deps.InstallEnvironment()
		response := &VersionStatusResponse{
			Current:   currentVer,
			Commit:    commit,
			BuildDate: buildDate,
			Source:    string(update.UpdateSourceCached),
		}
		applyEnvironment(response, env)

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

		// Regenerate commands once the announced release is known: a
		// prerelease swaps the CLI row to `javinizer upgrade --prerelease`
		// and drops the stable-only package-manager rows.
		response.UpgradeCommands = system.UpgradeCommands(env, runtime.GOOS, response.Prerelease, response.Latest, detectInstallMethod())

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
			StableOnly:                cfg.System.VersionCheckStableOnly,
		})

		// Perform the check (sync)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		state, err := service.ForceCheck(ctx)

		env := deps.InstallEnvironment()
		response := &VersionStatusResponse{
			Current:    version.Short(),
			Commit:     version.Commit,
			BuildDate:  version.BuildDate,
			Latest:     "",
			Prerelease: false,
		}
		applyEnvironment(response, env)

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

		response.UpgradeCommands = system.UpgradeCommands(env, runtime.GOOS, response.Prerelease, response.Latest, detectInstallMethod())
		c.JSON(http.StatusOK, response)
	}
}
