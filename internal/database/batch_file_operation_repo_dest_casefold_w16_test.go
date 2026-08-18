package database

// POSTER-WRITE-HARDENING codex PR#215 wave-16 (P2) — "fall back when Unicode
// case variants are mixed": the wave-9 prefilter layered ToLower/ToUpper
// transports of the destination onto the LIKE patterns, but those only see
// all-lower and all-upper STORED spellings. A journal row stored with a
// MIXED-case non-ASCII spelling (`…\äÖ.jpg`) matched neither — SQLite's
// built-in LIKE folds ASCII letters only, so the row never hydrated and the
// sequence/conflict logic went blind to a live chain. Enumerating per-letter
// case spellings is exponential in the destination's cased non-ASCII letters,
// so wave-16 takes the pre-wave-7 full-ledger scan for any destination
// carrying a foldable non-ASCII letter instead (destinationLikePatterns →
// nil → findDestinationCandidates' FindOperationsWithLedger leg). These tests
// pin the rescue on sqlite with spellings the old variant set provably
// missed, plus the behavioral probe that the fallback engages ONLY for those
// destinations.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// findCandidateIDsW16 runs the UNEXPORTED candidate fetch and returns row IDs —
// the fallback probe below keys on whether rows with no pattern match still
// arrive (fallback hydrated the full ledger) or not (SQL prefilter narrowed).
func findCandidateIDsW16(t *testing.T, repo *BatchFileOperationRepository, destination string) []uint {
	t.Helper()
	candidates, err := repo.findDestinationCandidates(context.Background(), destination)
	require.NoError(t, err)
	ids := make([]uint, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	return ids
}

// Stored `äÖ` (lower+upper — mixed), requested `ÄÖ`: the pre-wave-16 variant
// set was {ÄÖ, äö} — the stored spelling is NEITHER, so the row only survives
// inside the full-ledger fallback.
func TestFindOperationsByDestinationW16_MixedCaseStoredSpellingFoundViaFallback(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W16-MIX", `D:\Media\äÖ.jpg`, 7)
	decoyID := seedDestJournalRowW8(t, repo, "W16-MIX-DECOY", `D:\Media\Other.jpg`, 42)

	require.Nil(t, destinationLikePatterns("D:/Media/ÄÖ.jpg"), "the mixed-case query engages the fallback")
	require.Contains(t, findCandidateIDsW16(t, repo, "D:/Media/ÄÖ.jpg"), decoyID,
		"the candidate fetch is the un-prefiltered scan (decoy hydrated, filtered later in-process)")

	rows, err := repo.FindOperationsByDestination(ctx, "D:/Media/ÄÖ.jpg")
	require.NoError(t, err)
	require.Len(t, rows, 1, "the mixed-case stored chain is found via the fallback")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(7), maxJournaledSeqW8(t, rows),
		"sequence/conflict logic sees the chain the variant prefilter provably missed")
}

// A mixed trio: THREE cased non-ASCII letters, flip-flopped between stored
// and requested spellings. Per-letter enumeration needs 2³ = 8 variants —
// past any sane bound — while ToLower/ToUpper supply only 3 of the 8; the
// fallback needs none of them.
func TestFindOperationsByDestinationW16_MixedTrioFoundViaFallback(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	// stored: Ä b Ö c (upper, ASCII-lower, upper, ASCII-lower)
	id := seedDestJournalRowW8(t, repo, "W16-TRIO", `E:\Lib\ÄbÖc.mkv`, 13)
	seedDestJournalRowW8(t, repo, "W16-TRIO-DECOY", `E:\Lib\plain.mkv`, 1)

	// requested: ä B ö C — every cased letter flipped
	rows, err := repo.FindOperationsByDestination(ctx, "e:/lib/äBöC.mkv")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(13), maxJournaledSeqW8(t, rows))
}

// The probe in BOTH directions from one database: the pure-ASCII query's
// candidate fetch stays SQL-narrowed (decoy never arrives → no fallback),
// while the foldable non-ASCII query hydrates the whole ledger.
func TestFindOperationsByDestinationW16_FallbackEngagesOnlyForNonASCIICased(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	chainID := seedDestJournalRowW8(t, repo, "W16-PROBE", `D:/Media/Poster.jpg`, 3)
	decoyID := seedDestJournalRowW8(t, repo, "W16-PROBE-DECOY", `E:/Lib/Other.mkv`, 9)

	// Pure-ASCII (and separator-only form changes): prefilter narrows — the
	// decoy never reaches hydration, so NO fallback ran.
	require.NotNil(t, destinationLikePatterns("d:/MEDIA/poster.jpg"))
	ids := findCandidateIDsW16(t, repo, "d:/MEDIA/poster.jpg")
	require.Contains(t, ids, chainID)
	require.NotContains(t, ids, decoyID,
		"pure-ASCII queries keep the SQL prefilter — the fallback must NOT engage")

	// One foldable non-ASCII rune anywhere flips the query to the full scan.
	require.Nil(t, destinationLikePatterns("d:/MEDIA/poster-ä.jpg"))
	require.Contains(t, findCandidateIDsW16(t, repo, "d:/MEDIA/poster-ä.jpg"), decoyID,
		"a foldable non-ASCII rune hydrates the full ledger (fallback engaged)")
}
