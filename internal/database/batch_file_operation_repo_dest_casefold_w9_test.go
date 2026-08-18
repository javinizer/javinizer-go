package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-9 (codex P2 follow-up on wave-8's dest
// prefilter): SQLite's built-in LIKE folds ASCII case only, so a journal row
// stored under a differently-cased Unicode spelling (`…\Ä.jpg` vs `…/ä.jpg`)
// used to slip the bounded prefilter — the in-process DestKey matcher folds
// them together on an insensitive root, but the row was never fetched. The
// prefilter now generates Go-side ToLower/ToUpper transports of the full
// destination, hard-capped, with a full-ledger fallback on cap overflow.

// forceCaseFoldEnvironmentW9 pins BOTH destination-key seams for deterministic
// cross-platform runs: Windows separator policy (both spellings fold to one
// key) and a case-insensitive root probe (case folds without a host probe).
func forceCaseFoldEnvironmentW9(t *testing.T) {
	t.Helper()
	prevSep := fsutil.PathBackslashesAreSeparators
	fsutil.PathBackslashesAreSeparators = true
	prevProbe := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return false, nil }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.PathBackslashesAreSeparators = prevSep
		fsutil.CaseSensitiveProbe = prevProbe
		fsutil.ResetCaseSensitivityCache()
	})
}

func TestHasCaseFoldingNonASCIIW9(t *testing.T) {
	require.True(t, hasCaseFoldingNonASCII("D:/Media/Ä.jpg"), "UPPERCASE non-ASCII folds")
	require.True(t, hasCaseFoldingNonASCII("D:/Media/ä.jpg"), "lowercase non-ASCII folds")
	require.True(t, hasCaseFoldingNonASCII("é.E"), "mixed ASCII fold + non-ASCII")
	require.False(t, hasCaseFoldingNonASCII("D:/Media/MOVIE-001.jpg"), "pure ASCII needs no variants")
	require.False(t, hasCaseFoldingNonASCII("/中/字.jpg"), "CJK runes have no case fold")
	require.False(t, hasCaseFoldingNonASCII(""), "empty")
}

// TestDestinationLikePatternsW9_ASCIIByteIdentical re-pins wave-8 behavior:
// pure-ASCII destinations (and non-folding non-ASCII runes) never grow case
// variants.
func TestDestinationLikePatternsW9_ASCIIByteIdentical(t *testing.T) {
	require.Equal(t,
		[]string{destinationLikePattern("poster.jpg")},
		destinationLikePatterns("poster.jpg"))
	require.Equal(t,
		[]string{destinationLikePattern("a/b/c.jpg"), destinationLikePattern(`a\b\c.jpg`)},
		destinationLikePatterns("a/b/c.jpg"))
	require.Equal(t,
		[]string{destinationLikePattern("a/中.jpg"), destinationLikePattern(`a\中.jpg`)},
		destinationLikePatterns("a/中.jpg"), "non-folding non-ASCII keeps wave-8 patterns")
}

// TestDestinationLikePatternsW9_UnicodeCaseVariants: a differently-casable
// non-ASCII letter layers BOTH full-string case transports onto each
// separator spelling, deduped and bounded — caller casing first.
func TestDestinationLikePatternsW9_UnicodeCaseVariants(t *testing.T) {
	patterns := destinationLikePatterns("D:/Media/ä.jpg")
	require.NotNil(t, patterns, "the variant set stays under the hard cap")
	require.Len(t, patterns, 6, "3 case forms × 2 separator spellings")
	require.Equal(t, destinationLikePattern("D:/Media/ä.jpg"), patterns[0], "caller spelling first")
	require.Contains(t, patterns, destinationLikePattern(`D:\Media\ä.jpg`))
	require.Contains(t, patterns, destinationLikePattern("d:/media/ä.jpg"), "ToLower transport, slash spelling")
	require.Contains(t, patterns, destinationLikePattern(`d:\media\ä.jpg`), "ToLower transport, backslash spelling")
	require.Contains(t, patterns, destinationLikePattern("D:/MEDIA/Ä.JPG"), "ToUpper transport, slash spelling")
	require.Contains(t, patterns, destinationLikePattern(`D:\MEDIA\Ä.JPG`), "ToUpper transport names the stored uppercase form")

	// A case-idempotent destination adds only the differing transports.
	folded := destinationLikePatterns("d:/media/ä.jpg")
	require.Len(t, folded, 4, "ToLower of an all-lower destination dedupes away")
	require.NotContains(t, folded, destinationLikePattern("D:/Media/ä.jpg"), "the caller's exact casing appears once")
}

// TestDestinationLikePatternsW9_CapOverflowSignalsFallback: when the case ×
// separator fan-out exceeds the hard cap the generator returns nil rather
// than truncating a pattern that could name a live chain.
func TestDestinationLikePatternsW9_CapOverflowSignalsFallback(t *testing.T) {
	mixed := `C:/Mi\Xed/Ärger.jpg` // both separator classes + folding non-ASCII
	require.NotNil(t, destinationLikePatterns("C:/Mi"), "control: tiny destination generates patterns")
	require.Nil(t, destinationLikePatterns(mixed), "3 case forms × 3 separator spellings = 9 > cap → nil fallback")
}

// TestFindOperationsByDestinationW9_UnicodeCaseCrossSpelling: a row journaled
// with the uppercase backslash spelling is found by a lowercase slash query —
// fetched THROUGH the bounded pattern prefilter (not the full-ledger
// fallback), with a decoy row proving the prefilter still narrows.
func TestFindOperationsByDestinationW9_UnicodeCaseCrossSpelling(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W9-UAUT", `D:\Media\Ä.jpg`, 11)
	seedDestJournalRowW8(t, repo, "W9-UDECOY", `D:\Media\Poster.jpg`, 99)

	require.NotNil(t, destinationLikePatterns("D:/Media/ä.jpg"), "this destination stays under the cap — patterned path")
	candidates, err := repo.findDestinationCandidates(ctx, "D:/Media/ä.jpg")
	require.NoError(t, err)
	require.Len(t, candidates, 1, "the Unicode case variant prefilter fetches exactly the stored row")
	require.Equal(t, id, candidates[0].ID, "fetched through the pattern set, not a full scan (decoy excluded)")

	rows, err := repo.FindOperationsByDestination(ctx, "D:/Media/ä.jpg")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(11), maxJournaledSeqW8(t, rows), "sequence allocation rides the rescued row")
}

// TestFindOperationsByDestinationW9_CapOverflowFullLedgerFallback: a
// mixed-separator destination with folding non-ASCII overflows the pattern
// cap, and the candidate fetch falls back to the un-prefiltered full-ledger
// scan — the in-process exact match still finds the live chain while the
// decoy is rejected by the normalizer, never by a truncated prefilter.
func TestFindOperationsByDestinationW9_CapOverflowFullLedgerFallback(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W9-UOVER", `C:\Mi\Xed\ÄRGER.JPG`, 5)
	seedDestJournalRowW8(t, repo, "W9-UODECOY", `C:\Mi\Xed\Arger.jpg`, 8)

	query := `c:/MI\xed/ärger.jpg` // mixed separators, inverse casing
	require.Nil(t, destinationLikePatterns(query), "fan-out exceeds the cap → fallback engaged")

	rows, err := repo.FindOperationsByDestination(ctx, query)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the full-ledger fallback still finds the differently-spelled chain")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(5), maxJournaledSeqW8(t, rows), "conflict checks see the live chain")
}
