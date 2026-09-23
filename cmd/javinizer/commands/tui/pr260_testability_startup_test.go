package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type readinessWriter struct {
	destination io.Writer
	ready       chan struct{}
	once        sync.Once
}

func newReadinessWriter(destination io.Writer) *readinessWriter {
	return &readinessWriter{destination: destination, ready: make(chan struct{})}
}

func (w *readinessWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ready) })
	return w.destination.Write(p)
}

// Execute the registered command, not just a model constructor: the configured
// repositories, sort factory and event subscriber are built before Bubble Tea runs.
func TestPR260TUICommandStartsAndQuitsWithIsolatedIO(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	require.NoError(t, os.Mkdir(source, 0o755))
	configPath := filepath.Join(dir, "config.yaml")
	cfg, err := config.LoadOrCreate(configPath)
	require.NoError(t, err)
	cfg.Database.DSN = filepath.Join(dir, "javinizer.db")
	logPath := filepath.Join(dir, "tui.log")
	cfg.Logging.Output = logPath
	require.NoError(t, config.Save(cfg, configPath))
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	// This command performs real config, database, and repository bootstrap before
	// Bubble Tea starts. Keep the whole operation bounded, while allowing loaded CI
	// runners enough time to reach the observable first render.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	startedAt := time.Now()
	root := &cobra.Command{Use: "javinizer"}
	root.PersistentFlags().String("config", configPath, "isolated config")
	cmd := NewCommand()
	root.AddCommand(cmd)
	root.SetContext(ctx)
	cmd.SetIn(input)
	output := newReadinessWriter(io.Discard)
	cmd.SetOut(output)
	cmd.SetErr(io.Discard)
	root.SetArgs([]string{"tui", source})
	quitSent := make(chan struct{})
	go func() {
		defer close(quitSent)
		// Send q only after Bubble Tea has rendered, rather than guessing how long
		// real command bootstrap will take on a loaded runner.
		select {
		case <-output.ready:
		case <-ctx.Done():
			return
		}
		_, _ = writer.Write([]byte("q"))
	}()
	err = root.Execute()
	require.NoError(t, err, "TUI must render and terminate via q, not the deadline (elapsed %s, context: %v)", time.Since(startedAt), ctx.Err())
	require.NoError(t, ctx.Err())
	<-quitSent
	require.NoError(t, os.Remove(logPath))
	require.FileExists(t, cfg.Database.DSN)
	require.FileExists(t, configPath)
	db, err := database.New(&database.Config{Type: "sqlite", DSN: cfg.Database.DSN, LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	require.NotNil(t, database.NewActressRepository(db))
	require.NotNil(t, database.NewMovieRepository(db))
}

func TestPR260TUICommandCancellationIsBounded(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	require.NoError(t, os.Mkdir(source, 0o755))
	configPath := filepath.Join(dir, "config.yaml")
	cfg, err := config.LoadOrCreate(configPath)
	require.NoError(t, err)
	cfg.Database.DSN = filepath.Join(dir, "javinizer.db")
	logPath := filepath.Join(dir, "tui.log")
	cfg.Logging.Output = logPath
	require.NoError(t, config.Save(cfg, configPath))
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	root := &cobra.Command{Use: "javinizer"}
	root.PersistentFlags().String("config", configPath, "isolated config")
	cmd := NewCommand()
	root.AddCommand(cmd)
	root.SetContext(ctx)
	cmd.SetIn(input)
	cmd.SetOut(io.Discard)
	root.SetArgs([]string{"tui", source})
	err = root.Execute()
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled), "%v", err)
	require.NoError(t, os.Remove(logPath))
}
