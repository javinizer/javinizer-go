package api

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/api"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/cobra"

	_ "github.com/javinizer/javinizer-go/docs/swagger" // Import generated docs
)

// @title Javinizer API
// @version 1.0
// @description REST API for JAV metadata scraping and file organization
// @termsOfService https://github.com/javinizer/javinizer-go

// @contact.name API Support
// @contact.url https://github.com/javinizer/javinizer-go/issues

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:8080
// @BasePath /
// @schemes http https

// NewCommand creates the API server command
func NewCommand() *cobra.Command {
	var (
		host string
		port int
	)

	cmd := &cobra.Command{
		Use:   "api",
		Short: "Start the Javinizer API server",
		Long:  `Start a REST API server for scraping and retrieving JAV metadata`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get config file from persistent flag (set by root command)
			configFile, _ := cmd.Flags().GetString("config")
			return run(cmd, configFile, host, port)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "Server host address (default from config)")
	cmd.Flags().IntVar(&port, "port", 0, "Server port (default from config)")

	return cmd
}

func run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) error {
	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override config with flags if provided
	if hostFlag != "" {
		cfg.Server.Host = hostFlag
	}
	if portFlag != 0 {
		cfg.Server.Port = portFlag
	}

	logging.Infof("Loaded configuration from %s", configFile)

	// Ensure data directory exists
	dataDir := filepath.Dir(cfg.Database.DSN)
	if err := os.MkdirAll(dataDir, 0777); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Cleanup temp posters from previous sessions
	// Temp posters are ephemeral and tied to batch job lifecycle
	// Since batch jobs don't persist across restarts, cleanup all temp posters on startup
	tempPosterDir := filepath.Join("data", "temp", "posters")
	if err := os.RemoveAll(tempPosterDir); err != nil {
		logging.Warnf("Failed to clean temp poster directory on startup: %v", err)
	} else {
		logging.Info("Cleaned temp poster directory from previous sessions")
	}

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := db.AutoMigrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	logging.Info("Database initialized and migrated")

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)
	actressRepo := database.NewActressRepository(db)
	contentIDRepo := database.NewContentIDMappingRepository(db)

	// Initialize scrapers
	registry := models.NewScraperRegistry()
	registry.Register(r18dev.New(cfg))
	registry.Register(dmm.New(cfg, contentIDRepo))

	logging.Infof("Registered %d scrapers", len(registry.GetAll()))

	// Initialize aggregator
	agg := aggregator.NewWithDatabase(cfg, db)

	// Initialize matcher
	mat, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		log.Fatalf("Failed to initialize matcher: %v", err)
	}

	// Initialize job queue
	jobQueue := worker.NewJobQueue()

	// Create server dependencies
	apiDeps := &api.ServerDependencies{
		ConfigFile:  configFile,
		Registry:    registry,
		DB:          db,
		Aggregator:  agg,
		MovieRepo:   movieRepo,
		ActressRepo: actressRepo,
		Matcher:     mat,
		JobQueue:    jobQueue,
	}
	// Initialize atomic config pointer
	apiDeps.SetConfig(cfg)

	// Create and configure the server
	router := api.NewServer(apiDeps)

	// Log server info
	api.LogServerInfo(apiDeps.GetConfig())

	// Start server
	currentCfg := apiDeps.GetConfig()
	addr := fmt.Sprintf("%s:%d", currentCfg.Server.Host, currentCfg.Server.Port)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	return nil
}
