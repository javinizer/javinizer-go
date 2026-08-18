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
// them together on an insensitive root, but the row was never fetched.
//
// SUPERSEDED by wave-16 (codex P2): the wave-9 answer generated ToLower /
// ToUpper transports of the full destination, but all-lower and all-upper
// variants only rescue all-lower and all-upper STORED spellings — a
// mixed-case non-ASCII journal spelling (stored `äÖ` vs queried `ÄÖ`) still
// hid from the prefilter. destinationLikePatterns now returns NIL for any
// destination carrying a foldable non-ASCII letter and the candidate fetch
// takes the pre-wave-7 full-ledger scan; these tests re-pin the surviving
// wave-8 shape (pure-ASCII patterns) and the wave-16 fallback engagement.

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
// pure-ASCII destinations (and non-folding non-ASCII runes) engage the
// prefilter with exactly the wave-8 patterns — no fallback.
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

// TestDestinationLikePatternsW16_NonASCIICasedEngagesFallback: every spelling
// containing a foldable non-ASCII letter yields NO pattern set — the query
// takes the full-ledger fallback — because all-lower/all-upper variants
// cannot cover mixed-case stored spellings, and per-letter enumeration is
// exponential (SQLite LIKE folds ASCII only).
func TestDestinationLikePatternsW16_NonASCIICasedEngagesFallback(t *testing.T) {
	require.Nil(t, destinationLikePatterns("D:/Media/ä.jpg"),
		"a single foldable non-ASCII letter engages the fallback — no bounded variant set")
	require.Nil(t, destinationLikePatterns(`D:\Media\Ä.jpg`),
		"backslash spelling with a foldable rune engages it too")
	require.Nil(t, destinationLikePatterns("d:/media/äBÖ.mkv"),
		"multiple cased letters (exponential enumeration) engage it at any count")
	require.NotNil(t, destinationLikePatterns("D:/Media/MOVIE-001.mkv"),
		"control: pure-ASCII never falls back")
	require.NotNil(t, destinationLikePatterns("a/中.jpg"),
		"control: non-cased runes keep the patterned path")
}

// TestDestinationLikePatternsW16_PureASCIIMixedSeparatorsStayPatterned:
// mixed '/' + '\' pure-ASCII destinations still ride the schema of separator
// cross-spellings — never the fallback (pure-ASCII has no case gap).
func TestDestinationLikePatternsW16_PureASCIIMixedSeparatorsStayPatterned(t *testing.T) {
	patterns := destinationLikePatterns(`C:/Mi\Xed/Arger.jpg`)
	require.NotNil(t, patterns)
	require.Equal(t, []string{
		destinationLikePattern(`C:/Mi\Xed/Arger.jpg`),
		destinationLikePattern(`C:\Mi\Xed\Arger.jpg`),
		destinationLikePattern(`C:/Mi/Xed/Arger.jpg`),
	}, patterns, "three spellings, no case variants, no fallback")
	require.NotNil(t, destinationLikePatterns("C:/Mi"), "control: tiny destination generates patterns")
}

// TestFindOperationsByDestinationW9_UnicodeCaseCrossSpelling: a row journaled
// with the uppercase backslash spelling is found by a lowercase slash query.
// Wave-16: this rides the FULL-LEDGER fallback now — the behavioral probe
// proving it is the decoy ledger row, which shows up in the UN-prefiltered
// candidate fetch but is rejected by the caller's exact matcher.
func TestFindOperationsByDestinationW9_UnicodeCaseCrossSpelling(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W9-UAUT", `D:\Media\Ä.jpg`, 11)
	decoyID := seedDestJournalRowW8(t, repo, "W9-UDECOY", `D:\Media\Poster.jpg`, 99)

	require.Nil(t, destinationLikePatterns("D:/Media/ä.jpg"), "foldable non-ASCII → fallback, not a variant set")
	candidates, err := repo.findDestinationCandidates(ctx, "D:/Media/ä.jpg")
	require.NoError(t, err)
	ids := make([]uint, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	require.Contains(t, ids, decoyID,
		"behavioral probe: the un-prefiltered scan hydrates even nonmatching ledger rows")
	require.Contains(t, ids, id)

	rows, err := repo.FindOperationsByDestination(ctx, "D:/Media/ä.jpg")
	require.NoError(t, err)
	require.Len(t, rows, 1, "the in-process exact matcher keeps only the live chain")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(11), maxJournaledSeqW8(t, rows), "sequence allocation rides the rescued row")
}

// TestFindOperationsByDestinationW9_NonASCIIFullLedgerFallback: a mixed-separator
// destination with folding non-ASCII gets no prefilter, the candidate fetch
// falls back to the un-prefiltered full-ledger scan, and the in-process exact
// match still finds the live chain while the decoy is rejected by the
// normalizer, never by a pattern set.
func TestFindOperationsByDestinationW9_NonASCIIFullLedgerFallback(t *testing.T) {
	forceCaseFoldEnvironmentW9(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W9-UOVER", `C:\Mi\Xed\ÄRGER.JPG`, 5)
	seedDestJournalRowW8(t, repo, "W9-UODECOY", `C:\Mi\Xed\Arger.jpg`, 8)

	query := `c:/MI\xed/ärger.jpg` // mixed separators, inverse casing
	require.Nil(t, destinationLikePatterns(query), "foldable non-ASCII → fallback engaged")

	rows, err := repo.FindOperationsByDestination(ctx, query)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the full-ledger fallback still finds the differently-spelled chain")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(5), maxJournaledSeqW8(t, rows), "conflict checks see the live chain")
}
