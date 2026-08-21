package history

// POSTER-WRITE-HARDENING wave-56 (codex P1, PR#215 finding F2): the busy-
// marker acquisition now rides a provenance-bound token (fsutil.Acquire-
// ReplacementBusyEx). The token is always non-empty on a nil error in
// production, but the sweep side defends against a future acquire that yields
// a non-nil release with an empty token and a nil error: it REFUSES to record
// the claim (treats it as a failed acquire), warns, and keeps the entry's
// pre-mutation classification. The leg is only reachable through the
// acquireReplacementBusyExFn seam (same discipline as rearmPublishFn /
// restoreStagingOwnershipFn); these tests drive it with ("", nil) and pin
// both sweepOne and consumeRearmRefusedPending.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// swapBusyAcquireProvenanceUnavailable replaces the busy-marker acquire seam
// with one that yields a non-nil release, an empty token, and a nil error —
// the provenance-unavailable posture the refusal leg defends against. The
// release is a no-op (the seam's release stands in for the marker the test
// never created).
func swapBusyAcquireProvenanceUnavailable(t *testing.T) {
	t.Helper()
	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(_ afero.Fs, _ string) (func(), string, error) {
		return func() {}, "", nil
	}
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })
}

// sweepOne refuses to record a claim whose provenance is unavailable: the
// empty-token leg fires before any marker is journaled or mutation staged —
// no busy marker is created, the backup is byte-intact, the destination is
// absent, and the journal entry keeps its armed classification (no in-process
// pending removal either).
func TestSweepOneW56_BusyTokenProvenanceUnavailableRefusesClaim(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w56-prov", "PRV-001", "/w56-prov", p3HexA)

	swapBusyAcquireProvenanceUnavailable(t)

	sweeper := NewReplacementSweeper(base, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := base.Stat(backup)
	require.NoError(t, err)

	require.Equal(t, 0, sweeper.sweepOne(context.Background(), idx, "/w56-prov", info),
		"provenance unavailable refuses to record the claim — the entry is kept")

	// No mutation surface was entered: no busy marker claimed, backup
	// byte-intact, destination absent, journal entry still armed.
	markerExists, _ := afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.False(t, markerExists, "the refusal precedes the marker record — no busy marker was created")
	require.Equal(t, "original-PRV-001", string(mustRead2(t, base, backup)), "the backup is byte-intact")
	destExists, _ := afero.Exists(base, dest)
	require.False(t, destExists, "nothing was published")
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "the entry keeps its armed classification")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)), "no in-process pending removal was recorded")
}

// consumeRearmRefusedPending shares the provenance-unavailable refusal: the
// empty-token leg fires before the claim is recorded or the destination
// re-certified — no busy marker is created, the certified destination is
// untouched, and the rearm-refused pending keeps its classification.
func TestSweepW56_RearmRefusedPendingBusyTokenProvenanceUnavailableRefusesClaim(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, repo, "job-w56-regp", "RGP-001", "/w56-regp", "poster.jpg", p3HexA)
	writeSweepFile(t, base, dest, "new", time.Hour)
	require.NoError(t, base.Remove(backup))
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), repo, op.ID, sweepSlash(backup), models.RestorePendingKindRearmRefused))

	swapBusyAcquireProvenanceUnavailable(t)

	sweeper := NewReplacementSweeper(base, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	require.Len(t, idx.refusedPendings, 1)
	entry := idx.refusedPendings[0]

	require.Equal(t, 0, sweeper.consumeRearmRefusedPending(context.Background(), idx, entry),
		"provenance unavailable refuses to record the claim — the rearm-refused pending is kept")

	markerExists, _ := afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.False(t, markerExists, "no busy marker was claimed")
	require.Equal(t, "new", string(mustRead2(t, base, dest)), "the certified destination is untouched")
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(), "classification preserved")
}
