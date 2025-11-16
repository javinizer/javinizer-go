package info

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
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

	fmt.Println("=== Javinizer Configuration ===")
	fmt.Printf("Config file: %s\n", configFile)
	fmt.Printf("Database: %s (%s)\n", cfg.Database.DSN, cfg.Database.Type)
	fmt.Printf("Server: %s:%d\n\n", cfg.Server.Host, cfg.Server.Port)

	fmt.Println("Scrapers:")
	fmt.Printf("  Priority: %v\n", cfg.Scrapers.Priority)
	fmt.Printf("  - R18.dev: %v\n", cfg.Scrapers.R18Dev.Enabled)
	fmt.Printf("  - DMM: %v (scrape_actress: %v)\n\n", cfg.Scrapers.DMM.Enabled, cfg.Scrapers.DMM.ScrapeActress)

	fmt.Println("Output:")
	fmt.Printf("  - Folder format: %s\n", cfg.Output.FolderFormat)
	fmt.Printf("  - File format: %s\n", cfg.Output.FileFormat)
	fmt.Printf("  - Download cover: %v\n", cfg.Output.DownloadCover)
	fmt.Printf("  - Download extrafanart: %v\n", cfg.Output.DownloadExtrafanart)

	return nil
}
