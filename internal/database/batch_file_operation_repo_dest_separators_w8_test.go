package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-8 (codex P2 follow-up on wave-7's dest
// prefilter): the bounded fallback's LIKE patterns must cover BOTH Windows
// separator spellings — a journal written with backslashes must stay
// prefilter-visible to a forward-slash caller (and vice versa), while LIKE
// metacharacters in the raw path stay literal through the rewrite+escape
// pipeline.

// forceWindowsSeparatorsW8 pins the probe-aware destination key to the
// Windows separator policy on this host so the exact in-process matcher folds
// both spellings to one key, exactly as Windows production does.
func forceWindowsSeparatorsW8(t *testing.T) {
	t.Helper()
	prev := fsutil.PathBackslashesAreSeparators
	fsutil.PathBackslashesAreSeparators = true
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = prev })
}

// seedDestJournalRowW8 inserts one operation journaling a replacement for
// dest (spelling kept verbatim) and returns its row id.
func seedDestJournalRowW8(t *testing.T, repo *BatchFileOperationRepository, movieID, dest string, seq int64) uint {
	t.Helper()
	op := &models.BatchFileOperation{
		BatchJobID: "job-w8-sep", MovieID: movieID,
		OriginalPath: "/src/" + movieID + ".mkv", NewPath: "/lib/" + movieID + ".mkv",
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: dest,
				Backup:      dest + ".dlbak.0123456789abcdef",
				DestSeq:     seq,
			}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op.ID
}

// maxJournaledSeqW8 mirrors the caller-side nextDestSequence computation:
// max journaled DestSeq across matching rows (the next sequence is max+1).
func maxJournaledSeqW8(t *testing.T, rows []models.BatchFileOperation) int64 {
	t.Helper()
	var max int64
	for _, row := range rows {
		gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, err)
		for _, rep := range gf.Replacements {
			if rep.DestSeq > max {
				max = rep.DestSeq
			}
		}
	}
	return max
}

// TestFindOperationsByDestinationW8_BackslashJournalFoundBySlashQuery: a row
// journaled with the Windows backslash spelling is found when the caller
// queries the forward-slash spelling (and the derived sequence floor rides
// the rescued row).
func TestFindOperationsByDestinationW8_BackslashJournalFoundBySlashQuery(t *testing.T) {
	forceWindowsSeparatorsW8(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W8-BSLASH", `C:\Media\poster.jpg`, 7)
	seedDestJournalRowW8(t, repo, "W8-DECOY", `C:\Other\poster.jpg`, 99)

	rows, err := repo.FindOperationsByDestination(ctx, "C:/Media/poster.jpg")
	require.NoError(t, err)
	require.Len(t, rows, 1, "the backslash-journaled row must survive the cross-spelled fallback prefilter")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(7), maxJournaledSeqW8(t, rows), "the caller's nextDestSequence (+1) derives from the rescued row")
}

// TestFindOperationsByDestinationW8_SlashJournalFoundByBackslashQuery: the
// reverse direction — a slash-journaled row answers a backslash query.
func TestFindOperationsByDestinationW8_SlashJournalFoundByBackslashQuery(t *testing.T) {
	forceWindowsSeparatorsW8(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	id := seedDestJournalRowW8(t, repo, "W8-SLASH", "D:/Media/poster.jpg", 4)
	seedDestJournalRowW8(t, repo, "W8-DECOY", "D:/Other/poster.jpg", 42)

	rows, err := repo.FindOperationsByDestination(ctx, `D:\Media\poster.jpg`)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the slash-journaled row must survive the cross-spelled fallback prefilter")
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(4), maxJournaledSeqW8(t, rows))
}

// TestFindOperationsByDestinationW8_LikeMetacharactersStayLiteralAfterSeparatorRewrite:
// LIKE-escaping runs AFTER separator rewriting on each raw variant, so a '_'
// or '%' inside the destination can never wildcard a lookalike row into the
// candidate set.
func TestFindOperationsByDestinationW8_LikeMetacharactersStayLiteralAfterSeparatorRewrite(t *testing.T) {
	forceWindowsSeparatorsW8(t)
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	// Real match: dest carries a literal '_' — the escaped pattern must still
	// fetch it through its backslash cross-spelling.
	realID := seedDestJournalRowW8(t, repo, "W8-META", `C:\Media\pos_er.jpg`, 3)
	// Decoy differing ONLY at the metacharacter position — a raw '_' in the
	// variant pattern would single-char-wildcard this row into the candidate
	// set if escaping were lost during the separator rewrite.
	seedDestJournalRowW8(t, repo, "W8-METADECOY", `C:\Media\posXer.jpg`, 8)
	// '%' decoy — an unescaped '%' would let "100" prefix-match "10001".
	seedDestJournalRowW8(t, repo, "W8-PCTDECOY", `E:\Media\10001\poster.jpg`, 6)

	rows, err := repo.FindOperationsByDestination(ctx, "C:/Media/pos_er.jpg")
	require.NoError(t, err)
	require.Len(t, rows, 1, "the metachar row itself still matches exactly")
	require.Equal(t, realID, rows[0].ID)

	// The fallback fetch itself must stay escaped: only the literal-'_' row is
	// hydrated — the posXer lookalike never becomes a candidate.
	candidates, err := repo.findDestinationCandidates(ctx, "C:/Media/pos_er.jpg")
	require.NoError(t, err)
	require.Len(t, candidates, 1, "'_' stays a literal through rewrite+escape")
	require.Equal(t, realID, candidates[0].ID)

	// '%' is literal too: the 10001 decoy is not a candidate for a "100%"
	// query, and nothing else matches that destination at all.
	percentCandidates, err := repo.findDestinationCandidates(ctx, "E:/Media/100%/poster.jpg")
	require.NoError(t, err)
	require.Empty(t, percentCandidates, "'%' stays a literal through rewrite+escape")
}

// TestDestinationLikePatternsW8_BoundedVariants pins the pattern generator:
// separator-free destinations are byte-identical to wave-7 behavior, single-
// separator classes dedupe their no-op rewrite, mixed spellings produce both
// cross-forms, and the set never exceeds caller + two extras.
func TestDestinationLikePatternsW8_BoundedVariants(t *testing.T) {
	require.Equal(t, []string{destinationLikePattern("poster.jpg")}, destinationLikePatterns("poster.jpg"),
		"no separator → exactly the wave-7 single pattern")

	require.Equal(t,
		[]string{destinationLikePattern("a/b/c.jpg"), destinationLikePattern(`a\b\c.jpg`)},
		destinationLikePatterns("a/b/c.jpg"),
		"caller spelling first, slash→backslash cross-spelling second, no-op rewrite deduped")

	require.Equal(t,
		[]string{destinationLikePattern(`a\b\c.jpg`), destinationLikePattern("a/b/c.jpg")},
		destinationLikePatterns(`a\b\c.jpg`),
		"backslash→slash cross-spelling with the no-op rewrite deduped")

	require.Equal(t,
		[]string{
			destinationLikePattern(`a/b\c.jpg`),
			destinationLikePattern(`a\b\c.jpg`),
			destinationLikePattern("a/b/c.jpg"),
		},
		destinationLikePatterns(`a/b\c.jpg`),
		"mixed spellings generate both cross-forms, capped at two extra patterns")
}
