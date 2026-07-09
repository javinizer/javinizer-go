package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	apiauth "github.com/javinizer/javinizer-go/internal/api/auth"
	apicore "github.com/javinizer/javinizer-go/internal/api/core"
	apiserver "github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/desktop"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/system"
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

// @host localhost:8765
// @BasePath /
// @schemes http https

// NewCommand creates the API server command
func NewCommand() *cobra.Command {
	var (
		host string
		port int
	)

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the Javinizer web/API server",
		Long:  `Start a REST API server for scraping and retrieving JAV metadata`,
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
func Run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) (*apicore.APIDeps, *apicore.APIRuntime, error) {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)

	if hostFlag != "" {
		cfg.Server.Host = hostFlag
	}
	if portFlag != 0 {
		cfg.Server.Port = portFlag
	}

	if _, err := config.Prepare(cfg); err != nil {
		return nil, nil, fmt.Errorf("invalid configuration: %w", err)
	}

	logging.Infof("Loaded configuration from %s", configFile)

	authManager, err := apiauth.NewAuthManager(configFile, apiauth.DefaultSessionTTL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize authentication: %w", err)
	}

	// E2E test mode: disable rate limiting for automated login
	e2eAuth, e2eEnabled := os.LookupEnv("JAVINIZER_E2E_AUTH")
	if e2eEnabled && e2eAuth == "true" {
		authManager.SetDisableRateLimit(true)
	}

	apiDeps, rt, err := apicore.BootstrapAPI(cfg, configFile, authManager)
	if err != nil {
		return nil, nil, err
	}

	authManager.SetApiTokenRepo(apiDeps.Repos.ApiTokenRepo)

	// Classify the running build (docker/cli; desktop is impossible here — the
	// desktop bundle uses internal/desktop.StartServer, not this command) so
	// the /version handler can surface environment-specific upgrade guidance
	// (docker users must `docker pull`; cli users can self-upgrade). Computed
	// once at bootstrap and injected via CoreDeps because the API layer cannot
	// import internal/desktop without an import cycle.
	apiDeps.CoreDeps.SetInstallEnvironment(system.DetectEnvironment(afero.NewOsFs(), desktop.IsDesktopBuild()))

	return apiDeps, rt, nil
}

func run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) error {
	deps, rt, err := Run(cmd, configFile, hostFlag, portFlag)
	if err != nil {
		// Return the error rather than log.Fatalf, which would os.Exit before
		// the deferred cleanup below can run.
		return fmt.Errorf("failed to initialize API dependencies: %w", err)
	}
	defer func() {
		rt.Shutdown()
		_ = deps.CoreDeps.DB.Close()
	}()

	router := apiserver.NewServer(rt)

	apiserver.LogServerInfo(deps.CoreDeps.GetConfig())

	currentCfg := deps.CoreDeps.GetConfig()
	addr := fmt.Sprintf("%s:%d", currentCfg.Server.Host, currentCfg.Server.Port)

	// Bind the server lifetime to the root command's context so a cancellation
	// (e.g. SIGINT handled by cobra) triggers a graceful Shutdown instead of
	// relying on router.Run to return on its own. http.Server.Shutdown lets
	// in-flight requests drain before the deferred rt.Shutdown/DB close run.
	ctx := cmd.Context()
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shut down server: %w", err)
		}
	case err := <-errCh:
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
