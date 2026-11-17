//go:build windows

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// Windows cannot flush the read-only directory handle used by syncDir. This
// verifies the real OS failure before checking the compatibility contract, so a
// successful wrapper call alone cannot silently stop exercising the fallback.
func TestNativeWindowsDirectorySyncCompatibility(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	syncErr := f.Sync()
	require.Error(t, syncErr, "native Windows directory flush must fail for this fixture")
	var pathErr *os.PathError
	require.ErrorAs(t, syncErr, &pathErr)
	require.Equal(t, "sync", pathErr.Op)
	require.Equal(t, dir, pathErr.Path)
	var nativeErr syscall.Errno
	require.ErrorAs(t, pathErr.Err, &nativeErr)
	require.NotZero(t, nativeErr)
	require.NoError(t, f.Close())
	t.Logf("native directory flush error: %v", syncErr)

	require.NoError(t, syncDir(dir), "unsupported directory flush must remain best effort on Windows")
	missing := filepath.Join(dir, "missing")
	err = syncDir(missing)
	require.ErrorIs(t, err, os.ErrNotExist, "compatibility must not swallow directory-open errors")
	require.Contains(t, err.Error(), "failed to open directory for sync")

	// Exercise actual public configuration persistence and replacement despite
	// unsupported directory flushing. Both saves must remain readable, and no
	// lock, staging file, or backup may leak after publication.
	path := filepath.Join(dir, "config.yaml")
	cfg := DefaultConfig(nil, nil)
	for _, host := range []string{"127.0.0.1", "localhost"} {
		cfg.Server.Host = host
		require.NoError(t, Save(cfg, path))
		loaded, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, host, loaded.Server.Host)
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, "config.yaml", entries[0].Name())
	}
}
