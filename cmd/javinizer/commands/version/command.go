package version

import (
	"context"
	"fmt"
	"os"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

// updateChecker, when non-nil, is injected into the update service so tests
// can drive the --check error branches deterministically without network or a
// shared on-disk cache. Production leaves it nil (NewService is used).
var updateChecker update.Checker

// updateStatePath, when non-empty, overrides the on-disk cache location for the
// update service. Tests set it to a temp dir so no stale cached state masks a
// checker failure. Production leaves it empty (the real data dir is used).
var updateStatePath string

// runVersionCheck performs a sync update check and translates the service's
// internal state into the simple outcome the command's output branches need.
// It is a package-level variable so tests can swap it for a stub, covering the
// --check output paths (available / up-to-date / disabled / error) without
// touching the network — mirroring the upgrade command's SetRunUpgrade seam.
var runVersionCheck = func(ctx context.Context, configFile string) (*checkOutcome, error) {
	cfg, err := loadConfigForCheck(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ucfg := update.UpdateConfig{
		Enabled:                   cfg.System.VersionCheckEnabled,
		VersionCheckIntervalHours: cfg.System.VersionCheckIntervalHours,
		StableOnly:                cfg.System.VersionCheckStableOnly,
	}
	var service *update.Service
	if updateChecker != nil {
		service = update.NewServiceWithOptions(ucfg, update.ServiceOptions{Checker: updateChecker, StatePath: updateStatePath})
	} else {
		service = update.NewService(ucfg)
	}
	state, _ := service.ForceCheck(ctx)

	if state != nil && state.Source == update.UpdateSourceDisabled {
		return &checkOutcome{disabled: true}, nil
	}

	// ForceCheck translates every failure into the state's Source/Error fields
	// (it never returns a non-nil Go error), so branch on the state, not err.
	if state == nil || state.Version == "" || state.Source == update.UpdateSourceError {
		var errorMsg string
		if state != nil && state.Error != "" {
			errorMsg = state.Error
		} else {
			errorMsg = "Unknown error occurred while checking for updates"
		}
		return &checkOutcome{errMsg: errorMsg}, nil
	}

	return &checkOutcome{available: state.Available, version: state.Version}, nil
}

// checkOutcome is the result of a --check invocation, decoupling the command's
// output branches from the update package's internal state type so tests can
// drive every branch without constructing internal types.
type checkOutcome struct {
	disabled  bool
	available bool
	version   string
	errMsg    string
}

// SetRunVersionCheck swaps the version-check implementation and returns the
// previous one, so tests can inject a stub and restore it afterwards.
func SetRunVersionCheck(fn func(ctx context.Context, configFile string) (*checkOutcome, error)) func(context.Context, string) (*checkOutcome, error) {
	prev := runVersionCheck
	runVersionCheck = fn
	return prev
}

// NewCommand creates the version command.
func NewCommand() *cobra.Command {
	var short bool
	var check bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show build and release version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if check {
				configFile, _ := cmd.Flags().GetString("config")
				outcome, err := runVersionCheck(cmd.Context(), configFile)
				if err != nil {
					return err
				}

				if outcome.disabled {
					_, werr := fmt.Fprintln(cmd.OutOrStdout(), "Update checks are disabled in configuration")
					return werr
				}

				if outcome.errMsg != "" {
					_, ferr := fmt.Fprintf(cmd.ErrOrStderr(), "Error checking for updates: %v\n", outcome.errMsg)
					if ferr != nil {
						return ferr
					}
					return nil
				}

				current := version.Short()
				latestVer := outcome.version

				// outcome.available already reflects the build version comparison AND
				// the user's version_check_stable_only policy (prerelease
				// suppression). Reusing it keeps the CLI consistent with the API/UI
				// — recomputing CompareVersions here would surface a prerelease the
				// service deliberately suppressed.
				if outcome.available {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Update available: %s (current: %s)\n", latestVer, current); err != nil {
						return err
					}
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Update available: %s (current: %s)\n", latestVer, current); err != nil {
						return err
					}
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Run 'javinizer upgrade' to update.\n"); err != nil {
						return err
					}
				} else {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "You are running the latest version: %s\n", current); err != nil {
						return err
					}
				}

				return nil
			}

			if short {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Short())
				return err
			}

			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Info())
			return err
		},
	}

	cmd.Flags().BoolVarP(&short, "short", "s", false, "show only the short version")
	cmd.Flags().BoolVarP(&check, "check", "c", false, "check for updates")
	return cmd
}

func loadConfigForCheck(configFile string) (*config.Config, error) {
	// Mirror root command behavior: JAVINIZER_CONFIG overrides --config.
	if envConfig := os.Getenv("JAVINIZER_CONFIG"); envConfig != "" {
		configFile = envConfig
	}

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, err
	}

	config.ApplyEnvironmentOverrides(cfg)
	if _, err := config.Prepare(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
