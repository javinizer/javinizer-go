//go:build linux

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestStagedHandleChtimesFollowsRenamedInode(t *testing.T) {
	dir := t.TempDir()
	stage := filepath.Join(dir, "stage")
	renamed := filepath.Join(dir, "renamed")
	file, err := os.Create(stage)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	_, err = file.WriteString("owned staging bytes")
	require.NoError(t, err)
	require.NoError(t, os.Rename(stage, renamed))

	// A replacement at the original name must not receive the staged handle's
	// timestamps. This exercises the real kernel path, without a syscall seam.
	require.NoError(t, os.WriteFile(stage, []byte("replacement bytes"), 0o600))
	replacementTime := time.Unix(1_600_000_000, 0)
	require.NoError(t, os.Chtimes(stage, replacementTime, replacementTime))
	atime := time.Unix(1_650_000_000, 0)
	mtime := time.Unix(1_650_000_100, 0)
	require.NoError(t, stagedHandleChtimes(file.Fd(), atime, mtime))

	var ownedStat, replacementStat unix.Stat_t
	require.NoError(t, unix.Stat(renamed, &ownedStat))
	require.NoError(t, unix.Stat(stage, &replacementStat))
	require.Equal(t, atime.Unix(), int64(ownedStat.Atim.Sec))
	require.Equal(t, mtime.Unix(), int64(ownedStat.Mtim.Sec))
	require.Equal(t, replacementTime.Unix(), int64(replacementStat.Atim.Sec))
	require.Equal(t, replacementTime.Unix(), int64(replacementStat.Mtim.Sec))
	ownedBytes, err := os.ReadFile(renamed)
	require.NoError(t, err)
	require.Equal(t, "owned staging bytes", string(ownedBytes))
	replacementBytes, err := os.ReadFile(stage)
	require.NoError(t, err)
	require.Equal(t, "replacement bytes", string(replacementBytes))
}

func TestStagedHandleChtimesRejectsInvalidDescriptor(t *testing.T) {
	timestamp := time.Unix(1_650_000_000, 0)
	err := stagedHandleChtimes(^uintptr(0), timestamp, timestamp)
	// Older kernels can reject AT_EMPTY_PATH before checking the descriptor,
	// so the /proc fallback can report a missing path instead of EBADF.
	require.Error(t, err)
}
