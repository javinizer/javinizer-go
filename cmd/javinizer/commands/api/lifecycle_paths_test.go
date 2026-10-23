package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func lifecycleConfig(t *testing.T) (string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(dir, "database.sqlite")
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.System.VersionCheckEnabled = false
	cfg.Logging.Output = "stdout"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	t.Setenv("JAVINIZER_TEMP_DIR", cfg.System.TempDir)
	t.Setenv("JAVINIZER_E2E_AUTH", "false")
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	return path, cfg
}

func TestWebCommandPropagatesInitializationFailure(t *testing.T) {
	cmd := NewCommand()
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	require.NoError(t, os.WriteFile(path, []byte("["), 0o600))
	cmd.Flags().String("config", path, "")
	cmd.SetArgs(nil)
	err := cmd.ExecuteContext(context.Background())
	require.ErrorContains(t, err, "failed to initialize API dependencies: failed to load config:")
}

func TestWebCommandRejectsMalformedAuthState(t *testing.T) {
	path, _ := lifecycleConfig(t)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), "auth.credentials.json"), []byte("{}"), 0o600))
	deps, rt, err := Run(NewCommand(), path, "", 0)
	require.Nil(t, deps)
	require.Nil(t, rt)
	require.EqualError(t, err, "failed to initialize authentication: invalid auth credential file: username is required")
}

func TestWebCommandPropagatesBootstrapFailure(t *testing.T) {
	path, cfg := lifecycleConfig(t)
	blocker := filepath.Join(filepath.Dir(path), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("fixture"), 0o600))
	t.Setenv("JAVINIZER_DB", filepath.Join(blocker, "database.sqlite"))
	deps, rt, err := Run(NewCommand(), path, "", 0)
	require.Nil(t, deps)
	require.Nil(t, rt)
	require.ErrorContains(t, err, "failed to create database directory:")
	var pathErr *os.PathError
	require.True(t, errors.As(err, &pathErr))
	require.Equal(t, blocker, pathErr.Path)
	_, err = os.Stat(cfg.Database.DSN)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestWebCommandCanceledLifetimeReturnsCleanly(t *testing.T) {
	path, _ := lifecycleConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := NewCommand()
	cmd.SetContext(ctx)
	require.NoError(t, run(cmd, path, "", 0))
}

func TestWebCommandReportsListenFailure(t *testing.T) {
	path, _ := lifecycleConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	port := listener.Addr().(*net.TCPAddr).Port
	cmd := NewCommand()
	cmd.SetContext(context.Background())
	err = run(cmd, path, "127.0.0.1", port)
	require.ErrorContains(t, err, "failed to start server: listen tcp")
	var opErr *net.OpError
	require.True(t, errors.As(err, &opErr))
	require.Equal(t, "listen", opErr.Op)
	require.Equal(t, listener.Addr().String(), opErr.Addr.String())
}

// A 100 Continue response is emitted only when the handler starts reading the
// request body. Keep that body unfinished to exercise the real shutdown timeout
// deterministically, rather than guessing when a request became active.
func TestWebCommandReportsShutdownDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises the production 10-second graceful shutdown deadline")
	}
	path, _ := lifecycleConfig(t)
	t.Setenv("JAVINIZER_SETUP_SECRET", "fixture-bootstrap-secret")
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := reserved.Addr().String()
	port := reserved.Addr().(*net.TCPAddr).Port
	require.NoError(t, reserved.Close())
	ctx, cancel := context.WithCancel(context.Background())
	cmd := NewCommand()
	cmd.SetContext(ctx)
	result := make(chan error, 1)
	go func() { result <- run(cmd, path, "127.0.0.1", port) }()
	var conn net.Conn
	returned := false
	t.Cleanup(func() {
		cancel()
		if conn != nil {
			_ = conn.Close()
		}
		if !returned {
			select {
			case <-result:
			case <-time.After(15 * time.Second):
				t.Error("API command did not terminate during cleanup")
			}
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			break
		}
		select {
		case runErr := <-result:
			returned = true
			t.Fatalf("server stopped before accepting requests: %v", runErr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, err, "server must become available before the bounded startup deadline")
	require.NoError(t, conn.SetDeadline(time.Now().Add(20*time.Second)))
	_, err = fmt.Fprintf(conn, "POST /api/v1/auth/setup HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 1048576\r\nExpect: 100-continue\r\nX-Setup-Secret: fixture-bootstrap-secret\r\nConnection: close\r\n\r\n", addr)
	require.NoError(t, err)
	reader := bufio.NewReader(conn)
	interim, err := http.ReadResponse(reader, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusContinue, interim.StatusCode)
	require.NoError(t, interim.Body.Close())
	cancel()
	select {
	case err = <-result:
		returned = true
	case <-time.After(15 * time.Second):
		t.Fatal("shutdown did not observe its 10-second deadline")
	}
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualError(t, err, "failed to shut down server: context deadline exceeded")
	// Release the blocked JSON decoder, then read its response to establish that
	// the handler has finished before cleanup closes the remaining client socket.
	tcp, ok := conn.(*net.TCPConn)
	require.True(t, ok)
	require.NoError(t, tcp.CloseWrite())
	response, err := http.ReadResponse(reader, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.NoError(t, response.Body.Close())
}
