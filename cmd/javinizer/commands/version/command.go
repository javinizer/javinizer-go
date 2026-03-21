package version

import (
	"fmt"
	"os"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

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
				// Perform a sync check using the service
				ctx := cmd.Context()
				configFile, _ := cmd.Flags().GetString("config")
				cfg, err := loadConfigForCheck(configFile)
				if err != nil {
					return fmt.Errorf("failed to load config: %w", err)
				}

				service := update.NewService(cfg)
				state, err := service.ForceCheck(ctx)

				if state != nil && state.Source == "disabled" {
					_, werr := fmt.Fprintln(cmd.OutOrStdout(), "Update checks are disabled in configuration")
					return werr
				}

				// Check for errors - ForceCheck may return err OR set state.Source to "error"
				if err != nil || state.Source == "error" || state.Version == "" {
					// Print error to stderr
					var errorMsg string
					if err != nil {
						errorMsg = err.Error()
					} else if state.Error != "" {
						errorMsg = state.Error
					} else {
						errorMsg = "Unknown error occurred while checking for updates"
					}
					_, ferr := fmt.Fprintf(cmd.ErrOrStderr(), "Error checking for updates: %v\n", errorMsg)
					if ferr != nil {
						return ferr
					}
					return nil
				}

				current := version.Short()
				latestVer := state.Version

				// Determine if update is available
				updateAvailable := update.CompareVersions(current, latestVer) < 0

				// Print to stdout for easy parsing
				if updateAvailable {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Update available: %s (current: %s)\n", latestVer, current); err != nil {
						return err
					}
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Update available: %s (current: %s)\n", latestVer, current); err != nil {
						return err
					}
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Run 'javinizer update' to update.\n"); err != nil {
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
	return cfg, nil
}
