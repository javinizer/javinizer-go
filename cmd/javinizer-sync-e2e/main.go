// Command javinizer-sync-e2e is a disposable host for real offline sync APIs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	_ "github.com/javinizer/javinizer-go/docs/swagger"
	apiauth "github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

const disposableMarker = "javinizer-sync-e2e disposable fixture\n"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func validateRoot(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("fixture root must be absolute and clean")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("fixture root must be an existing private directory, not a symlink")
	}
	info, err = os.Lstat(filepath.Join(root, ".javinizer-sync-e2e"))
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(disposableMarker)) {
		return errors.New("fixture marker must be a fixed regular file")
	}
	marker, err := os.ReadFile(filepath.Join(root, ".javinizer-sync-e2e"))
	if err != nil || string(marker) != disposableMarker {
		return errors.New("refusing unmarked fixture directory")
	}
	for _, name := range []string{"names.db", "names.db-wal", "names.db-shm", "fixture.json", "config.yaml", "worker.paused", "worker.release"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !info.Mode().IsRegular() {
			return errors.New("fixture state must be regular files")
		}
	}
	return nil
}
func run() error {
	root := os.Getenv("JAVINIZER_SYNC_E2E_ROOT")
	if err := validateRoot(root); err != nil {
		return err
	}
	if len(os.Args) > 1 {
		if len(os.Args) == 2 && os.Args[1] == "inspect" {
			return inspect(root)
		}
		return errors.New("only inspect subcommand supported")
	}
	mode := env("JAVINIZER_SYNC_E2E_MODE", "ready")
	switch mode {
	case "ready", "disabled", "missing", "stale", "future":
	default:
		return errors.New("invalid fixture mode")
	}
	port, err := strconv.Atoi(env("JAVINIZER_SYNC_E2E_PORT", "18081"))
	if err != nil || port < 1024 || port > 65535 {
		return errors.New("invalid fixture port")
	}
	pause, err := strconv.Atoi(env("JAVINIZER_SYNC_E2E_PAUSE_AFTER", "-1"))
	if err != nil || pause < -1 {
		return errors.New("invalid pause count")
	}
	for _, name := range []string{"tmp", "artifacts", "artifacts/ready", "artifacts/stale", "artifacts/future", "artifacts/missing"} {
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("fixture directories must not be symlinks")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
	}
	_, fixtureErr := os.Stat(filepath.Join(root, "fixture.json"))
	_, dbErr := os.Stat(filepath.Join(root, "names.db"))
	if errors.Is(fixtureErr, os.ErrNotExist) != errors.Is(dbErr, os.ErrNotExist) {
		return errors.New("incomplete fixture initialization; use a fresh directory")
	}
	dsn := url.URL{Scheme: "file", Path: filepath.Join(root, "names.db"), RawQuery: "_journal_mode=WAL&_busy_timeout=10000"}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn.String(), LogLevel: "silent"})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		return err
	}
	if errors.Is(fixtureErr, os.ErrNotExist) {
		if err := provision(root, db); err != nil {
			return err
		}
	} else if fixtureErr != nil {
		return fixtureErr
	}
	cfg := config.DefaultConfig(nil, nil)
	cfg.Server.Host, cfg.Server.Port = "127.0.0.1", port
	cfg.Logging.Level = "error"
	cfg.System.TempDir = filepath.Join(root, "tmp")
	cfg.System.VersionCheckEnabled = false
	cfg.API.Security.AllowedDirectories = []string{root}
	cfg.API.Security.DeniedDirectories = nil
	cfg.API.Security.AllowedOrigins = []string{env("JAVINIZER_SYNC_E2E_ORIGIN", "http://localhost:5181")}
	cfg.API.Security.RateLimit.RequestsPerMinute = 0
	cfg.Metadata.ActressDatabase.Enabled = true
	artifactMode := mode
	if artifactMode == "disabled" {
		artifactMode = "ready"
	}
	cfg.Metadata.ActressDatabase.OfflineNameSync = config.OfflineNameSyncConfig{Enabled: mode != "disabled", ArtifactDirectory: filepath.Join(root, "artifacts", artifactMode), MaxAgeHours: 24, MaxQueuedJobs: 8, MaxQueuedTasks: 32}
	configPath := filepath.Join(root, "config.yaml")
	auth, err := apiauth.NewAuthManager(configPath, time.Hour, func(string) (string, bool) { return "", false })
	if err != nil {
		return err
	}
	if !auth.IsInitialized() {
		if err := auth.Setup("admin", "adminpassword123"); err != nil {
			return err
		}
	}
	auth.SetDisableRateLimit(true)
	repos := db.Repositories()
	auth.SetApiTokenRepo(repos.ApiTokenRepo)
	repos.ActressRepo = &gatedRepository{ActressRepository: database.NewActressRepository(db), root: root, pauseAfter: pause}
	fs := afero.NewOsFs()
	engine := template.NewEngine()
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{DB: db, ScraperRegistry: scraperutil.NewScraperRegistry()}, ConfigFile: configPath, Repos: repos, Auth: auth, Fs: fs, EventEmitter: eventlog.NewEmitter(repos.EventRepo), Reverter: history.NewReverter(fs, repos.BatchFileOpRepo), TokenStore: core.NewTokenStore(), JobStore: worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, cfg.System.TempDir, engine, fs, worker.WithActressRepo(repos.ActressRepo), worker.WithHistoryRepo(repos.HistoryRepo), worker.WithEditTransactor(db), worker.WithCollisionRepo(repos.CreditCollisionRepo))}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	defer rt.Shutdown()
	router := server.NewServer(rt)
	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: router, ReadHeaderTimeout: 5 * time.Second}
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", srv.Addr)
	if err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { result <- srv.Serve(listener) }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	// Disabled services intentionally do not run an artifact reader. Do not wait
	// for their Ready channel: readiness is the listening HTTP server in that mode.
	err = func() error {
		if mode != "disabled" {
			select {
			case <-rt.OfflineNameSync().Ready():
			case err := <-result:
				return err
			case <-sig:
				return nil
			}
		}
		fmt.Println("javinizer-sync-e2e: ready")
		select {
		case err := <-result:
			return err
		case <-sig:
			return nil
		}
	}()
	rt.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := srv.Shutdown(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}
