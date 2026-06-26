// Command javinizer-e2e boots the real Javinizer API server with a
// deterministic mock scraper substituted at the scraper seam — every other
// subsystem (HTTP middleware, route handlers, worker pool, scrape/apply
// phases, result tracker, SQLite DB, auth, websocket hub) is the production
// code exercised by the real SvelteKit frontend + Playwright full-stack E2E
// tests.
//
// It is the test-only twin of `javinizer api`. Production code paths wire
// all real scrapers (r18dev, dmm, ...) via internal/scraper/registration.go;
// this binary instead injects a registry pre-populated with the single
// "e2emock" scraper registered in internal/scraper/e2emock/. The mock returns
// deterministic metadata per MovieID (see e2emock package docs) so E2E tests
// are stable + offline — no real network scraper calls.
//
// Auth is auto-initialized via JAVINIZER_E2E_AUTH=true with admin /
// adminpassword123 (the standard credentials set by web/frontend/tests/e2e/
// global-setup.ts), and rate-limiting is disabled so the Playwright
// repeated-login loop can iterate without lockouts.
//
// The binary listens on port 18080 by default (JAVINIZER_E2E_PORT env
// override). The Vite dev server's proxy config forwards /api + /ws + /health
// to it; tests point at localhost:5174 (Vite) which transparently proxies to
// this binary's port.
//
// Usage:
//
//	# From repo root
//	go run ./cmd/javinizer-e2e
//	# or with overrides:
//	JAVINIZER_E2E_PORT=19000 go run ./cmd/javinizer-e2e
//
// The process logs the actual listen address to stdout once the HTTP server
// is ready, then serves until killed (SIGTERM/SIGINT).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apiauth "github.com/javinizer/javinizer-go/internal/api/auth"
	apicore "github.com/javinizer/javinizer-go/internal/api/core"
	apiserver "github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraper/e2emock"
	"github.com/javinizer/javinizer-go/internal/scraperutil"

	// Generated swagger docs (side-effect import matching the production
	// `javinizer api` command — keeps the API docs endpoint functional).
	_ "github.com/javinizer/javinizer-go/docs/swagger"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("javinizer-e2e: %v", err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Build an isolated temp DB so repeated test runs don't accumulate.
	// Use a per-process temp file (shared-cache in-memory for speed),
	// closed on exit.
	tmpDir, err := os.MkdirTemp("", "javinizer-e2e-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	db, err := database.New(&database.Config{
		Type: "sqlite",
		// File-backed (under tmpDir) instead of mode=memory&cache=shared.
		// Shared-cache in-memory SQLite is destroyed the moment the last
		// GORM connection cycles (WAL checkpoint / idle timeout), at which
		// point the next connection opens an empty DB and mid-suite requests
		// start returning "no such table". A file in tmpDir survives
		// connection cycling + is removed by the defer RemoveAll below.
		DSN:      "file:" + tmpDir + "/e2e.db?_journal_mode=WAL&_busy_timeout=10000",
		LogLevel: "error",
	})
	if err != nil {
		return fmt.Errorf("open DB: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		return fmt.Errorf("migrate DB: %w", err)
	}

	// Build a scraper registry pre-populated with ONLY the e2emock scraper.
	// Production wires all of r18dev/dmm/javdb/... via scraper.RegisterAll;
	// we substitute the e2emock so tests are offline + deterministic. Every
	// other subsystem (registry finalize, scraper instantiation, scraper
	// priority resolution, instance store, hot-reload paths) is exercised
	// identically to production.
	reg := scraperutil.NewScraperRegistry()
	e2emock.Register(reg)

	// Apply e2emock-only priority to the config + finalize scraper settings
	// (same flow commandutil.NewDependenciesWithOptions would run in
	// production when given no injected registry).
	e2emock.ApplyToConfig(cfg)
	if err := cfg.Scrapers.Finalize(reg); err != nil {
		return fmt.Errorf("finalize scraper config: %w", err)
	}

	// Instantiate scraper instances — populates the instance store so the
	// worker pool's GetInstancesByPriority call returns our e2emock.
	registry, err := scraper.NewDefaultScraperRegistryFrom(reg,
		scraper.ScraperRegistryConfigFromApp(cfg),
		database.NewContentIDMappingRepository(db),
	)
	if err != nil {
		return fmt.Errorf("init scraper registry: %w", err)
	}

	// Bring up the auth manager the same way the production `api` command
	// does — auto-initializes admin/adminpassword123 via JAVINIZER_E2E_AUTH,
	// disables rate limit for repeated Playwright login flows. We pass an
	// empty configFile so the AuthManager uses its default session-path
	// resolution (under the user's home dir or temp), avoiding any dependency
	// on the user's real javinizer config.yaml.
	authManager, err := apiauth.NewAuthManager("", apiauth.DefaultSessionTTL)
	if err != nil {
		return fmt.Errorf("init auth: %w", err)
	}
	authManager.SetDisableRateLimit(true)

	// Bootstrap API deps with the mock-scraper registry + injected DB so
	// commandutil.NewDependenciesWithOptions uses our registry instead of
	// running scraper.RegisterAll + the real-scraper instantiation.
	apiDeps, rt, err := apicore.BootstrapAPIWithOpts(cfg, "", authManager,
		&commandutil.DependenciesOptions{
			DB:              db,
			ScraperRegistry: registry,
		},
	)
	if err != nil {
		return fmt.Errorf("bootstrap API: %w", err)
	}
	authManager.SetApiTokenRepo(apiDeps.Repos.ApiTokenRepo)

	defer rt.Shutdown()

	router := apiserver.NewServer(rt)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	apiserver.LogServerInfo(cfg)

	// Serve with graceful shutdown on TERM/INT — Playwright's webServer will
	// send SIGTERM when tearing down, so the DB closes cleanly + WAL is
	// checkpointed before the temp file is removed.
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()

	fmt.Printf("javinizer-e2e: listening on http://%s\n", addr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server: %w", err)
		}
	case s := <-sig:
		logging.Infof("javinizer-e2e: received %s, shutting down", s)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return nil
}

