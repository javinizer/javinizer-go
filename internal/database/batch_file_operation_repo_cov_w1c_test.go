package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestW1CBatchFileOperationRepositoryDestinationSkipsMalformedLedger(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	destination := "/cov-w1c/poster.jpg"

	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:     "cov-w1c-destination",
		MovieID:        "MALFORMED",
		OriginalPath:   "/source/movie.mkv",
		OperationType:  models.OperationTypeCopy,
		GeneratedFiles: `{"replacements":[{"destination":"/cov-w1c/poster.jpg","backup":`,
		RevertStatus:   models.RevertStatusApplied,
	}))

	// The SQL prefilter selects this row because the destination is present,
	// then the exact matcher must skip its malformed legacy JSON.
	rows, err := repo.FindOperationsByDestination(ctx, destination)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestW1CBatchFileOperationRepositoryFindOperationsWithReplacementsBranches(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:     "cov-w1c-replacements",
		MovieID:        "MALFORMED",
		OriginalPath:   "/source/malformed.mkv",
		OperationType:  models.OperationTypeCopy,
		GeneratedFiles: `{"replacements":broken`,
		RevertStatus:   models.RevertStatusApplied,
	}))
	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:     "cov-w1c-replacements",
		MovieID:        "EMPTY",
		OriginalPath:   "/source/empty.mkv",
		OperationType:  models.OperationTypeCopy,
		GeneratedFiles: `{"replacements":[]}`,
		RevertStatus:   models.RevertStatusApplied,
	}))
	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "cov-w1c-replacements",
		MovieID:       "VALID",
		OriginalPath:  "/source/valid.mkv",
		OperationType: models.OperationTypeCopy,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: "/cov-w1c/valid.jpg",
				Backup:      "/cov-w1c/valid.jpg.bak",
				DestSeq:     1,
			}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}))

	rows, err := repo.FindOperationsWithReplacements(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "VALID", rows[0].MovieID)
}

func TestW1CBatchFileOperationRepositoryFindOperationsWithReplacementsQueryError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.Close())

	_, err := repo.FindOperationsWithReplacements(context.Background())
	require.Error(t, err)
}
