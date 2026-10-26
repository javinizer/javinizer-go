package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplacementBackupCandidatesMalformedNonceUsesCompleteProjection(t *testing.T) {
	repo := backupCandidateRepo(t)
	// Neither of these valid ledgers mentions the requested malformed name.
	// Returning only a nonce match could falsely authorize shared-backup cleanup.
	firstRaw := backupCandidateJSON("/first.dlbak.0123456789abcdef")
	secondRaw := backupCandidateJSON("/second.dlbak.fedcba9876543210")
	firstID := seedBackupCandidate(t, repo, firstRaw)
	secondID := seedBackupCandidate(t, repo, secondRaw)
	seedBackupCandidate(t, repo, "") // Non-ledger rows are still outside the projection.
	for _, name := range []string{
		"/x.dlbak.0123456789abcdeg",  // Correct length but not hexadecimal.
		"/x.dlbak.0123456789abcdef.", // A present ordinal must not be empty.
	} {
		t.Run(name, func(t *testing.T) {
			require.Empty(t, replacementBackupNonce(name))
			rows, err := repo.FindReplacementBackupCandidates(t.Context(), name)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			require.Equal(t, firstID, rows[0].ID)
			require.Equal(t, secondID, rows[1].ID)
			require.Equal(t, firstRaw, rows[0].GeneratedFiles)
			require.Equal(t, secondRaw, rows[1].GeneratedFiles)
			for _, row := range rows {
				require.Empty(t, row.NFOSnapshot, "fallback must retain the narrow projection")
				require.Empty(t, row.NewPath)
				require.Empty(t, row.BatchJobID)
			}
		})
	}
}
