package database

// POSTER-WRITE-HARDENING codex PR#215 wave-20 (P2) — the final-sigma rescue:
// DestKey's insensitive form now runs FULL Unicode case folding
// (cases.Fold), so spellings simple lowering kept distinct — stored ς
// (final sigma), queried σ — resolve to one key. A foldable non-ASCII
// destination engages the wave-16 full-ledger fallback (no LIKE prefilter:
// SQLite folds ASCII only), and the in-process Fold matcher finds the chain
// through it. These tests pin the native-vs-stored mixed-spelling fetch:
// journalled ς-variant FOUND by σ/Σ-variant queries on the insensitive
// posture, with the decoy probe proving the fallback engaged. The wave-16
// pins live in batch_file_operation_repo_dest_casefold_w16_test.go.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The exact pair from the finding: stored final-sigma ς spelling, queried as
// plain σ — ToLower never folded these together, so the row was invisible to
// the exact matcher even after the wave-16 fallback hydrated it. Full fold
// resolves the pair; the fallback probe (decoy hydrated) proves which leg
// carried the fetch.
func TestFindOperationsByDestinationW20_FinalSigmaStoredFoundBySigmaQuery(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W20-SIGMA", `D:\Media\Movies\ςτ.jpg`, 21)
	decoyID := seedDestJournalRowW8(t, repo, "W20-SIGMA-DECOY", `D:\Media\Other.jpg`, 99)

	query := "D:/Media/Movies/στ.jpg" // σ-variant against the stored ς-variant
	require.Nil(t, destinationLikePatterns(query),
		"a foldable non-ASCII letter engages the wave-16 full-ledger fallback")
	require.Contains(t, findCandidateIDsW16(t, repo, query), decoyID,
		"behavioral probe: the un-prefiltered full-ledger scan hydrated the fetch")

	rows, err := repo.FindOperationsByDestination(ctx, query)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the ς-stored chain is FOUND by the σ-spelled query")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(21), maxJournaledSeqW8(t, rows),
		"sequence allocation sees the rescued chain")
}

// Mixed-spelling table: every native-vs-stored sigma combination folds onto
// the stored journal row, including all-uppercase Σ and a stored mixed
// flip-flop.
func TestFindOperationsByDestinationW20_SigmaSpellingTable(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W20-STABLE", `E:\Lib\ΣΤΟΥ.jpg`, 34)
	seedDestJournalRowW8(t, repo, "W20-STABLE-DECOY", `E:\Lib\plain.jpg`, 1)

	for _, query := range []string{
		`E:/Lib/στου.jpg`, // all-lower σ
		`E:/Lib/ςτου.jpg`, // final sigma at a NON-final position (byte-distinct, case-equivalent)
		`E:/Lib/ΣΤΟΥ.jpg`, // all-upper
		`e:/lIb/Στου.JPG`, // flip-flopped mixed case incl. ASCII rungs
		`E:\Lib\ςΤΟΥ.jpg`, // backslash + ς/ΣΤ mix
	} {
		require.Nil(t, destinationLikePatterns(query), "%q engages the fallback", query)
		rows, err := repo.FindOperationsByDestination(ctx, query)
		require.NoError(t, err, "query %q", query)
		require.Len(t, rows, 1, "query %q found exactly the stored row", query)
		require.Equal(t, id, rows[0].ID, "query %q", query)
		require.Equal(t, int64(34), maxJournaledSeqW8(t, rows), "query %q", query)
	}
}

// The gate itself: the final-sigma runes count as foldable non-ASCII (so the
// query takes the full-ledger fallback) — and the Fold pairing now resolves
// them there. Pure-ASCII and non-cased runes keep the patterned path.
func TestDestinationLikePatternsW20_FinalSigmaEngagesFallback(t *testing.T) {
	require.Nil(t, destinationLikePatterns("D:/Media/ς.jpg"), "lowercase final sigma is foldable")
	require.Nil(t, destinationLikePatterns(`D:\Media\σ.jpg`), "plain sigma is foldable")
	require.Nil(t, destinationLikePatterns("D:/Media/Σ.jpg"), "capital sigma is foldable")
	require.NotNil(t, destinationLikePatterns("D:/Media/POSTER.jpg"), "pure ASCII stays patterned")
	require.NotNil(t, destinationLikePatterns("D:/Media/中.jpg"), "non-cased runes stay patterned")
}

// Guard rail: fold-distinct spellings must NOT match through the same
// fallback — a sigma-spelled query does not fold onto a plain-ASCII-stored
// row, so the exact matcher still rejects non-equivalent ledger rows.
func TestFindOperationsByDestinationW20_NonEquivalentNonASCIIStaysInvisible(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	seedDestJournalRowW8(t, repo, "W20-NOMATCH", `D:\Media\Movies\other.jpg`, 5)

	rows, err := repo.FindOperationsByDestination(ctx, "D:/Media/Movies/στ.jpg")
	require.NoError(t, err)
	require.Empty(t, rows, "non-equivalent rows stay invisible through the fallback leg")
}
