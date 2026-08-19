//go:build !windows

package history

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-8 (codex P2 follow-up on wave-7's
// fsutil.RestoreStagingOwnership): the history restore path — and the startup
// sweep sharing copyRestoreBytes — must hand the staged inode back to the
// backup's owner before the swap, or a privileged revert of another account's
// backup permanently loses its uid/gid when the backup is deleted. The chown
// seam itself (restoreFchown) lives in fsutil and is reachable only from that
// package's tests (already covered there in wave 7); this package's seam
// restoreStagingOwnershipFn wraps the fsutil helper and records the same
// (staged, uid, gid) triple restoreFchown receives — the fsutil wave-7 tests
// prove the 1:1 mapping from this triple to the fchown call.
//
// wave-29 (codex P1, PR#215): the seam carries the OPEN STAGING HANDLE
// instead of the staged path — the hand-off is a handle-scoped fchown. The
// recorder restores the staged NAME from the handle (afero.File.Name()) so
// ordering/state assertions stay name-based.

// ownershipHandoffW8 records one restoreStagingOwnershipFn invocation.
type ownershipHandoffW8 struct {
	staged   string
	uid, gid int
	haveIDs  bool
}

// swapRestoreOwnershipW8 installs a recording seam over the history restore
// path's ownership hand-off, restoring production wiring in cleanup. hook
// runs INSIDE the seam call (after staging, before the swap) so tests can
// probe the on-disk state at exactly that point.
func swapRestoreOwnershipW8(t *testing.T, hook func(h ownershipHandoffW8)) *[]ownershipHandoffW8 {
	t.Helper()
	calls := new([]ownershipHandoffW8)
	prev := restoreStagingOwnershipFn
	restoreStagingOwnershipFn = func(_ afero.Fs, staged afero.File, source os.FileInfo) {
		stagedName := ""
		if staged != nil {
			stagedName = staged.Name()
		}
		h := ownershipHandoffW8{staged: stagedName}
		if st, ok := source.Sys().(*syscall.Stat_t); ok {
			h.uid, h.gid, h.haveIDs = int(st.Uid), int(st.Gid), true
		}
		*calls = append(*calls, h)
		if hook != nil {
			hook(h)
		}
	}
	t.Cleanup(func() { restoreStagingOwnershipFn = prev })
	return calls
}

func writeOSFileW8(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), perm))
}

func readOSFileW8(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// ownershipOfW8 returns the kernel uid/gid of path (POSIX OsFs FileInfo).
func ownershipOfW8(t *testing.T, path string) (int, int) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok, "POSIX OsFs FileInfo must expose *syscall.Stat_t")
	return int(st.Uid), int(st.Gid)
}

// TestRestoreOwnershipW8_HistoryRestoreHandsBackupOwnershipBeforeSwap: the
// history restore path calls the ownership helper with the backup's uid/gid
// on the STAGED inode, after staging and before the ReplaceFile swap.
func TestRestoreOwnershipW8_HistoryRestoreHandsBackupOwnershipBeforeSwap(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	writeOSFileW8(t, backup, "original bytes", 0o640)
	writeOSFileW8(t, dest, "current bytes", 0o644)
	uid, gid := ownershipOfW8(t, backup)

	calls := swapRestoreOwnershipW8(t, func(h ownershipHandoffW8) {
		// Probed inside the seam call: the staged inode already carries the
		// restored bytes while the destination still holds the pre-restore
		// bytes — ownership is applied AFTER staging, BEFORE the swap.
		require.Equal(t, "original bytes", readOSFileW8(t, h.staged))
		require.Equal(t, "current bytes", readOSFileW8(t, dest))
	})

	require.NoError(t, copyRestoreBytes(fs, backup, dest))

	require.Len(t, *calls, 1, "exactly one ownership hand-off per restore")
	h := (*calls)[0]
	require.True(t, h.haveIDs, "the OsFs backup FileInfo exposes *syscall.Stat_t")
	require.Equal(t, uid, h.uid, "the hand-off targets the backup's uid, not the Javinizer account's")
	require.Equal(t, gid, h.gid, "the hand-off targets the backup's gid, not the Javinizer account's")
	require.True(t, strings.HasPrefix(h.staged, dest+".rstr."), "ownership lands on the staged inode, got %s", h.staged)

	require.Equal(t, "original bytes", readOSFileW8(t, dest))
	// copyRestoreBytes never consumes the backup — the caller does afterwards.
	require.Equal(t, "original bytes", readOSFileW8(t, backup))
}

// TestRestoreOwnershipW8_EpermOwnershipHandoffStillRestores: an unprivileged
// restore's chown answers EPERM, which the fsutil helper deliberately
// swallows (wave-7-covered in that package). Modelled at this seam as the
// observable outcome — the hand-off is attempted, ownership stays untouched,
// and the restore must still swap and complete.
func TestRestoreOwnershipW8_EpermOwnershipHandoffStillRestores(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexB
	writeOSFileW8(t, backup, "original bytes", 0o600)
	writeOSFileW8(t, dest, "current bytes", 0o644)

	calls := swapRestoreOwnershipW8(t, nil)

	require.NoError(t, copyRestoreBytes(fs, backup, dest))
	require.Len(t, *calls, 1, "the hand-off is attempted even when the kernel would refuse it")
	require.Equal(t, "original bytes", readOSFileW8(t, dest), "a swallowed EPERM never blocks the restore")
	_, err := os.Stat(backup)
	require.NoError(t, err, "the backup is left for the caller's consumption step")
}

// TestRestoreOwnershipW8_RealHelperRestoreSucceedsRegardlessOfChownOutcome:
// no seam override — the real fsutil.RestoreStagingOwnership runs the real
// os.Chown. Unprivileged runs EPERM (swallowed), root runs succeed; either
// way the restore completes. This pins the production wiring behind
// restoreStagingOwnershipFn to the actual helper (no duplicated logic).
func TestRestoreOwnershipW8_RealHelperRestoreSucceedsRegardlessOfChownOutcome(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexC
	writeOSFileW8(t, backup, "original bytes", 0o640)
	writeOSFileW8(t, dest, "current bytes", 0o644)

	require.NoError(t, copyRestoreBytes(fs, backup, dest))
	require.Equal(t, "original bytes", readOSFileW8(t, dest))
}

// TestRestoreOwnershipW8_StartupSweepRestoreHandsBackupOwnership: the startup
// sweep's journaled crash-window restore funnels through the same
// copyRestoreBytes staging path, so it hands ownership back too (and consumes
// the journal entry afterwards).
func TestRestoreOwnershipW8_StartupSweepRestoreHandsBackupOwnership(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	writeOSFileW8(t, backup, "original bytes", 0o640)
	// Destination absent: the sweep must restore the journaled crash window.
	uid, gid := ownershipOfW8(t, backup)

	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-w8-sweep", "W8-SWEEP", dest, backup, 1, models.RevertStatusApplied)

	calls := swapRestoreOwnershipW8(t, nil)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed, "the sweep restores the journaled crash-window backup")

	require.Len(t, *calls, 1, "the sweep's restore shares the ownership hand-off")
	h := (*calls)[0]
	require.True(t, h.haveIDs)
	require.Equal(t, uid, h.uid)
	require.Equal(t, gid, h.gid)
	require.True(t, strings.HasPrefix(h.staged, dest+".rstr."), "ownership lands on the staged inode, got %s", h.staged)
	require.Equal(t, "original bytes", readOSFileW8(t, dest))

	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Empty(t, gf.Replacements, "the journal entry is consumed after the restore")
}
