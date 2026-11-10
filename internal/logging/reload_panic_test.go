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

// Formatting the recovered value is the last operation that observes it in
// the cleanup goroutine. The channel makes that recovery observable without
// replacing process-wide stderr or racing the asynchronous closer.
type observedCleanupPanic struct{ recovered chan<- struct{} }

func (p observedCleanupPanic) String() string {
	p.recovered <- struct{}{}
	return "old logger cleanup failed"
}

type panickingLogCloser struct{ value observedCleanupPanic }

func (c panickingLogCloser) Close() error { panic(c.value) }

func TestReloadKeepsNewLoggerUsableAfterCleanupPanic(t *testing.T) {
	init := newLoggerInit()
	recovered := make(chan struct{}, 1)
	oldLogger := logrus.New()
	init.current.Store(&loggerState{
		logger: oldLogger,
		closers: []io.Closer{
			panickingLogCloser{observedCleanupPanic{recovered}},
		},
	})

	path := filepath.Join(t.TempDir(), "replacement.log")
	require.NoError(t, init.Init(&Config{Level: "info", Format: "text", Output: path}))
	state := init.current.Load().(*loggerState)
	t.Cleanup(func() {
		for _, closer := range state.closers {
			require.NoError(t, closer.Close())
		}
	})

	select {
	case <-recovered:
	case <-time.After(5 * time.Second):
		t.Fatal("old logger cleanup panic was not recovered and reported")
	}

	logger := init.getLogrus()
	require.NotSame(t, oldLogger, logger)
	require.Same(t, state.logger, logger)
	logger.Info("replacement logger remains usable")
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(contents), "replacement logger remains usable")
}
