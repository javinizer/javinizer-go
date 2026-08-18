package workflow

// POSTER-WRITE-HARDENING codex PR#215 wave-19 (P2) — the workflow-side
// journal-marking helper the downloader's refused re-arm rides:
// dbRevertLog.MarkReplacementRestorePending converts an armed journal entry
// into the rearm-refused restore-pending state inside the row's journal
// transaction, with the downloader seam's exact-spelling matching and
// idempotent no-change merges — the payload the history retry routes on
// (finding 1's state machine).

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestRevertLog_MarkReplacementRestorePendingW19_MarksRearmRefusedKind(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "W19-MARK")
	dest := "/dst/W19-MARK/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))

	require.NoError(t, rl.MarkReplacementRestorePending(ctx, opID, dest, backup))

	gf := p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 1, "the entry stays journaled — it converts, not retracts")
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].RestorePendingKind)

	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	require.Contains(t, row.GeneratedFiles, `"restore_pending":true,"restore_pending_kind":"rearm_refused"`,
		"the persisted payload carries the wave-19 kind field")

	// Idempotent: the identical re-mark is a no-change merge, still nil.
	require.NoError(t, rl.MarkReplacementRestorePending(ctx, opID, dest, backup))
	gf = p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind())

	// Missing entry: tolerated (idempotent), exactly like ReleaseReplacement.
	require.NoError(t, rl.MarkReplacementRestorePending(ctx, opID, dest+"/other", backup+".other"))
	gf = p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 1, "unmatched marks leave the ledger untouched")
}

// A legacy clean-pending entry (no kind — the wave-18 payload) UPGRADES to
// the rearm-refused kind; nothing ever downgrades back to clean.
func TestRevertLog_MarkReplacementRestorePendingW19_UpgradesLegacyCleanNeverDowngrades(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "W19-UPG")
	dest := "/dst/W19-UPG/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))

	// Seed the LEGACY clean marker through the stored row (the wave-18 shape).
	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindClean))
	row.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(ctx, row))
	require.NotContains(t, row.GeneratedFiles, "restore_pending_kind", "the clean kind never materializes")

	require.NoError(t, rl.MarkReplacementRestorePending(ctx, opID, dest, backup))
	gf = p3Ledger(t, repo, opID)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"clean upgrades to rearm-refused")

	// The marked entry has no downgrade path: a second mark is a no-op and the
	// kind survives any later legitimate mutation of the entry untouched.
	require.NoError(t, rl.MarkReplacementRestorePending(ctx, opID, dest, backup))
	gf = p3Ledger(t, repo, opID)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind())
}

func TestRevertLog_MarkReplacementRestorePendingW19_ErrorLegs(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	dest := "/dst/W19-ERR/poster.jpg"
	backup := dest + ".dlbak.x"

	require.Error(t, rl.MarkReplacementRestorePending(ctx, "", dest, backup), "empty opID")
	require.Error(t, rl.MarkReplacementRestorePending(ctx, "nan", dest, backup), "unparsable opID")
	require.Error(t, rl.MarkReplacementRestorePending(ctx, "0", dest, backup), "record id 0")
	require.Error(t, rl.MarkReplacementRestorePending(ctx, "424242", dest, backup), "missing row surfaces")

	// A malformed persisted ledger refuses the merge rather than dropping it.
	opID := beginP3Op(t, rl, "W19-ERR")
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))
	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	row.GeneratedFiles = `{"replacements":definitely-not-json`
	require.NoError(t, repo.Update(ctx, row))
	require.Error(t, rl.MarkReplacementRestorePending(ctx, opID, dest, backup))

	// Repository outage on the transaction surfaces.
	flaky := &failUpdateBFORepo{repo: repo, err: errors.New("w19 outage")}
	broken := NewDBRevertLog(flaky, NewRevertLogConfig(true, nil), "job-x", nil, nil, nil, nil)
	row.GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
		Destination: dest, Backup: backup, DestSeq: 1,
	}}})
	require.NoError(t, repo.Update(ctx, row))
	require.Error(t, broken.MarkReplacementRestorePending(ctx, opID, dest, backup))
	flaky.err = nil
	require.NoError(t, broken.MarkReplacementRestorePending(ctx, opID, dest, backup), "healed repo marks cleanly")
	gf := p3Ledger(t, repo, opID)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind())
}

// The no-op log accepts the mark silently (no durable store — the same
// no-op contract as its sibling mutators).
func TestNoOpRevertLog_MarkReplacementRestorePendingW19(t *testing.T) {
	l := NewRevertLogFromConfig(nil, NewRevertLogConfig(true, nil), "j", nil, nil, nil, nil)
	require.NoError(t, l.MarkReplacementRestorePending(context.Background(), "1", "/d", "/b"))
}
