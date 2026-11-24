//go:build linux

package testkit

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestCreateTestDepsReportsMigrationWriteLimit(t *testing.T) {
	const childMarker = "JAVINIZER_TESTKIT_MIGRATION_WRITE_LIMIT_CHILD"
	if os.Getenv(childMarker) == "1" {
		// Allow the initial SQLite database and WAL headers to be written,
		// but let the kernel reject larger writes during schema migration.
		// Keep the hard limit unchanged and restore the original soft limit
		// before the test binary writes any coverage output.
		var previous unix.Rlimit
		require.NoError(t, unix.Getrlimit(unix.RLIMIT_FSIZE, &previous))
		const migrationLimit = 16 * 1024
		require.Greater(t, previous.Cur, uint64(migrationLimit))
		bounded := previous
		bounded.Cur = migrationLimit
		signal.Ignore(unix.SIGXFSZ)
		defer signal.Reset(unix.SIGXFSZ)
		require.NoError(t, unix.Setrlimit(unix.RLIMIT_FSIZE, &bounded))
		defer func() { require.NoError(t, unix.Setrlimit(unix.RLIMIT_FSIZE, &previous)) }()

		CreateTestDeps(t, config.DefaultConfig(nil, nil), "")
		return
	}

	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	args := []string{"-test.run=^TestCreateTestDepsReportsMigrationWriteLimit$"}
	if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+coverDir.Value.String())
	}
	child := exec.CommandContext(ctx, executable, args...)
	childTempRoot := t.TempDir()
	child.Env = append(os.Environ(), childMarker+"=1", "TMPDIR="+childTempRoot)
	out, err := child.CombinedOutput()
	require.NoError(t, ctx.Err(), "%s", out)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s", out)
	require.Equal(t, 1, exitErr.ExitCode(), "%s", out)
	require.Contains(t, string(out), "Failed to migrate test database:")
	require.Contains(t, string(out), "disk I/O error")

	// Migration failure must still run the database and TempDir cleanups.
	entries, err := os.ReadDir(childTempRoot)
	require.NoError(t, err)
	require.Empty(t, entries)
}
