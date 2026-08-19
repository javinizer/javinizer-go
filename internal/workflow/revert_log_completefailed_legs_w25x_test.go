package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// W25x coverage close-out for CompleteFailed legs that no prior test reached:
// the junk-opID early return, the FolderPath→OldDirectoryPath source-dir
// fallback, and the FoundNFOPath assignment.

func TestW25X_CompleteFailedJunkOpIDReturnsNil(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	require.NoError(t, rl.CompleteFailed(context.Background(), "not-a-number", &ApplyResult{
		Movie: &models.Movie{ID: "JUNK-001"},
	}), "a non-parseable opID is a documented no-op")
}

func TestW25X_CompleteFailedEmptySourceDirFallsBackToOldDirectoryPath(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(repo, nil, "job-w25x", nil, nil, nil, nil)

	var txPersisted string
	var saved models.BatchFileOperation
	repo.On("FindByID", mock.Anything, uint(21)).Return(&models.BatchFileOperation{
		ID: 21, BatchJobID: "job-w25x", MovieID: "W25X-001",
		RevertStatus: models.RevertStatusApplied,
		// Deliberately EMPTY: exercises the FolderPath -> OldDirectoryPath
		// fallback when assigning sourceDir.
		OriginalDirPath: "",
	}, nil)
	runFnMock(repo, 21, &models.BatchFileOperation{ID: 21, RevertStatus: models.RevertStatusApplied}, &txPersisted)
	repo.On("UpdateNonJournalFields", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).
		Run(func(args mock.Arguments) { saved = *args.Get(1).(*models.BatchFileOperation) }).Return(nil)

	err := log.CompleteFailed(context.Background(), "21", &ApplyResult{
		Movie:        &models.Movie{ID: "W25X-001"},
		FoundNFOPath: "/found/w25x.nfo",
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:          "/dst/w25x.mp4",
			FolderPath:       "/dst/w25x",
			OldDirectoryPath: "/old/w25x",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "/found/w25x.nfo", saved.NFOPath, "FoundNFOPath fills an empty NFO field")
	assert.Equal(t, "/old/w25x", saved.OriginalDirPath, "empty sourceDir falls back to OldDirectoryPath")
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}
