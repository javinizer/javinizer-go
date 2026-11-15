package main

import (
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFixtureServerShutdownLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		waitReady  bool
		serverExit string
	}{
		{name: "accept-error-before-readiness", waitReady: true, serverExit: "closed-listener"},
		{name: "accept-error-after-readiness", serverExit: "closed-listener"},
		{name: "signal-before-readiness", waitReady: true},
		{name: "signal-without-artifact-reader"},
		{name: "closed-server-before-readiness", waitReady: true, serverExit: "closed-server"},
		{name: "closed-server-after-readiness", serverExit: "closed-server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise real net/http Serve errors and shutdown, not a fabricated
			// successful HTTP/runtime result or a package-global transport hook.
			lc := net.ListenConfig{}
			listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			server := &http.Server{ReadHeaderTimeout: time.Second}
			t.Cleanup(func() { _ = server.Close() })
			shutdownCalled := make(chan struct{}, 1)
			server.RegisterOnShutdown(func() { shutdownCalled <- struct{}{} })
			var shutdownProbe net.Listener
			if tc.serverExit != "closed-server" {
				shutdownProbe, err = lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
				require.NoError(t, err)
				require.NoError(t, shutdownProbe.Close())
			}

			result := make(chan error, 1)
			var serveErr error
			switch tc.serverExit {
			case "closed-listener":
				require.NoError(t, listener.Close())
				serveErr = server.Serve(listener)
				require.ErrorIs(t, serveErr, net.ErrClosed)
				result <- serveErr
			case "closed-server":
				require.NoError(t, server.Close())
				serveErr = server.Serve(listener)
				require.ErrorIs(t, serveErr, http.ErrServerClosed)
				result <- serveErr
			default:
				go func() { result <- server.Serve(listener) }()
			}
			var ready <-chan struct{}
			if tc.waitReady {
				ready = make(chan struct{})
			}
			sig := make(chan os.Signal, 1)
			if tc.serverExit == "" {
				sig <- os.Interrupt
			}
			output, err := os.CreateTemp(t.TempDir(), "readiness")
			require.NoError(t, err)
			t.Cleanup(func() { _ = output.Close() })
			oldStdout := os.Stdout
			os.Stdout = output
			t.Cleanup(func() { os.Stdout = oldStdout })

			stopCalls := 0
			err = waitForFixtureShutdown(server, ready, result, sig, func() {
				stopCalls++
				if shutdownProbe != nil {
					// This closed real listener reports an accept failure before
					// Shutdown, but ErrServerClosed afterward. Unlike an async
					// shutdown callback, it deterministically checks the order.
					require.ErrorIs(t, server.Serve(shutdownProbe), net.ErrClosed, "stop the worker before HTTP shutdown")
				}
			})
			require.Equal(t, 1, stopCalls)
			if tc.serverExit == "closed-listener" {
				require.Same(t, serveErr, err, "preserve the original accept failure")
			} else {
				require.NoError(t, err)
			}
			select {
			case <-shutdownCalled:
			case <-time.After(5 * time.Second):
				t.Fatal("HTTP shutdown was not called")
			}
			if tc.serverExit == "" {
				select {
				case err := <-result:
					require.ErrorIs(t, err, http.ErrServerClosed)
				case <-time.After(5 * time.Second):
					t.Fatal("HTTP server did not stop after signal")
				}
			}
			message, err := os.ReadFile(output.Name())
			require.NoError(t, err)
			if tc.waitReady {
				require.Empty(t, message, "do not announce readiness after early shutdown")
			} else {
				require.Equal(t, "javinizer-sync-e2e: ready\n", string(message))
			}
		})
	}
}
