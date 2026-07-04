package desktop

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"
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
