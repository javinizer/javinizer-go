package desktop

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	apicore "github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
)

// writeTestConfig creates a minimal config file in t.TempDir() with an
// in-memory SQLite database, matching the pattern used by
// cmd/javinizer/commands/api tests. Returns the config file path.
func writeTestConfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.yaml"
	configContent := `config_version: 3
database:
  dsn: ":memory:"
  type: sqlite
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return configPath
}

// TestServerInstance_BaseURL verifies that BaseURL returns a well-formed
// http://127.0.0.1:<non-zero-port> URL after StartServer succeeds.
func TestServerInstance_BaseURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Setenv("JAVINIZER_DB", ":memory:")
	configPath := writeTestConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inst, err := StartServer(ctx, configPath)
	if err != nil {
		t.Fatalf("StartServer() error: %v", err)
	}
	defer func() { _ = inst.Shutdown() }()

	baseURL := inst.BaseURL()
	if baseURL == "" {
		t.Fatal("BaseURL() returned empty string")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("BaseURL() is not a valid URL: %v", err)
	}
	if u.Hostname() != "127.0.0.1" {
		t.Errorf("BaseURL() host = %q, want 127.0.0.1", u.Hostname())
	}
	portStr := u.Port()
	if portStr == "" {
		t.Fatal("BaseURL() has no port")
	}
	if portStr == "0" {
		t.Error("BaseURL() port = 0, want a kernel-assigned non-zero port")
	}
}

// TestServerInstance_Shutdown_Idempotent verifies that calling Shutdown
// multiple times does not panic and the second call returns the same
// result (nil or the first error) without re-closing resources.
func TestServerInstance_Shutdown_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Setenv("JAVINIZER_DB", ":memory:")
	configPath := writeTestConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inst, err := StartServer(ctx, configPath)
	if err != nil {
		t.Fatalf("StartServer() error: %v", err)
	}

	firstErr := inst.Shutdown()
	secondErr := inst.Shutdown()

	if secondErr != nil {
		t.Errorf("second Shutdown() returned error: %v; want nil (idempotent no-op)", secondErr)
	}
	_ = firstErr
}

// TestServerInstance_Shutdown_NilServer covers the s.srv == nil branch in
// Shutdown: a ServerInstance that never started (or was constructed without
// a server) must ShutDown without panicking and return nil.
func TestServerInstance_Shutdown_NilServer(t *testing.T) {
	inst := &ServerInstance{srv: nil}
	if err := inst.Shutdown(); err != nil {
		t.Errorf("Shutdown() with nil server returned error: %v; want nil", err)
	}
}

// TestServerInstance_DoneClosesAfterShutdown verifies that Done() blocks
// before Shutdown and is closed after Shutdown completes.
func TestServerInstance_DoneClosesAfterShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Setenv("JAVINIZER_DB", ":memory:")
	configPath := writeTestConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inst, err := StartServer(ctx, configPath)
	if err != nil {
		t.Fatalf("StartServer() error: %v", err)
	}

	// Done() should block while the server is running.
	select {
	case <-inst.Done():
		t.Fatal("Done() closed before Shutdown was called")
	default:
	}

	_ = inst.Shutdown()

	// Done() should be closed after Shutdown.
	select {
	case <-inst.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done() did not close within 5s after Shutdown")
	}
}

// TestServerInstance_StartServer_InvalidConfig verifies that StartServer
// with a bogus config path still succeeds — LoadOrCreate creates defaults
// when the file is missing, so this is the expected behavior.
func TestServerInstance_StartServer_InvalidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Setenv("JAVINIZER_DB", ":memory:")
	tmpDir := t.TempDir()
	bogusPath := tmpDir + "/nonexistent/config.yaml"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inst, err := StartServer(ctx, bogusPath)
	if err != nil {
		t.Fatalf("StartServer() with missing config should succeed (LoadOrCreate creates defaults), got: %v", err)
	}
	defer func() { _ = inst.Shutdown() }()

	if inst.BaseURL() == "" {
		t.Error("BaseURL() is empty even though StartServer succeeded")
	}
}

// TestStartServer_ListenFails covers the net.Listen error branch (server.go
// ~L56-58): when the free-port listener cannot be created, StartServer must
// surface the error. Listen on 127.0.0.1:0 essentially never fails, so the
// listenFn seam is swapped to return a deterministic error.
func TestStartServer_ListenFails(t *testing.T) {
	origListen, origLoad := listenFn, loadConfigFn
	t.Cleanup(func() { listenFn, loadConfigFn = origListen, origLoad })

	listenFn = func(ctx context.Context) (net.Listener, error) {
		return nil, errors.New("simulated listen failure")
	}
	loadConfigFn = func(path string) (*config.Config, error) {
		return &config.Config{}, nil
	}

	_, err := StartServer(context.Background(), "unused.yaml")
	if err == nil {
		t.Fatal("StartServer() with failing listener should error, got nil")
	}
}

// TestStartServer_PrepareFails covers the second config.Prepare error branch
// (server.go ~L63-66): after env overrides, an invalid config must abort
// startup and close the already-bound listener. LoadOrCreate always returns a
// valid (prepared) config, so the loadConfigFn seam is swapped to return a
// config whose version is too new -> Prepare fails before reaching Validate.
func TestStartServer_PrepareFails(t *testing.T) {
	origListen, origLoad := listenFn, loadConfigFn
	t.Cleanup(func() { listenFn, loadConfigFn = origListen, origLoad })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}
	defer ln.Close() // idempotent; the error path also closes it

	listenFn = func(ctx context.Context) (net.Listener, error) { return ln, nil }
	loadConfigFn = func(path string) (*config.Config, error) {
		return &config.Config{ConfigVersion: config.CurrentConfigVersion + 1}, nil
	}

	_, err = StartServer(context.Background(), "unused.yaml")
	if err == nil {
		t.Fatal("StartServer() with invalid config should error, got nil")
	}
}

// TestStartServer_ServeError covers the non-ErrServerClosed Serve error branch
// (server.go ~L102-104): when the underlying listener is closed externally
// (not via Shutdown), srv.Serve returns a non-ErrServerClosed error which must
// be logged and inst.done closed.
func TestStartServer_ServeError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Setenv("JAVINIZER_DB", ":memory:")
	configPath := writeTestConfig(t)

	inst, err := StartServer(context.Background(), configPath)
	if err != nil {
		t.Fatalf("StartServer() error: %v", err)
	}
	defer inst.Shutdown() // drains rt/deps/db even though Serve already errored

	// Close the underlying listener directly (NOT Shutdown) so Serve returns a
	// non-ErrServerClosed error, exercising the error-log branch.
	_ = inst.listener.Close()

	select {
	case <-inst.Done():
		// done closed after Serve returned its error
	case <-time.After(5 * time.Second):
		t.Fatal("Done() did not close after external listener close")
	}
}

// TestStartServer_InvalidYAML covers the config.LoadOrCreate error branch:
// a config file with invalid YAML syntax must surface as a StartServer error
// before any listener or server is created.
func TestStartServer_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.yaml"
	if err := os.WriteFile(configPath, []byte("::: not valid yaml ::: [[[\n  : bad"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := StartServer(context.Background(), configPath)
	if err == nil {
		t.Fatal("StartServer() with invalid YAML should fail, got nil")
	}
}

// TestStartServer_CorruptCredentials covers the apiauth.NewAuthManager error
// branch: a valid config with a corrupt auth.credentials.json next to it must
// surface as a StartServer error (the listener is closed on failure).
func TestStartServer_CorruptCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Setenv("JAVINIZER_DB", ":memory:")
	configPath := writeTestConfig(t)
	// Write a corrupt credentials file alongside the config (same dir).
	if err := os.WriteFile(
		filepath.Dir(configPath)+"/auth.credentials.json",
		[]byte("::: not valid json :::"), 0o600,
	); err != nil {
		t.Fatalf("write corrupt credentials: %v", err)
	}

	_, err := StartServer(context.Background(), configPath)
	if err == nil {
		t.Fatal("StartServer() with corrupt credentials should fail, got nil")
	}
}

// TestStartServer_InvalidDBPath covers the apicore.BootstrapAPI error branch:
// a valid config whose database DSN points to an unwritable path (under a
// regular file) must surface as a StartServer error. The listener is closed
// on failure, so no resource leak.
func TestStartServer_InvalidDBPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	// Create a regular file to use as a blocker — a DSN under it can't be created.
	blocker, err := os.CreateTemp(tmpDir, "blocker")
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	defer blocker.Close()

	configPath := tmpDir + "/config.yaml"
	configContent := `config_version: 3
