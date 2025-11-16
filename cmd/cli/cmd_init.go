package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration and database",
		RunE:  runWithDeps(runInit),
	}
}

func runInit(cmd *cobra.Command, args []string, deps *Dependencies) error {
	fmt.Println("Initializing Javinizer...")

	// Create data directory
	dataDir := filepath.Dir(deps.Config.Database.DSN)
	if err := os.MkdirAll(dataDir, 0777); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	fmt.Printf("✅ Created data directory: %s\n", dataDir)

	// Database is already initialized via deps, just run migrations
	if err := deps.DB.AutoMigrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	fmt.Printf("✅ Initialized database: %s\n", deps.Config.Database.DSN)

	// Save config if it was just created
	if err := config.Save(deps.Config, cfgFile); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Printf("✅ Saved configuration: %s\n", cfgFile)

	fmt.Println("\n🎉 Initialization complete!")
	fmt.Println("\nNext steps:")
	fmt.Println("  - Run 'javinizer scrape IPX-535' to test scraping")
	fmt.Println("  - Run 'javinizer info' to view configuration")

	return nil
}
