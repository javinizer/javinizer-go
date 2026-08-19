package workflow

// POSTER-WRITE-HARDENING codex PR#215 wave-21 (P2) — the workflow-side
// journal-marking helper the downloader's generalized (every-failure-class)
// re-arm compensation rides: dbRevertLog.MarkReplacementRestorePendingKind
// carries the classified restore-pending kind through the row's journal
// transaction — the rearm-refused wave-19 kind (delegated shorthand:
// MarkReplacementRestorePending) for unowned names, and the clean kind for
// names the failed re-arm demonstrably published (fsutil.PublishCompleted).
// The clean kind persists with legacy blob parity (no kind field); unknown
// kinds are rejected outright — this build must never persist a marker whose
// routing it cannot interpret.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestRevertLog_MarkReplacementRestorePendingKindW21_CleanPersistsLegacyBlobShape(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "W21-MARK-CLEAN")
	dest := "/dst/W21-MARK-CLEAN/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))

	require.NoError(t, rl.MarkReplacementRestorePendingKind(ctx, opID, dest, backup, models.RestorePendingKindClean))

	gf := p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 1, "the entry stays journaled — it converts, not retracts")
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindClean, gf.Replacements[0].PendingKind())

	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	require.Contains(t, row.GeneratedFiles, `"restore_pending":true`)
	require.NotContains(t, row.GeneratedFiles, "restore_pending_kind",
		"the clean kind never materializes (omitempty) — wave-18 blob parity")

	// Idempotent: the identical re-mark is a no-change merge, still nil.
	require.NoError(t, rl.MarkReplacementRestorePendingKind(ctx, opID, dest, backup, models.RestorePendingKindClean))
	require.Len(t, p3Ledger(t, repo, opID).Replacements, 1)

	// Missing entry: tolerated (idempotent), exactly like the refusal mark.
	require.NoError(t, rl.MarkReplacementRestorePendingKind(ctx, opID, dest+"/other", backup+".other", models.RestorePendingKindClean))
	require.Len(t, p3Ledger(t, repo, opID).Replacements, 1, "unmatched marks leave the ledger untouched")
}

// The rearm-refused kind round-trips verbatim; the wave-19 shorthand
// delegates to exactly this path.
func TestRevertLog_MarkReplacementRestorePendingKindW21_RefusedParityWithShorthand(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "W21-MARK-REFUSED")
	dest := "/dst/W21-MARK-REFUSED/poster.jpg"
	backup := dest + ".dlbak.1023456789abcdef"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))

	require.NoError(t, rl.MarkReplacementRestorePendingKind(ctx, opID, dest, backup, models.RestorePendingKindRearmRefused))
	gf := p3Ledger(t, repo, opID)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].RestorePendingKind)

	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	require.Contains(t, row.GeneratedFiles, `"restore_pending":true,"restore_pending_kind":"rearm_refused"`)

	// One-way merge discipline survives the kind-carrying path: a later CLEAN
	// mark must never downgrade the refused kind.
	require.NoError(t, rl.MarkReplacementRestorePendingKind(ctx, opID, dest, backup, models.RestorePendingKindClean))
	gf = p3Ledger(t, repo, opID)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"a name once proven unowned never re-enters the removal path")
}

// Unknown kinds are rejected before any transaction opens; validation errors
// keep the wave-19 shapes (empty/unparsable/zero opID, missing row).
func TestRevertLog_MarkReplacementRestorePendingKindW21_ErrorLegs(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	dest := "/dst/W21-MARK-ERR/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"

	require.Error(t, rl.MarkReplacementRestorePendingKind(ctx, "1", dest, backup, "future-kind"),
		"unknown kinds are never persisted")
	require.Error(t, rl.MarkReplacementRestorePendingKind(ctx, "", dest, backup, models.RestorePendingKindClean))
	require.Error(t, rl.MarkReplacementRestorePendingKind(ctx, "nan", dest, backup, models.RestorePendingKindRearmRefused))
	require.Error(t, rl.MarkReplacementRestorePendingKind(ctx, "0", dest, backup, models.RestorePendingKindClean))
	require.Error(t, rl.MarkReplacementRestorePendingKind(ctx, "424242", dest, backup, models.RestorePendingKindRearmRefused),
		"missing row surfaces")

	opID := beginP3Op(t, rl, "W21-MARK-ERR")
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))
	require.Error(t, rl.MarkReplacementRestorePendingKind(ctx, opID, dest, backup, ""),
		"the empty (legacy) selector is not a persistence kind")
	gf := p3Ledger(t, repo, opID)
	require.False(t, gf.Replacements[0].RestorePending, "rejected kinds mutate nothing")
}

// The no-op log accepts the kind-carrying mark silently (same no-op contract
// as its sibling mutators).
func TestNoOpRevertLog_MarkReplacementRestorePendingKindW21(t *testing.T) {
	l := NewRevertLogFromConfig(nil, NewRevertLogConfig(true, nil), "j", nil, nil, nil, nil)
	require.NoError(t, l.MarkReplacementRestorePendingKind(context.Background(), "1", "/d", "/b", models.RestorePendingKindClean))
}