// loadConfig builds an in-memory config without touching the user's config
// file. Defaults match the production app's `config.DefaultConfig()` plus
// our E2E-specific overrides: a deterministic port, mock-friendly output
// templates, the input directory the E2E tests write sample video files
// into as an allowed directory, and E2E auth enabled.
func loadConfig() (*config.Config, error) {
	cfg := config.DefaultConfig(nil, nil)

	port := 18080
	if envPort := os.Getenv("JAVINIZER_E2E_PORT"); envPort != "" {
		var p int
		if _, err := fmt.Sscanf(envPort, "%d", &p); err == nil && p > 0 {
			port = p
		}
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Database.DSN = ":memory:" // overridden via injected DB in run()
	cfg.Logging = config.LoggingConfig{Level: "error"}

	// Output templates — keep them simple + deterministic so organize-preview
	// paths are predictable in assertions. Bug 6ed5d0e5's symptom is that
	// the organize preview video row renders `<ID>` with no `.mp4` — the
	// template `<ID>` (no template function) is what surfaces it because the
	// organizer appends match.Extension. With our template, the preview
	// response's file_name should be the movie ID plus the `.mp4` extension
	// when the scrape phase preserves match.Extension.
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Operation.RenameFile = true
	cfg.Output.MediaFormat.PosterFormat = "<ID>-poster.jpg"
	cfg.Output.MediaFormat.FanartFormat = "<ID>-fanart.jpg"
	cfg.Output.MediaFormat.ScreenshotFolder = "extrafanart"

	// Default the matcher to regex mode so JAV-style IDs in test filenames
	// (GOOD-001, FAIL-001, MULTI-001-pt1) parse to a MovieID even though the
	// scanner finds no real sibling files. Bug 83fba0c5's fix depended on a
	// non-empty MovieID being derived from the filename via the matcher
	// fallback, which requires regex-enabled matching.
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = `(?i)([a-z]{2,10}-?\d{2,5}[a-z]?)`
	cfg.Matching.Extensions = []string{".mp4", ".mkv"}

	// Allow the test fixture directories only — keeps the security validator
	// happy for any path under them. Input dir = where the test sample video
	// files live. Output dir = where organize preview / organize apply
	// write target paths (preview validates destination against this list).
	inputDir := os.Getenv("JAVINIZER_E2E_INPUT_DIR")
	if inputDir == "" {
		inputDir = "/tmp/javinizer-e2e-input"
	}
	if err := os.MkdirAll(inputDir, 0o750); err != nil {
		return nil, fmt.Errorf("create e2e input dir %q: %w", inputDir, err)
	}
	outputDir := os.Getenv("JAVINIZER_E2E_OUTPUT_DIR")
	if outputDir == "" {
		outputDir = "/tmp/javinizer-e2e-output"
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, fmt.Errorf("create e2e output dir %q: %w", outputDir, err)
	}
	cfg.API.Security.AllowedDirectories = []string{inputDir, outputDir}
	cfg.API.Security.DeniedDirectories = nil
	// Disable the IPRateLimiter middleware entirely (RPM=0 skips
	// middleware construction in registerAPIV1Routes). The full-stack
	// Playwright suite issues many rapid GET /batch/:id polls per test —
	// a default per-IP limit would 429 + flake specs that exercise
	// long-running scrape/apply jobs.
	cfg.API.Security.RateLimit.RequestsPerMinute = 0

	// Performance: low worker count keeps the test pipe snappy and avoids
	// goroutine leak races during teardown.
	cfg.Performance.MaxWorkers = 4
	cfg.Performance.WorkerTimeout = 30

	// Auto-init auth for the E2E flow — admin/adminpassword123 is the
	// standard the existing web/frontend/tests/e2e/global-setup.ts expects.
	if err := os.Setenv("JAVINIZER_E2E_AUTH", "true"); err != nil {
		return nil, fmt.Errorf("set JAVINIZER_E2E_AUTH: %w", err)
	}
	_ = os.Setenv("JAVINIZER_E2E_USERNAME", "admin")
	_ = os.Setenv("JAVINIZER_E2E_PASSWORD", "adminpassword123")

	if _, err := config.Prepare(cfg); err != nil {
		return nil, fmt.Errorf("prepare config: %w", err)
	}
	return cfg, nil
}
