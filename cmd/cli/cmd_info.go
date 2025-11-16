package main

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
)

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show configuration and scraper information",
		RunE:  runWithConfig(runInfo),
	}
}

func runInfo(cmd *cobra.Command, args []string, cfg *config.Config) error {
	fmt.Println("=== Javinizer Configuration ===")
	fmt.Printf("Config file: %s\n", cfgFile)
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
