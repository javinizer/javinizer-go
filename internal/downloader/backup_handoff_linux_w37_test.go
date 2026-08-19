//go:build linux

package downloader

// POSTER-WRITE-HARDENING wave-37 (codex P2, PR#215) — the Linux atomic legs:
// renameat2(RENAME_EXCHANGE) swaps the dest and reservation dentries with NO
// verify→rename window; the exchange-parked placeholder at dest is unlinked
// only while provably the claimed reservation object.
//
// Test matrix:
//   - real exchange success → dest absent, backup holds the original bytes,
//     full armed overwrite through installOverwriting unchanged;
//   - a foreign plant riding the swap (kernel seam plants at the reservation
//     name mid-exchange) → typed collision refusal: plant byte-intact AT THE
//     DESTINATION, original bytes preserved at the backup name, nothing
//     journaled, no unlink of the foreign object;
//   - ENOSYS / EINVAL / EOPNOTSUPP → the identity-bound degrade leg serves
//     the handoff;
//   - an arbitrary exchange error → the swap provably never happened, the
//     still-ours reservation is released (or a foreign occupant preserved);
//   - releaseExchangedPlaceholder / destPlaceholderMatchesClaim unit legs.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// withBackupExchangeKernel replays a scripted kernel exchange and restores
// the real one.
func withBackupExchangeKernel(t *testing.T, fn func(src, dst string) error) {
	t.Helper()
	previous := backupExchangeKernel
	backupExchangeKernel = fn
	t.Cleanup(func() { backupExchangeKernel = previous })
}

// reserveW37Pair builds a real destination + claimed reservation pair on an
// OsFs temp dir, returning everything the handoff needs.
func reserveW37Pair(t *testing.T) (fsys afero.Fs, dir, dest, backup string, claim os.FileInfo) {
	t.Helper()
	fsys = afero.NewOsFs()
	dir = t.TempDir()
	dest = filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))
	backup, claim, err := claimOverwriteBackupPath(fsys, dest, "w37-op")
	require.NoError(t, err)
	require.NotEmpty(t, backup)
	require.NotNil(t, claim)
	return fsys, dir, dest, backup, claim
}

// The headline atomic leg: the exchange leaves backup holding the original
// destination bytes and dest cleared of our verified placeholder.
func TestHandoffToReservedBackupW37_ExchangeSuccess(t *testing.T) {
	fsys, _, dest, backup, claim := reserveW37Pair(t)

	require.NoError(t, handoffToReservedBackup(fsys, dest, backup, claim))

	got, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "current", string(got), "the original destination bytes landed at the backup name")
	_, statErr := os.Lstat(dest)
	require.True(t, os.IsNotExist(statErr), "the exchange-parked placeholder was removed — destination absent for the staged publish")
}

// The atomic exchange leg keeps the full armed overwrite unchanged:
// destination replaced, original bytes journaled at the backup name.
func TestInstallOverwritingW37_ExchangeLegArmedOverwriteUnchanged(t *testing.T) {
	resetBackupOrdinalW22(t)
	fsys := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))
	require.NoError(t, os.WriteFile(staged, []byte("new"), 0o644))

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fsys, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w37-exchange-armed", recorder: recorder,
	})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)
	require.Equal(t, "new", string(mustReadDownloaderW7(t, fsys, dest)))
	records := recorder.get()
	require.Len(t, records, 1)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fsys, records[0].backupPath)),
		"the previous destination bytes were set aside via the atomic exchange")
}

// A foreign plant landing between the claim and the exchange RIDES the swap
// into the destination name — and the post-exchange identity re-proof refuses
// the placeholder unlink: the plant is neither destroyed nor overwritten, the
// original bytes stay preserved under the backup name, the refusal is typed.
func TestHandoffToReservedBackupW37_PlantRodeSwapKeepsAllBytes(t *testing.T) {
	fsys, _, dest, backup, claim := reserveW37Pair(t)

	real := backupExchangeKernel
	plant := []byte("foreign occupant riding the swap")
	withBackupExchangeKernel(t, func(src, dst string) error {
		// The foreign writer replaces the reservation at the last instant.
		require.NoError(t, os.Remove(dst))
		require.NoError(t, os.WriteFile(dst, plant, 0o600))
		return real(src, dst)
	})

	err := handoffToReservedBackup(fsys, dest, backup, claim)
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the refusal is the typed collision class")

	gotDest, derr := os.ReadFile(dest)
	require.NoError(t, derr)
	require.Equal(t, string(plant), string(gotDest), "the plant rode the swap and is byte-intact at the destination — never destroyed")
	gotBackup, berr := os.ReadFile(backup)
	require.NoError(t, berr)
	require.Equal(t, "current", string(gotBackup), "the original destination bytes are preserved at the backup name")
}

