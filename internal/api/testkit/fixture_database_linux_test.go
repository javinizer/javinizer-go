//go:build linux

package testkit

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCreateTestDepsReportsUnusableDatabaseTempPath(t *testing.T) {
	const childMarker = "JAVINIZER_TESTKIT_LONG_TMPDIR_CHILD"
	if os.Getenv(childMarker) == "1" {
		CreateTestDeps(t, config.DefaultConfig(nil, nil), "")
		return
	}

	// The bundled SQLite Unix VFS limits paths to 512 bytes. An OS-valid
	// TMPDIR can therefore work for Go's TempDir but not for its SQLite DB.
	longDir := t.TempDir()
	for i := 0; i < 4; i++ {
		longDir = filepath.Join(longDir, strings.Repeat("nested-build-output-", 8))
	}
	require.Greater(t, len(longDir), 512)
	require.NoError(t, os.MkdirAll(longDir, 0o700))

	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	args := []string{"-test.run=^TestCreateTestDepsReportsUnusableDatabaseTempPath$"}
	if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+coverDir.Value.String())
	}

	child := exec.CommandContext(ctx, executable, args...)
	child.Env = append(os.Environ(), childMarker+"=1", "TMPDIR="+longDir)
	out, err := child.CombinedOutput()
	require.NoError(t, ctx.Err(), "%s", out)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s", out)
	require.Equal(t, 1, exitErr.ExitCode(), "%s", out)
	require.Contains(t, string(out), "Failed to create test database: failed to connect to database: unable to open database file")
}
