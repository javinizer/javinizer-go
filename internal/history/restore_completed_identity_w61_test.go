//go:build !windows

package history

// POSTER-WRITE-HARDENING codex PR#215 wave-61 (P2) — the ENOSYS-times-skipped
// publish leg (AIX/Solaris/illumos: stagedHandleChtimes answers ENOSYS, r12
// refuses the name-based fallback) surfaces ErrPublishCompleted WITH a
// verified non-nil destination identity. Pre-wave-61 copyRestoreBytesPublish
// discarded that identity and returned the error, so the explicit-revert and
// sweep-restore legs treated the completed publish as a FAILED restore:
// backup + journal entry never consumed, every retry republished and failed
// again. The fix: treat the completed-with-identity outcome as a successful
// restore — hand the identity back so wave-31's dest revalidation + backup
// removal + journal consumption run exactly like the plain-success leg; on
// drift mid-revalidation the wave-31 refusal fires (no consumption, no
// overwrite of a substitute).

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w61CompletedIdentityPublish replaces publishStagedBoundInfoFn with a fake
// that simulates the ENOSYS-times-skipped leg: the staged bytes land at dest
// (the successful publish consumed the staged name) and the call returns the
// dest's post-publish stat alongside an ErrPublishCompleted-carrying error.
// A non-nil substitute replays a foreign writer swapping dest AFTER the
// verified publish — dest then carries the substitute while the returned
// identity still names the published object (the wave-31 revalidation
// refuses it without consuming or overwriting).
func w61CompletedIdentityPublish(t *testing.T, substitute []byte) {
	t.Helper()
	prev := publishStagedBoundInfoFn
	publishStagedBoundInfoFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		stagedBytes, rerr := afero.ReadFile(p.FS, p.Staged)
		require.NoError(t, rerr, "the staged copy holds the backup bytes at publish time")
		require.NoError(t, afero.WriteFile(p.FS, p.Dest, stagedBytes, 0o644),
			"the completed publish lands the staged bytes at dest")
		_ = p.FS.Remove(p.Staged) // the successful publish consumed the staged name
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		info, lerr := lstatRestoreSource(p.FS, p.Dest) // the verified published identity
		require.NoError(t, lerr)
		if substitute != nil {
			require.NoError(t, afero.WriteFile(p.FS, p.Dest, substitute, 0o644),
				"the foreign writer swaps dest for a substitute after the verified publish")
		}
		return info, fmt.Errorf("staged times for %s never applied — no fd-scoped times primitive on this platform and the name-based fallback is refused (its check/apply window could chase a planted substitute); the destination carries the published bytes: %w: %w", p.Dest, syscall.ENOSYS, fsutil.ErrPublishCompleted)
	}
	t.Cleanup(func() { publishStagedBoundInfoFn = prev })
}

// Explicit-revert: the completed-with-identity publish converges — backup
// removed, journal entry consumed, dest carries the restored bytes.
func TestRestoreReplacementJournalW61_CompletedIdentityConverges(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W61A", []byte("new poster"), []byte("original poster"), "stamped")

	w61CompletedIdentityPublish(t, nil)

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err, "the completed-with-identity publish is a successful restore, not a failure")
	require.True(t, restored[dest])

	require.Equal(t, "original poster", string(mustRead2(t, fs, dest)),
		"dest carries the restored backup bytes")
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "the verified-owned backup was unlinked like the plain-success leg")
	require.Empty(t, w25JournalEntries(t, repo, op.ID), "the journal entry was consumed")
}

// Sweep crash-window restore: identical convergence through the no-replace
// leg — backup removed, entry consumed, dest carries the restored bytes.
func TestSweepW61_CompletedIdentityConverges(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25SweepCrashOp(t, fs, repo, "W61B", []byte("original bytes"), "stamped")

	w61CompletedIdentityPublish(t, nil)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed, "the completed-with-identity publish heals the crash-window restore")
	require.Equal(t, "original bytes", string(mustRead2(t, fs, dest)), "dest carries the restored bytes")
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "the verified-owned backup was unlinked")
	require.Empty(t, w25JournalEntries(t, repo, op.ID), "the journal entry was consumed")
}

// Drift mid-revalidation: the completed publish landed the bytes, but a
// foreign writer swapped dest for a substitute before the wave-31 recheck.
// The refusal fires — backup retained, journal entry NOT consumed, the
// substitute at dest never overwritten (no consumption licenses a removal).
func TestRestoreReplacementJournalW61_DriftRefusesWithoutConsumption(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W61C", []byte("new poster"), []byte("original poster"), "stamped")

	w61CompletedIdentityPublish(t, []byte("foreign substitute"))

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no longer names the restored object")
	require.True(t, restored[dest], "the restored path stays protected from the delete-list cleanup")

	require.Equal(t, "original poster", string(mustRead2(t, fs, backup)),
		"the backup — the sole remaining copy of the pre-replacement bytes — is retained on drift")
	require.Equal(t, "foreign substitute", string(mustRead2(t, fs, dest)),
		"the substitute at dest is never overwritten (no consumption licenses a removal)")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the journal entry was NOT consumed on drift")
	require.False(t, entries[0].RestorePending,
		"no restore-pending marker: that would certify dest carries restored bytes (unproven on drift)")
}