database:
  dsn: "` + blocker.Name() + `/sub/javinizer.db"
  type: sqlite
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err = StartServer(context.Background(), configPath)
	if err == nil {
		t.Fatal("StartServer() with invalid DB path should fail, got nil")
	}
}

// TestServerInstance_ContextCancelTriggersShutdown verifies that cancelling
// the context passed to StartServer triggers a graceful shutdown.
func TestServerInstance_ContextCancelTriggersShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Setenv("JAVINIZER_DB", ":memory:")
	configPath := writeTestConfig(t)

	ctx, cancel := context.WithCancel(context.Background())

	inst, err := StartServer(ctx, configPath)
	if err != nil {
		t.Fatalf("StartServer() error: %v", err)
	}

	// Done() should block while running.
	select {
	case <-inst.Done():
		t.Fatal("Done() closed before context cancel")
	default:
	}

	cancel()

	// After context cancellation, Done() should close.
	select {
	case <-inst.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done() did not close within 5s after context cancel")
	}
}

// TestServerInstance_Deps verifies that Deps() returns the API dependencies
// the desktop bootstrap wires into the ServerInstance (the bundle updater is
// injected via CoreDeps.SetBundleUpdater off this accessor).
func TestServerInstance_Deps(t *testing.T) {
	deps := &apicore.APIDeps{}
	inst := &ServerInstance{deps: deps}
	if inst.Deps() != deps {
		t.Fatal("Deps() should return the wired APIDeps pointer")
	}
}

// TestServerInstance_Deps_Nil verifies Deps() returns nil after Shutdown has
// released the dependencies (the zero value).
func TestServerInstance_Deps_Nil(t *testing.T) {
	inst := &ServerInstance{}
	if inst.Deps() != nil {
		t.Fatal("Deps() should be nil on a freshly constructed ServerInstance")
	}
}
