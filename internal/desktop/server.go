package desktop

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	apiauth "github.com/javinizer/javinizer-go/internal/api/auth"
	apicore "github.com/javinizer/javinizer-go/internal/api/core"
	apiserver "github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/system"
)

var (
	listenFn = func(ctx context.Context, port int) (net.Listener, error) {
		addr := "127.0.0.1:0"
		if port > 0 {
			addr = fmt.Sprintf("127.0.0.1:%d", port)
		}
		return (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	}
	loadConfigFn = config.LoadOrCreate
)

// ServerInstance is a running API server bound to a free localhost port.
// It serves the REST API and the embedded Web UI (web/dist) — the same surface
// the `javinizer web` command exposes — so the desktop webview can load it.
type ServerInstance struct {
	baseURL     string
	srv         *http.Server
	rt          *apicore.APIRuntime
	deps        *apicore.APIDeps
	listener    net.Listener
	done        chan struct{}
	once        sync.Once
	shutdownErr error
}

// StartServer bootstraps and starts the API server on a free 127.0.0.1 port.
// It mirrors cmd/javinizer/commands/api.run but (a) binds to an OS-assigned
// free port so it never collides with a running `javinizer web`, (b) returns
// immediately so the caller (the Wails window) can open at the returned URL,
// and (c) exposes a Shutdown that drains in-flight requests.
//
// The server runs until ctx is cancelled or Shutdown is called.
func StartServer(ctx context.Context, configFile string) (*ServerInstance, error) {
	cfg, err := loadConfigFn(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	config.ApplyEnvironmentOverrides(cfg)

	// Try to reuse the port from config for stable restarts (firewall rules,
	// bookmarks, etc.). If the configured port is unavailable or zero, fall
	// back to an OS-assigned free port.
	cfg.Server.Host = "127.0.0.1"
	ln, err := listenFn(ctx, cfg.Server.Port)
	if err != nil && cfg.Server.Port != 0 {
		logging.Infof("desktop: port %d unavailable, falling back to a free port", cfg.Server.Port)
		ln, err = listenFn(ctx, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("desktop: failed to find free port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if cfg.Server.Port != port {
		cfg.Server.Port = port
		if saveErr := config.Save(cfg, configFile); saveErr != nil {
			logging.Warnf("desktop: failed to persist port %d to config: %v", port, saveErr)
		}
	}

	if _, err := config.Prepare(cfg); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	logging.Infof("Loaded configuration from %s", configFile)

	authManager, err := apiauth.NewAuthManager(configFile, apiauth.DefaultSessionTTL)
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("failed to initialize authentication: %w", err)
	}

	deps, rt, err := apicore.BootstrapAPI(cfg, configFile, authManager)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	authManager.SetApiTokenRepo(deps.Repos.ApiTokenRepo)

	// This binary is a desktop build (the file is compiled with the desktop
	// build tag), so the upgrade UX must point users at a new bundle rather
	// than an in-place self-swap. Recorded once at bootstrap so the /version
	// handler can surface it without importing internal/desktop (import cycle).
	deps.CoreDeps.SetInstallEnvironment(system.EnvironmentDesktop)

	router := apiserver.NewServer(rt)

	// The desktop webview connects to /ws/progress directly at 127.0.0.1:PORT
	// (the Wails AssetServer returns 501 for WS upgrades, so the reverse proxy
	// cannot carry them). That connection is cross-origin, so override the
	// upgrader to accept the desktop webview origins. See ws_origin.go.
	rt.EnsureRuntime().SetWebSocketUpgrader(desktopWSUpgrader())

	apiserver.LogServerInfo(cfg)

	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	inst := &ServerInstance{
		baseURL:  fmt.Sprintf("http://127.0.0.1:%d", port),
		srv:      srv,
		rt:       rt,
		deps:     deps,
		listener: ln,
		done:     make(chan struct{}),
	}

	go func() {
		defer close(inst.done)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logging.Errorf("desktop: API server stopped: %v", err)
		}
	}()

	// Bind server lifetime to the caller's context (e.g. the Wails app ctx).
	go func() {
		select {
		case <-ctx.Done():
			_ = inst.Shutdown()
		case <-inst.done:
		}
	}()

	return inst, nil
}

// BaseURL is the origin the webview should load (e.g. http://127.0.0.1:54321).
func (s *ServerInstance) BaseURL() string { return s.baseURL }

// Done is closed when the server has stopped.
func (s *ServerInstance) Done() <-chan struct{} { return s.done }

// Deps returns the API dependencies so the desktop bootstrap can wire
// build-specific services (e.g. the bundle updater) into CoreDeps. Returns
// nil after Shutdown has released them.
func (s *ServerInstance) Deps() *apicore.APIDeps { return s.deps }

// Shutdown gracefully stops the HTTP server, drains in-flight requests, and
// releases API runtime + database resources. Safe to call multiple times;
// the first call performs the work and subsequent calls are no-ops returning
// the first result.
func (s *ServerInstance) Shutdown() error {
	s.once.Do(func() {
		if s.srv == nil {
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.shutdownErr = s.srv.Shutdown(shutdownCtx)
		s.srv = nil
		if s.rt != nil {
			if s.shutdownErr == nil {
				s.shutdownErr = s.rt.CloseAfterHTTPDrain(10 * time.Second)
			} else {
				s.rt.Shutdown()
			}
			s.rt = nil
		}
		// Retain dependencies on failed HTTP/background drain: active work can
		// still hold scraper snapshots. Only release after verified quiescence.
		if s.deps != nil && s.shutdownErr == nil {
			_ = s.deps.CoreDeps.Close()
			s.deps = nil
		}
	})
	return s.shutdownErr
}
