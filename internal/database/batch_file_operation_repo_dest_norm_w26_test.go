package database

// POSTER-WRITE-HARDENING codex PR#215 wave-26 (P2) — fall back for decomposed
// normalization variants. The wave-16 fallback gate (destinationLikePatterns
// → nil → full-ledger scan) engaged only for destinations carrying a CASED
// non-ASCII letter. A decomposed query — `e` + COMBINING ACUTE (the NFD
// spelling of é) — is ASCII letters plus a combining MARK: the case gate
// stayed dark, the bounded LIKE prefilter searched the journal for the
// literal NFD bytes, and a row journaled in NFC never hydrated, hiding the
// live chain from sequence allocation and revert conflict checks (exactly the
// wave-16 harm, through decomposition instead of case). The gate now also
// engages when the destination differs from either canonical form
// (hasNormalizationVariants): the unfiltered ledger scan hydrates the row and
// the in-process matcher — DestKey NFC-canonicalizes on normalization-
// insensitive roots — equates what SQL LIKE cannot.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// forceNormalizationVariantsW26Environment pins both destination-key probe
// seams to a deterministic normalization-insensitive (APFS/HFS+-style) root:
// DestKey then NFC-canonicalizes both spellings of one name, exactly like a
// production volume that aliases NFC/NFD. Nil probes are the documented
// forced-insensitive test postures (definitive, cached).
func forceNormalizationVariantsW26Environment(t *testing.T) {
	t.Helper()
	prevCase := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = nil
	prevNorm := fsutil.NormalizationProbe
	fsutil.NormalizationProbe = nil
	fsutil.ResetCaseSensitivityCache()
	fsutil.ResetNormalizationCache()
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = prevCase
		fsutil.NormalizationProbe = prevNorm
		fsutil.ResetCaseSensitivityCache()
		fsutil.ResetNormalizationCache()
	})
}

func TestHasNormalizationVariantsW26_Classifications(t *testing.T) {
	require.False(t, hasNormalizationVariants("d:/Media/MOVIE-001.jpg"), "pure ASCII is normalization-stable")
	require.False(t, hasNormalizationVariants(""), "empty")
	require.False(t, hasNormalizationVariants("/中/字.jpg"), "CJK has no canonical decomposition")
	require.True(t, hasNormalizationVariants("/lib/e\u0301.jpg"), "NFD (decomposed) spelling differs from its NFC form")
	require.True(t, hasNormalizationVariants("/lib/\u00e9.jpg"), "NFC precomposed spelling differs from its NFD expansion")
}

func TestDestinationLikePatternsW26_DecompositionGate(t *testing.T) {
	require.Nil(t, destinationLikePatterns("/w26/e\u0301.jpg"),
		"a decomposed query engages the full-ledger fallback")
	require.Nil(t, destinationLikePatterns("/w26/\u00e9.jpg"),
		"a precomposed spelling of a decomposable name engages the fallback too")
	require.NotNil(t, destinationLikePatterns("/w26/poster.jpg"),
		"pure ASCII keeps the bounded wave-8 patterns")
	require.NotNil(t, destinationLikePatterns("/w26/中.jpg"),
		"normalization-stable non-cased non-ASCII keeps the bounded patterns")
}

// The finding's headline shape: journal stored NFC, query arrives in NFD.
// Before wave-26 the LIKE prefilter searched the ledger for the literal
// decomposed bytes and the NFC-journaled row never hydrated.
func TestFindOperationsByDestinationW26_NFDQueryFindsNFCStoredRow(t *testing.T) {
	forceNormalizationVariantsW26Environment(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	storedNFC := "/w26lib/\u00e9.jpg"
	id := seedDestJournalRowW8(t, repo, "W26-NFC-STORED", storedNFC, 7)
	decoyID := seedDestJournalRowW8(t, repo, "W26-NORM-DECOY", "/w26other/plain.jpg", 42)

	queryNFD := "/w26lib/e\u0301.jpg" // "e" + COMBINING ACUTE — the NFD spelling of the same name
	require.Nil(t, destinationLikePatterns(queryNFD), "the NFD query's gate")
	require.Contains(t, findCandidateIDsW16(t, repo, queryNFD), decoyID,
		"the candidate fetch is the un-prefiltered scan (decoy hydrated, filtered later in-process)")

	rows, err := repo.FindOperationsByDestination(ctx, queryNFD)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the NFC-stored chain hydrates through the fallback")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(7), maxJournaledSeqW8(t, rows),
		"sequence/conflict logic sees the chain the literal prefilter provably missed")
}

// The mirror direction: a row journaled in NFD form, queried in NFC — the
// prefilter's literal bytes again cannot cross the decomposition.
func TestFindOperationsByDestinationW26_NFCQueryFindsNFDStoredRow(t *testing.T) {
	forceNormalizationVariantsW26Environment(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	storedNFD := "/w26lib/e\u0301.jpg" // NFD stored verbatim (a POSIX volume preserves the spelling)
	id := seedDestJournalRowW8(t, repo, "W26-NFD-STORED", storedNFD, 11)
	decoyID := seedDestJournalRowW8(t, repo, "W26-NFD-DECOY", "/w26other/plain.jpg", 5)

	queryNFC := "/w26lib/\u00e9.jpg"
	require.Nil(t, destinationLikePatterns(queryNFC), "the NFC query's gate")
	require.Contains(t, findCandidateIDsW16(t, repo, queryNFC), decoyID,
		"the full-ledger fallback engaged (decoy hydrated)")

	rows, err := repo.FindOperationsByDestination(ctx, queryNFC)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(11), maxJournaledSeqW8(t, rows))
}

// Pure-ASCII queries are normalization-stable: the gate must NOT widen them
// into the full-ledger scan — the prefilter stays SQL-narrow (decoy never
// hydrates) and the wave-8 bounded patterns apply unchanged.
func TestFindOperationsByDestinationW26_PureASCIIQueryKeepsBoundedPrefilter(t *testing.T) {
	forceNormalizationVariantsW26Environment(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	chainID := seedDestJournalRowW8(t, repo, "W26-ASCII", "/w26lib/poster.jpg", 3)
	decoyID := seedDestJournalRowW8(t, repo, "W26-ASCII-DECOY", "/w26other/other.jpg", 9)

	require.NotNil(t, destinationLikePatterns("/w26lib/poster.jpg"))
	ids := findCandidateIDsW16(t, repo, "/w26lib/poster.jpg")
	require.Contains(t, ids, chainID)
	require.NotContains(t, ids, decoyID,
		"the wave-26 gate never widens normalization-stable queries into the full-ledger scan")

	rows, err := repo.FindOperationsByDestination(context.Background(), "/w26lib/poster.jpg")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, chainID, rows[0].ID)
}
