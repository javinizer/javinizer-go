package commandutil

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryContentIDRepository_CreateAndFind(t *testing.T) {
	repo := newMemoryContentIDRepository()
	ctx := context.Background()

	err := repo.Create(ctx, &models.ContentIDMapping{SearchID: "MDB-087", ContentID: "61mdb087", Source: "dmm"})
	require.NoError(t, err)

	found, err := repo.FindBySearchID(ctx, "mdb-087")
	require.NoError(t, err)
	assert.Equal(t, "61mdb087", found.ContentID)
	assert.Equal(t, "MDB-087", found.SearchID)
}

func TestMemoryContentIDRepository_FindNotFound(t *testing.T) {
	repo := newMemoryContentIDRepository()
	_, err := repo.FindBySearchID(context.Background(), "NOPE")
	assert.Error(t, err)
}

func TestMemoryContentIDRepository_CreateNil(t *testing.T) {
	repo := newMemoryContentIDRepository()
	err := repo.Create(context.Background(), nil)
	assert.Error(t, err)
}

func TestMemoryContentIDRepository_Delete(t *testing.T) {
	repo := newMemoryContentIDRepository()
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{SearchID: "ABC-123", ContentID: "abc123", Source: "dmm"}))
	require.NoError(t, repo.Delete(ctx, "abc-123"))
	_, err := repo.FindBySearchID(ctx, "ABC-123")
	assert.Error(t, err)
}

func TestMemoryContentIDRepository_GetAll(t *testing.T) {
	repo := newMemoryContentIDRepository()
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{SearchID: "BBB-001", ContentID: "bbb1", Source: "dmm"}))
	require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{SearchID: "AAA-001", ContentID: "aaa1", Source: "dmm"}))
	all, err := repo.GetAll(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.Equal(t, "AAA-001", all[0].SearchID)
	assert.Equal(t, "BBB-001", all[1].SearchID)
}

func TestMemoryContentIDRepository_GetAllPaginated(t *testing.T) {
	repo := newMemoryContentIDRepository()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{SearchID: string(rune('A'+i)) + "-001", ContentID: "c", Source: "dmm"}))
	}
	page, err := repo.GetAllPaginated(ctx, 2, 1)
	require.NoError(t, err)
	assert.Len(t, page, 2)

	overflow, err := repo.GetAllPaginated(ctx, 10, 100)
	require.NoError(t, err)
	assert.Empty(t, overflow)
}

func TestMemoryContentIDRepository_GetAllChunked(t *testing.T) {
	repo := newMemoryContentIDRepository()
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{SearchID: "X-1", ContentID: "c1", Source: "dmm"}))
	require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{SearchID: "X-2", ContentID: "c2", Source: "dmm"}))
	all, err := repo.GetAllChunked(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}
