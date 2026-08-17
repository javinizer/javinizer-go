package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestW1CBatchFileOperationRepositoryInterfaceMethods(t *testing.T) {
	db := newDatabaseTestDB(t)
	var repo BatchFileOperationRepositoryInterface = NewBatchFileOperationRepository(db)
	ctx := context.Background()

	_, err := repo.FindOperationsByDestination(ctx, "/cov-w1c/interface-miss.jpg")
	require.NoError(t, err)
	_, err = repo.FindOperationsWithReplacements(ctx)
	require.NoError(t, err)
	_, err = repo.FindOperationsWithLedger(ctx)
	require.NoError(t, err)
}
