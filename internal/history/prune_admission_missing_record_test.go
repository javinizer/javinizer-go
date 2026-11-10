package history

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPruneAdmissionMissingRecordRetainsBackup(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	repo.On("FindByID", mock.Anything, uint(42)).Return((*models.BatchFileOperation)(nil), nil).Once()
	fs := afero.NewMemMapFs()
	const backup = "/library/poster.jpg.dlbak.0123456789abcdef"
	require.NoError(t, afero.WriteFile(fs, backup, []byte("recoverable original"), 0600))
	sweeper := NewReplacementSweeper(fs, repo)
	err := sweeper.markPruneCleanupPending(t.Context(), 42, models.ReplacementEntry{Destination: "/library/poster.jpg", Backup: backup})
	require.EqualError(t, err, "operation 42 missing during prune admission")
	body, err := afero.ReadFile(fs, backup)
	require.NoError(t, err)
	require.Equal(t, "recoverable original", string(body))
	repo.AssertNotCalled(t, "UpdateJournalInTx", mock.Anything, mock.Anything, mock.Anything)
}
