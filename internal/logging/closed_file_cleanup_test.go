package logging

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type cleanupBarrier struct{ done chan struct{} }

func (c cleanupBarrier) Close() error { close(c.done); return nil }

func closedLogFixture(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "closed.log"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f
}

func TestReloadContinuesCleanupAfterClosedFile(t *testing.T) {
	init := newLoggerInit()
	done := make(chan struct{})
	init.current.Store(&loggerState{logger: logrus.New(), closers: []io.Closer{closedLogFixture(t), cleanupBarrier{done}}})
	require.NoError(t, init.Init(&Config{Level: "info", Format: "text", Output: "stdout"}))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup stopped at already closed file")
	}
	require.Same(t, os.Stdout, init.getLogrus().Out)
}

func TestShutdownContinuesCleanupAfterClosedFile(t *testing.T) {
	// Global entry points cannot use an isolated initializer. Restore both the
	// singleton and its compatibility alias; no subtest runs in parallel.
	previous, previousCurrent := globalInit, current
	t.Cleanup(func() { globalInit = previous; current = previousCurrent })
	for _, shutdown := range []struct {
		name  string
		close func()
	}{{"public", CloseLogger}, {"internal", closeLogger}} {
		t.Run(shutdown.name, func(t *testing.T) {
			globalInit = newLoggerInit()
			current = &globalInit.current
			done := make(chan struct{})
			globalInit.current.Store(&loggerState{logger: logrus.New(), closers: []io.Closer{closedLogFixture(t), cleanupBarrier{done}}})
			shutdown.close()
			select {
			case <-done:
			default:
				t.Fatal("shutdown skipped remaining closers")
			}
			require.Nil(t, globalInit.current.Load().(*loggerState))
			shutdown.close() // shutdown remains idempotent after a close error
		})
	}
}
