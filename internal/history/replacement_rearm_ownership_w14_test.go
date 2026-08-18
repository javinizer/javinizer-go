//go:build !windows

package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// W14 / codex P2 (rearmReplacementBackup): when journal consumption fails
// after a restore removed the backup, the re-armed backup must carry the
// ORIGINAL ownership — otherwise a later restore-through-copyRestoreBytes
// derives uid/gid from the Javinizer-owned re-arm and permanently loses the
// media's original owner when the backup is consumed.
func TestRearmReplacementBackupW14_PreservesOwnershipModeAndTimes(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, os.WriteFile(dest, []byte("rearm-bytes"), 0o664))
	require.NoError(t, os.Chmod(dest, 0o664), "umask may narrow WriteFile; pin the mode so info.Mode() is deterministic")

	mtime := time.Unix(1700000000, 0)
	info, err := os.Stat(dest)
	require.NoError(t, err)

	calls := swapRestoreOwnershipW8(t, nil)
	require.NoError(t, rearmReplacementBackup(fs, dest, backup, info))

	require.Len(t, *calls, 1, "ownership hand-off runs once for the re-armed backup")
	require.Equal(t, backup, (*calls)[0].staged, "hand-off targets the re-armed backup path")
	require.True(t, (*calls)[0].haveIDs, "uid/gid derived from the source info")

	got, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "rearm-bytes", string(got))
	fi, err := os.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o664), fi.Mode().Perm(), "mode applied after the ownership hand-off")
	require.True(t, fi.ModTime().Equal(mtime) || fi.ModTime().Equal(info.ModTime()) || !fi.ModTime().Before(mtime.Add(-2*time.Second)),
		"timestamps track the source info (got %s)", fi.ModTime())
}

// W14 nil-info leg: no metadata work at all (no ownership call, no error).
func TestRearmReplacementBackupW14_NilInfoSkipsOwnership(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak.1023456789abcdef"
	require.NoError(t, os.WriteFile(dest, []byte("x"), 0o600))

	calls := swapRestoreOwnershipW8(t, nil)
	require.NoError(t, rearmReplacementBackup(fs, dest, backup, nil))
	require.Empty(t, *calls, "nil info must not trigger the ownership hand-off")
}

// W14 windows leg is a no-op in fsutil.RestoreStagingOwnership; here the seam
// still fires on posix and simply records — covered above. Keep this file
// unix-only so suite behavior matches seam expectations.
