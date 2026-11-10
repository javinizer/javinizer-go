package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCompleteFailedAcceptsRepositoryNilRecordWithoutMutation(t *testing.T) {
	for _, result := range []*ApplyResult{nil, {NFOPath: "/library/movie.nfo"}} {
		t.Run(fmt.Sprintf("partial=%t", result != nil), func(t *testing.T) {
			repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
			repo.On("FindByID", mock.Anything, uint(42)).Return((*models.BatchFileOperation)(nil), nil).Once()
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/library/movie.nfo", []byte("existing metadata"), 0600))
			log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "missing-record", fs, nil, nil, nil)
			require.NoError(t, log.CompleteFailed(context.Background(), "42", result))
			body, err := afero.ReadFile(fs, "/library/movie.nfo")
			require.NoError(t, err)
			require.Equal(t, "existing metadata", string(body))
			repo.AssertNotCalled(t, "UpdateJournalInTx", mock.Anything, mock.Anything, mock.Anything)
			repo.AssertNotCalled(t, "UpdateNonJournalFields", mock.Anything, mock.Anything)
		})
	}
}

func TestPersistNonJournalColumnsPreservesConcurrentRevertStatus(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	row := &models.BatchFileOperation{ID: 42, RevertStatus: models.RevertStatusApplied, GeneratedFiles: `{"delete":["/library/movie.nfo"]}`, NFOPath: "/library/movie.nfo"}
	before := *row
	repo.On("UpdateNonJournalFields", mock.Anything, row).Return(fmt.Errorf("concurrent completion: %w", database.ErrOperationRowReverted)).Once()
	log := &dbRevertLog{repo: repo}
	require.NoError(t, log.persistNonJournalColumns(context.Background(), "42", row, applyJournalState{raw: row.GeneratedFiles, status: row.RevertStatus}))
	before.RevertStatus = models.RevertStatusReverted
	require.Equal(t, before, *row, "only the in-memory revert status changes; completion payload and journal remain intact")
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateJournalInTx", mock.Anything, mock.Anything, mock.Anything)
}