// Kernels/filesystems that cannot express RENAME_EXCHANGE degrade to the
// identity-bound rename leg — the handoff still completes, byte-for-byte.
func TestExchangeBackupHandoffW37_UnsupportedKernelDegrades(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENOSYS, syscall.EINVAL, syscall.EOPNOTSUPP} {
		t.Run(errno.Error(), func(t *testing.T) {
			fsys, _, dest, backup, claim := reserveW37Pair(t)
			withBackupExchangeKernel(t, func(src, dst string) error { return errno })

			exchanged, err := exchangeBackupHandoff(fsys, dest, backup, claim)
			require.NoError(t, err)
			require.False(t, exchanged, "unsupported kernels hand off to the identity-bound leg")

			require.NoError(t, handoffViaVerifiedRename(fsys, dest, backup, claim))
			got, rerr := os.ReadFile(backup)
			require.NoError(t, rerr)
			require.Equal(t, "current", string(got))
			_, statErr := os.Lstat(dest)
			require.True(t, os.IsNotExist(statErr))
		})
	}
}

// An arbitrary exchange error means the swap never happened: the failure
// surfaces wrapped and the cleanup still only ever touches the claimed
// placeholder — still-ours reservation released, foreign occupant preserved.
func TestExchangeBackupHandoffW37_ExchangeErrorBoundCleanup(t *testing.T) {
	t.Run("still-ours reservation released", func(t *testing.T) {
		fsys, _, dest, backup, claim := reserveW37Pair(t)
		sentinel := errors.New("w37 exchange wedged")
		withBackupExchangeKernel(t, func(src, dst string) error { return sentinel })

		exchanged, err := exchangeBackupHandoff(fsys, dest, backup, claim)
		require.True(t, exchanged)
		require.ErrorIs(t, err, sentinel)
		_, statErr := os.Lstat(backup)
		require.True(t, os.IsNotExist(statErr), "the still-ours placeholder was released")
		require.Equal(t, "current", string(mustReadDownloaderW7(t, fsys, dest)), "destination untouched")
	})

	t.Run("foreign occupant at reservation name preserved", func(t *testing.T) {
		fsys, _, dest, backup, claim := reserveW37Pair(t)
		sentinel := errors.New("w37 exchange wedged")
		plant := []byte("foreign occupant")
		withBackupExchangeKernel(t, func(src, dst string) error {
			require.NoError(t, os.Remove(dst))
			require.NoError(t, os.WriteFile(dst, plant, 0o600))
			return sentinel
		})

		exchanged, err := exchangeBackupHandoff(fsys, dest, backup, claim)
		require.True(t, exchanged)
		require.ErrorIs(t, err, sentinel)
		got, rerr := os.ReadFile(backup)
		require.NoError(t, rerr)
		require.Equal(t, string(plant), string(got), "the cleanup never unlinks a foreign occupant")
		require.Equal(t, "current", string(mustReadDownloaderW7(t, fsys, dest)), "destination untouched")
	})
}

// A virtual filesystem never takes the kernel leg.
func TestExchangeBackupHandoffW37_VirtualFilesystemSkipsExchange(t *testing.T) {
	exchanged, err := exchangeBackupHandoff(afero.NewMemMapFs(), "/d", "/b", nil)
	require.NoError(t, err)
	require.False(t, exchanged)
}

// destPlaceholderMatchesClaim's unit legs moved untagged
// (backup_handoff_cov_w37x_test.go) with the function itself — wave-38
// keeps the leg table host-testable.
