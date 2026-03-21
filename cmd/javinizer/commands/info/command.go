package info

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

// NewCommand creates the info command
func NewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show configuration and scraper information",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get config file from persistent flag (set by root command)
			configFile, _ := cmd.Flags().GetString("config")
			return run(cmd, configFile)
		},
	}
}

func run(cmd *cobra.Command, configFile string) error {
	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "=== Javinizer Configuration ==="); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Config file: %s\n", configFile); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Database: %s (%s)\n", cfg.Database.DSN, cfg.Database.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Server: %s:%d\n\n", cfg.Server.Host, cfg.Server.Port); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Scrapers:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Priority: %v\n", cfg.Scrapers.Priority); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - R18.dev: %v\n", cfg.Scrapers.R18Dev.Enabled); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - DMM: %v (scrape_actress: %v)\n\n", cfg.Scrapers.DMM.Enabled, cfg.Scrapers.DMM.ScrapeActress); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Output:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Folder format: %s\n", cfg.Output.FolderFormat); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - File format: %s\n", cfg.Output.FileFormat); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Download cover: %v\n", cfg.Output.DownloadCover); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Download extrafanart: %v\n", cfg.Output.DownloadExtrafanart); err != nil {
		return err
	}

	// Show update status
	if err := printUpdateStatus(cmd, cfg); err != nil {
		return err
	}

	return nil
}

func printUpdateStatus(cmd *cobra.Command, cfg *config.Config) error {
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "\nUpdate:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Current version: %s\n", version.Short()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Update enabled: %v\n", cfg.System.UpdateEnabled); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Check interval: %d hours\n", cfg.System.UpdateCheckIntervalHours); err != nil {
		return err
	}

	// Use update service to get status
	service := update.NewService(cfg)
	ctx := cmd.Context()
	state, err := service.GetStatus(ctx)
	if err != nil {
		_, werr := fmt.Fprintf(cmd.ErrOrStderr(), "  - Error loading status: %v\n", err)
		return werr
	}

	if state.Source == "disabled" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "  - Updates are disabled in config")
		return err
	}

	if state.Source == "none" || state.CheckedAt == "" {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "  - Last checked: never"); err != nil {
			return err
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "  - Latest version: (unknown)")
		return err
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Last checked: %s\n", state.CheckedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Latest version: %s\n", state.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - Update available: %v\n", state.Available); err != nil {
		return err
	}

	// Show prerelease warning if applicable
	if state.Prerelease && !update.IsPrerelease(version.Short()) {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "  - Warning: Latest version is a prerelease"); err != nil {
			return err
		}
	}

	return nil
}
