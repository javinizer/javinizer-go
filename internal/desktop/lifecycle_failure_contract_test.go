package desktop

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type shutdownErrorListener struct {
	net.Listener
	err error
}

func (l *shutdownErrorListener) Close() error {
	_ = l.Listener.Close()
	return l.err
}

type shutdownCountingScraper struct {
	models.Scraper
	closes atomic.Int32
}

func (s *shutdownCountingScraper) Name() string { return "shutdown-contract" }
func (s *shutdownCountingScraper) Close() error { s.closes.Add(1); return nil }

func TestServerShutdownListenerFailureCancelsRuntimeButRetainsDependencies(t *testing.T) {
	configPath := writeTestConfig(t)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	cfg.System.VersionCheckEnabled = false
	require.NoError(t, config.Save(cfg, configPath))
	t.Setenv("JAVINIZER_DB", ":memory:")

	want := errors.New("listener cleanup failed")
	originalListen := listenFn
	t.Cleanup(func() { listenFn = originalListen })
	listenFn = func(ctx context.Context, _ int) (net.Listener, error) {
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		return &shutdownErrorListener{Listener: listener, err: want}, nil
	}
	inst, err := StartServer(context.Background(), configPath)
	require.NoError(t, err)
	deps, rt := inst.Deps(), inst.rt
	t.Cleanup(func() { _ = inst.Shutdown(); _ = deps.CoreDeps.Close() })
	scraper := &shutdownCountingScraper{}
	deps.CoreDeps.GetRegistry().RegisterInstance(scraper)
	runtimeCtx := rt.ServerCtx()

	// Exercise the actual HTTP server before provoking listener shutdown failure.
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(inst.BaseURL() + "/api/v1/health")
	require.NoError(t, err)
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	require.ErrorIs(t, inst.Shutdown(), want)
	require.ErrorIs(t, runtimeCtx.Err(), context.Canceled)
	require.Same(t, deps, inst.Deps(), "an unsuccessful drain must retain dependency ownership")
	require.Zero(t, scraper.closes.Load(), "failed drain must not close potentially active scraper snapshots")
	require.ErrorIs(t, inst.Shutdown(), want, "repeat shutdown preserves the first failure")
	require.Zero(t, scraper.closes.Load())
	select {
	case <-inst.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP listener did not stop")
	}
	// This fixture has no active requests/tasks now, so explicitly release the
	// retained graph and verify that ownership was not lost or double-released.
	require.NoError(t, deps.CoreDeps.Close())
	require.EqualValues(t, 1, scraper.closes.Load())
}
