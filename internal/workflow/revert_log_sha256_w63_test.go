package workflow

// POSTER-WRITE-HARDENING wave-63 (codex P2 PR#215) — RecordReplacement
// threads the arm-time BackupSHA256 into the journaled entry alongside the
// wave-25 size+mtime facts; it round-trips through persistence and marshals
// under the backup_sha256 key, while a variadic omission stays byte-identical
// to the pre-wave-63 legacy blob.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestRevertLogW63_RecordReplacementStampsSHA256(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "W63SHA")
	dest := "/dst/W63SHA/poster.jpg"
	const wantSHA = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, dest+".b",
		models.ReplacementBackupFacts{Size: 4321, ModUnix: 1_700_000_007, SHA256: wantSHA}))

	gf := p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, wantSHA, gf.Replacements[0].BackupSHA256,
		"the sha256 stamp round-trips through the persisted journal")
	stamped := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{gf.Replacements[0]}})
	require.Contains(t, stamped, "backup_sha256", "a sha-stamped entry serializes the key")

	// Variadic omission records a legacy-shape entry: no sha, byte-identical to
	// a pre-wave-63 blob (the field stays omitempty).
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, dest+".c"))
	gf = p3Ledger(t, repo, opID)
	require.Empty(t, gf.Replacements[1].BackupSHA256, "variadic omission leaves the sha unstamped")
	legacyBlob := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{gf.Replacements[1]}})
	require.NotContains(t, legacyBlob, "backup_sha256", "unstamped entries serialize without the key")
}
