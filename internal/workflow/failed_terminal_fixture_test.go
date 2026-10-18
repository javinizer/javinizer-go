package workflow

import (
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// seedFailedHistoryStatus exercises Undo against a synthetic history-status
// variant after fully published filesystem work. It is not failed-Apply or
// compensation coverage: successful terminal completion cannot be changed by
// CompleteFailed. Preserve the exact successful journal and ownership proofs.
func seedFailedHistoryStatus(t *testing.T, repo *database.BatchFileOperationRepository, id OperationID) {
	t.Helper()
	recordID, err := strconv.ParseUint(id, 10, 64)
	require.NoError(t, err)
	before, err := repo.FindByID(t.Context(), uint(recordID))
	require.NoError(t, err)
	require.NotNil(t, before)
	require.Equal(t, models.RevertStatusApplied, before.RevertStatus)
	ledger, err := models.ParseGeneratedFiles(before.GeneratedFiles)
	require.NoError(t, err)
	require.NotNil(t, ledger.ApplyTerminal)
	require.Equal(t, "success", ledger.ApplyTerminal.Outcome)
	require.False(t, ledger.PublicationPending)
	require.False(t, ledger.UndoStarted)

	require.NoError(t, repo.UpdateRevertStatus(t.Context(), uint(recordID), models.RevertStatusFailed))
	after, err := repo.FindByID(t.Context(), uint(recordID))
	require.NoError(t, err)
	require.NotNil(t, after)
	require.Equal(t, models.RevertStatusFailed, after.RevertStatus)
	require.Equal(t, before.GeneratedFiles, after.GeneratedFiles, "synthetic status must preserve the exact successful terminal journal")
	want := *before
	want.RevertStatus = models.RevertStatusFailed
	want.UpdatedAt = after.UpdatedAt
	require.Equal(t, want, *after, "only status and its repository-managed update timestamp may change")
}
