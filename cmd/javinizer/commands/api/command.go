package api

import (
	"fmt"
	"log"
	"os"

	apiauth "github.com/javinizer/javinizer-go/internal/api/auth"
	apicore "github.com/javinizer/javinizer-go/internal/api/core"
	apiserver "github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
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
// Returns initialized APIDeps for the API server.
func Run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) (*apicore.APIDeps, error) {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)

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

	authManager, err := apiauth.NewAuthManager(configFile, apiauth.DefaultSessionTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize authentication: %w", err)
	}

	// E2E test mode: disable rate limiting for automated login
	e2eAuth, e2eEnabled := os.LookupEnv("JAVINIZER_E2E_AUTH")
	if e2eEnabled && e2eAuth == "true" {
		authManager.SetDisableRateLimit(true)
	}

	apiDeps, err := apicore.BootstrapAPI(cfg, configFile, authManager)
	if err != nil {
		return nil, err
	}

	authManager.SetApiTokenRepo(apiDeps.Repos.ApiTokenRepo)

	return apiDeps, nil
}

func run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) error {
	deps, err := Run(cmd, configFile, hostFlag, portFlag)
	if err != nil {
		log.Fatalf("Failed to initialize API dependencies: %v", err)
	}
	defer func() {
		rt := deps.GetRuntime()
		if rt == nil {
			rt = apicore.NewAPIRuntime(deps)
		}
		rt.Shutdown()
		_ = deps.CoreDeps.DB.Close()
	}()

	router := apiserver.NewServer(deps)

	apiserver.LogServerInfo(deps.CoreDeps.GetConfig())

	currentCfg := deps.CoreDeps.GetConfig()
	addr := fmt.Sprintf("%s:%d", currentCfg.Server.Host, currentCfg.Server.Port)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	return nil
}
