package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressRepositorySearchIncludesDMMID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 19244, JapaneseName: "安倍亜沙美"}
	require.NoError(t, repo.Create(context.Background(), actress))

	results, err := repo.Search(context.Background(), "19244")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, actress.ID, results[0].ID)

	paged, err := repo.SearchPaged(context.Background(), "19244", 10, 0)
	require.NoError(t, err)
	require.Len(t, paged, 1)
	require.Equal(t, actress.ID, paged[0].ID)

	sorted, err := repo.SearchPagedSorted(context.Background(), "19244", 10, 0, "name", "asc")
	require.NoError(t, err)
	require.Len(t, sorted, 1)
	require.Equal(t, actress.ID, sorted[0].ID)

	count, err := repo.CountSearch(context.Background(), "19244")
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}
