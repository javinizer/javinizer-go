package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	apiauth "github.com/javinizer/javinizer-go/internal/api/auth"
	apicore "github.com/javinizer/javinizer-go/internal/api/core"
	apiserver "github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
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
		Use:     "api",
		Aliases: []string{"web"},
		Short:   "Start the Javinizer API server (web alias: javinizer web)",
		Long:    `Start a REST API server for scraping and retrieving JAV metadata`,
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

// Run executes the API command initialization without starting the server.
// Exported for testing purposes (Epic 7 Story 7.1).
// Returns initialized ServerDependencies for the API server.
func Run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) (*apicore.ServerDependencies, error) {
	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)

	// Override config with flags if provided
	if hostFlag != "" {
		cfg.Server.Host = hostFlag
	}
	if portFlag != 0 {
		cfg.Server.Port = portFlag
	}

	if _, err := config.Prepare(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	logging.Infof("Loaded configuration from %s", configFile)

	// Initialize single-user authentication manager (credentials file next to config).
	authManager, err := apiauth.NewAuthManager(configFile, apiauth.DefaultSessionTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize authentication: %w", err)
	}

	// Ensure data directory exists
	dataDir := filepath.Dir(cfg.Database.DSN)
	if err := os.MkdirAll(dataDir, config.DirPermConfig); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Run startup migrations before repositories are initialized.
	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	logging.Info("Database initialized and migrated")

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)
	actressRepo := database.NewActressRepository(db)
	genreReplacementRepo := database.NewGenreReplacementRepository(db)

	// Initialize scrapers using centralized registry
	registry, err := scraper.NewDefaultScraperRegistry(cfg, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize scraper registry: %w", err)
	}

	logging.Infof("Registered %d scrapers", len(registry.GetAll()))

	// Initialize aggregator
	agg := aggregator.NewWithDatabase(cfg, db)

	// Initialize matcher
	mat, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize matcher: %w", err)
	}

	// Initialize job repository and queue
	jobRepo := database.NewJobRepository(db)
	sharedEngine := template.NewEngine()
	jobQueue := worker.NewJobQueue(jobRepo, cfg.System.TempDir, sharedEngine)

	// Initialize history repository
	historyRepo := database.NewHistoryRepository(db)

	// Initialize new repositories and emitter for history/logging separation
	batchFileOpRepo := database.NewBatchFileOperationRepository(db)
	eventRepo := database.NewEventRepository(db)
	eventEmitter := eventlog.NewEmitter(eventRepo)

	// Initialize reverter for batch-level file revert
	reverter := history.NewReverter(afero.NewOsFs(), batchFileOpRepo)

	// Create server dependencies
	apiDeps := &apicore.ServerDependencies{
		ConfigFile:           configFile,
		Registry:             registry,
		DB:                   db,
		Aggregator:           agg,
		MovieRepo:            movieRepo,
		ActressRepo:          actressRepo,
		HistoryRepo:          historyRepo,
		BatchFileOpRepo:      batchFileOpRepo,
		EventRepo:            eventRepo,
		EventEmitter:         eventEmitter,
		Reverter:             reverter,
		Matcher:              mat,
		JobRepo:              jobRepo,
		JobQueue:             jobQueue,
		Auth:                 authManager,
		TokenStore:           apicore.NewTokenStore(),
		GenreReplacementRepo: genreReplacementRepo,
	}
	// Initialize atomic config pointer
	apiDeps.SetConfig(cfg)

	// Emit server startup event
	if err := eventEmitter.EmitSystemEvent("server", "Javinizer API server initialized", models.SeverityInfo, map[string]interface{}{
		"host": cfg.Server.Host,
		"port": cfg.Server.Port,
	}); err != nil {
		logging.Warnf("Failed to emit server startup event: %v", err)
	}

	return apiDeps, nil
}

func run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) error {
	// Initialize all dependencies using exported Run function
	apiDeps, err := Run(cmd, configFile, hostFlag, portFlag)
	if err != nil {
		log.Fatalf("Failed to initialize API dependencies: %v", err)
	}
	defer func() { _ = apiDeps.DB.Close() }()

	// Create and configure the server
	router := apiserver.NewServer(apiDeps)

	// Log server info
	apiserver.LogServerInfo(apiDeps.GetConfig())

	// Start server (blocking operation)
	currentCfg := apiDeps.GetConfig()
	addr := fmt.Sprintf("%s:%d", currentCfg.Server.Host, currentCfg.Server.Port)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	return nil
}
