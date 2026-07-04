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
)

var (
	listenFn = func(ctx context.Context) (net.Listener, error) {
		return (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	}
	loadConfigFn = config.LoadOrCreate
)

// ServerInstance is a running API server bound to a free localhost port.
// It serves the REST API and the embedded Web UI (web/dist) — the same surface
// the `javinizer api` command exposes — so the desktop webview can load it.
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
// free port so it never collides with a running `javinizer api`, (b) returns
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

	// Bind to a free localhost port. The listener holds the port across the
	// (slow) bootstrap below, so the URL we report to the window stays valid.
	ln, err := listenFn(ctx)
	if err != nil {
		return nil, fmt.Errorf("desktop: failed to find free port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port

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

	router := apiserver.NewServer(rt)
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
			s.rt.Shutdown()
			s.rt = nil
		}
		if s.deps != nil {
			_ = s.deps.CoreDeps.DB.Close()
			s.deps = nil
		}
	})
	return s.shutdownErr
}
