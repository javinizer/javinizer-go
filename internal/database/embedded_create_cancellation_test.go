package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// These tests exercise cancellation diagnostics on the exposed embedded API,
// not production delegation. A pre-cancelled context must prevent any write;
// successful writes use concrete repositories and their domain invariants.
func TestActressEmbeddedCreateCancellationPreservesState(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	baseline := models.Actress{FirstName: "Existing", LastName: "Owner"}
	require.NoError(t, repo.Create(t.Context(), &baseline))
	snapshot := func() []models.Actress {
		t.Helper()
		var rows []models.Actress
		require.NoError(t, db.WithContext(t.Context()).Order("id ASC").Find(&rows).Error)
		return rows
	}
	before := snapshot()
	require.Len(t, before, 1)
	proposed := models.Actress{ID: 930071, FirstName: "Cancelled", LastName: "Proposal"}
	callerBefore := proposed
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := repo.BaseRepository.Create(ctx, &proposed)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualError(t, err, "create actress 930071: context canceled")
	require.Equal(t, callerBefore, proposed)
	require.Equal(t, before, snapshot())
	missing, err := repo.FindByID(t.Context(), proposed.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.Nil(t, missing)

	// Recovery deliberately uses concrete Create, never a generic success path.
	recovery := models.Actress{FirstName: "Recovered", LastName: "Owner"}
	require.NoError(t, repo.Create(t.Context(), &recovery))
	require.True(t, recovery.Verified)
	require.Equal(t, ActressOriginUser, recovery.Origin)
	loaded, err := repo.FindByID(t.Context(), recovery.ID)
	require.NoError(t, err)
	require.Equal(t, recovery, *loaded)
	require.Equal(t, append(before, recovery), snapshot())
}

func TestBatchFileOperationEmbeddedCreateCancellationPreservesState(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	baseline := models.BatchFileOperation{
		BatchJobID: "cancel-baseline", OriginalPath: "/source/existing.mp4", NewPath: "/destination/existing.mp4",
	}
	require.NoError(t, repo.Create(t.Context(), &baseline))
	snapshot := func() []models.BatchFileOperation {
		t.Helper()
		var rows []models.BatchFileOperation
		require.NoError(t, db.WithContext(t.Context()).Order("id ASC").Find(&rows).Error)
		return rows
	}
	before := snapshot()
	require.Len(t, before, 1)
	proposed := models.BatchFileOperation{
		ID: 930072, BatchJobID: "cancel-proposal", OriginalPath: "/source/proposed.mp4", NewPath: "/destination/proposed.mp4",
	}
	callerBefore := proposed
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := repo.BaseRepository.Create(ctx, &proposed)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualError(t, err, "create batch file operation 930072: context canceled")
	require.Equal(t, callerBefore, proposed)
	require.Equal(t, before, snapshot())
	missing, err := repo.FindByID(t.Context(), proposed.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.Nil(t, missing)

	// Concrete Create retains the atomic prune fence and default handling.
	recovery := models.BatchFileOperation{
		BatchJobID: "cancel-recovery", OriginalPath: "/source/recovered.mp4", NewPath: "/destination/recovered.mp4",
	}
	require.NoError(t, repo.Create(t.Context(), &recovery))
	require.Equal(t, models.OperationTypeMove, recovery.OperationType)
	require.Equal(t, models.RevertStatusApplied, recovery.RevertStatus)
	loaded, err := repo.FindByID(t.Context(), recovery.ID)
	require.NoError(t, err)
	require.Equal(t, recovery, *loaded)
	require.Equal(t, append(before, recovery), snapshot())
}
