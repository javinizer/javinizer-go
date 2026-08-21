package downloader

// POSTER-WRITE-HARDENING r15 (codex P2, PR#215) — mirror of r14's history
// wave-61 fix into the downloader rollback path. copyBackupToDestPublish's
// publish-error arm used to dismiss EVERY ErrPublishCompleted-carrying error
// as a failed rollback. The ENOSYS-times-skipped leg (a platform with no
// fd-scoped times primitive: PublishStagedBoundInfo skips the times after an
// identity-VERIFIED publish and surfaces ErrPublishCompleted WITH the
// post-publish-verified destination stat) is a SUCCESSFUL rollback: dest
// provably carries the restored bytes. Pre-r15 the ConfirmReplacement-failure
// rollback never removed the backup nor retracted the journal entry — the
// armed entry stayed against the dest forever. The fix classifies exactly
// like r14's copyRestoreBytesPublish: when fsutil.PublishCompleted(pubErr) &&
// published != nil, treat as a successful rollback and hand the identity back
// so the caller's wave-31 revalidation + backup removal + journal retraction
// run like the plain-success leg. On drift mid-revalidation the wave-31
// refusal fires (no removal, no overwrite of a substitute).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w64FrozenFileInfo snapshots a MemMapFs FileInfo's Size/ModTime at capture
// time: afero's mem FileInfo is a LIVE view (a post-capture substitute write
// retargets Size/ModTime through the same object), while the real os.Lstat
// PublishStagedBoundInfo hands back on the ENOSYS leg is a frozen snapshot.
// The frozen view keeps the drift test's identity capture faithful to
// production (Mode/Sys still delegate to the live info — regular on every
// leg here, and nil Sys yields no dev/inode exactly like MemMapFs).
type w64FrozenFileInfo struct {
	os.FileInfo
	size    int64
	modTime time.Time
}

func (f w64FrozenFileInfo) Size() int64        { return f.size }
func (f w64FrozenFileInfo) ModTime() time.Time { return f.modTime }

// w64CompletedIdentityRollbackPublish replaces rollbackPublishStagedBoundInfoFn
// with a fake that simulates the ENOSYS-times-skipped leg: the staged bytes
// land at dest (the successful publish consumed the staged name) and the call
// returns the dest's post-publish stat alongside an ErrPublishCompleted-carrying
// error. A non-nil substitute replays a foreign writer swapping dest AFTER the
// verified publish — dest then carries the substitute while the returned
// identity still names the published object (the wave-31 revalidation refuses
// it without consuming or overwriting).
func w64CompletedIdentityRollbackPublish(t *testing.T, substitute []byte) {
	t.Helper()
	prev := rollbackPublishStagedBoundInfoFn
	rollbackPublishStagedBoundInfoFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		stagedBytes, rerr := afero.ReadFile(p.FS, p.Staged)
		require.NoError(t, rerr, "the staged copy holds the backup bytes at publish time")
		require.NoError(t, afero.WriteFile(p.FS, p.Dest, stagedBytes, 0o644),
			"the completed publish lands the staged bytes at dest")
		_ = p.FS.Remove(p.Staged) // the successful publish consumed the staged name
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		info, lerr := lstatBackupCandidate(p.FS, p.Dest) // the verified published identity
		require.NoError(t, lerr)
		frozen := w64FrozenFileInfo{FileInfo: info, size: info.Size(), modTime: info.ModTime()}
		if substitute != nil {
			require.NoError(t, afero.WriteFile(p.FS, p.Dest, substitute, 0o644),
				"the foreign writer swaps dest for a substitute after the verified publish")
		}
		return frozen, fmt.Errorf("staged times for %s never applied — no fd-scoped times primitive on this platform and the name-based fallback is refused (its check/apply window could chase a planted substitute); the destination carries the published bytes: %w", p.Dest, fsutil.ErrPublishCompleted)
	}
	t.Cleanup(func() { rollbackPublishStagedBoundInfoFn = prev })
}

// Convergence: the completed-with-identity rollback converges — backup
// consumed, journal entry retracted, dest carries the restored pre-existing
// bytes (exactly like the plain-success rollback leg).
func TestInstallOverwritingW64_CompletedIdentityRollbackConverges(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)

	w64CompletedIdentityRollbackPublish(t, nil)

	confirmErr := errors.New("w64 confirmation failed")
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: confirmErr,
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w64-converges", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, confirmErr)
	require.Contains(t, err.Error(), "install-confirm failed, rolled back to pre-existing bytes")

	require.Equal(t, old, mustReadDownloaderW7(t, fs, dest),
		"dest carries the restored pre-existing bytes")
	require.Equal(t, 1, recorder.releaseCalls, "the journal entry was retracted")
	require.True(t, recorder.releaseSawNoBackup, "the verified-owned backup was unlinked like the plain-success leg")
	require.Empty(t, recorder.get(), "the journal entry was retracted")
	requireNoDownloaderBackupW25(t, fs, filepath.Dir(dest))
}

// Drift mid-revalidation: the completed publish landed the bytes, but a
// foreign writer swapped dest for a substitute before the wave-31 recheck.
// The refusal fires — backup retained, journal entry NOT retracted, the
// substitute at dest never overwritten (no consumption licenses a removal).
func TestInstallOverwritingW64_CompletedIdentityRollbackDriftRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	substitute := []byte("a foreign writer swapped dest for a substitute after the verified rollback publish")
	staged, dest := w25InstallFixture(t, fs, old)

	w64CompletedIdentityRollbackPublish(t, substitute)

	confirmErr := errors.New("w64 confirmation failed")
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: confirmErr,
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w64-drift", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, confirmErr)
	require.Contains(t, err.Error(), "install-confirm failed")
	require.Contains(t, err.Error(), "no longer names the restored object")
	require.Contains(t, err.Error(), "journal entry stays armed")

	require.Equal(t, substitute, mustReadDownloaderW7(t, fs, dest),
		"the substitute at dest is never overwritten (no consumption licenses a removal)")
	records := recorder.get()
	require.Len(t, records, 1, "the journal entry was NOT retracted on drift")
	require.Equal(t, old, mustReadDownloaderW7(t, fs, records[0].backupPath),
		"the backup — the sole remaining copy of the pre-existing bytes — is retained on drift")
	require.Zero(t, recorder.releaseCalls, "the armed entry is never released after the refused cleanup")
	require.Empty(t, recorder.getPendings(), "no restore-pending mark — the armed entry arbitrates recovery")
}
