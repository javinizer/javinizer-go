package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/stretchr/testify/require"
)

func TestStartServerContinuesAfterPortPersistenceFailure(t *testing.T) {
	configPath := writeTestConfig(t)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	cfg.Server.Port = 0
	cfg.System.VersionCheckEnabled = false
	require.NoError(t, config.Save(cfg, configPath))
	t.Setenv("JAVINIZER_DB", ":memory:")
	var original []byte

	logPath := filepath.Join(t.TempDir(), "server.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = logFile.Close() })
	restoreOutput := logging.SetOutput(logFile)
	t.Cleanup(restoreOutput)

	originalListen := listenFn
	t.Cleanup(func() { listenFn = originalListen })
	savedPath := configPath + ".saved"
	markerPath := filepath.Join(configPath, "keep.txt")
	listenCalls := 0
	listenFn = func(ctx context.Context, port int) (net.Listener, error) {
		listenCalls++
		require.Zero(t, port, "request a native ephemeral port")
		listener, err := originalListen(ctx, port)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = listener.Close() })
		// The real config was already loaded. Change only the filesystem at
		// this existing boundary, without racing startup or replacing Save.
		// A directory cannot be overwritten by the staged config file.
		original, err = os.ReadFile(configPath)
		require.NoError(t, err)
		require.NoError(t, os.Rename(configPath, savedPath))
		require.NoError(t, os.Mkdir(configPath, 0o700))
		require.NoError(t, os.WriteFile(markerPath, []byte("preserve this directory"), 0o600))
		return listener, nil
	}

	inst, err := StartServer(t.Context(), configPath)
	require.NoError(t, err, "port persistence is best-effort and must not prevent serving")
	t.Cleanup(func() { _ = inst.Shutdown() })
	require.Equal(t, 1, listenCalls)
	port := inst.listener.Addr().(*net.TCPAddr).Port
	require.Positive(t, port)
	require.Equal(t, fmt.Sprintf("http://127.0.0.1:%d", port), inst.BaseURL())
	require.Equal(t, port, inst.Deps().CoreDeps.GetConfig().Server.Port)

	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	response, err := client.Get(inst.BaseURL() + "/health")
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusOK, response.StatusCode)
	var health struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&health))
	require.NoError(t, response.Body.Close())
	require.Equal(t, "ok", health.Status)
	require.NoError(t, inst.Shutdown())
	require.Nil(t, inst.Deps(), "successful shutdown releases dependencies")
	select {
	case <-inst.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop after shutdown")
	}

	logged, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(logged), fmt.Sprintf("desktop: failed to persist port %d to config", port))
	require.Contains(t, string(logged), "failed to atomically replace config file")
	preserved, err := os.ReadFile(savedPath)
	require.NoError(t, err)
	require.Equal(t, original, preserved)
	marker, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	require.Equal(t, "preserve this directory", string(marker))
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(configPath), ".config.yaml.tmp-*"))
	require.NoError(t, err)
	require.Empty(t, temporary, "failed port save must remove its staged config")
	_, err = os.Stat(configPath + ".lock")
	require.ErrorIs(t, err, os.ErrNotExist, "failed port save must release the config lock")

	// Restore the actual config and prove a later startup can publish its port.
	require.NoError(t, os.Remove(markerPath))
	require.NoError(t, os.Remove(configPath))
	require.NoError(t, os.Rename(savedPath, configPath))
	listenFn = originalListen
	recovered, err := StartServer(t.Context(), configPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = recovered.Shutdown() })
	persisted, err := config.Load(configPath)
	require.NoError(t, err)
	require.Equal(t, recovered.listener.Addr().(*net.TCPAddr).Port, persisted.Server.Port)
	require.NoError(t, recovered.Shutdown())
}
