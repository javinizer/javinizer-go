package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestWordReplacementDB(t *testing.T) *DB {
	t.Helper()
	return newDatabaseTestDB(t)
}

func TestWordReplacementRepository_Create(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "F***", Replacement: "Fuck"}
	err := repo.Create(context.TODO(), wr)
	require.NoError(t, err)
	assert.NotZero(t, wr.ID)
}

func TestWordReplacementRepository_Upsert_Create(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "R**e", Replacement: "Rape"}
	err := repo.Upsert(context.TODO(), wr)
	require.NoError(t, err)
	assert.NotZero(t, wr.ID)

	found, err := repo.FindByOriginal(context.TODO(), "R**e")
	require.NoError(t, err)
	assert.Equal(t, "Rape", found.Replacement)
}

func TestWordReplacementRepository_Upsert_Update(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "R**e", Replacement: "Rape"}
	require.NoError(t, repo.Create(context.TODO(), wr))

	updated := &models.WordReplacement{Original: "R**e", Replacement: "Raped"}
	err := repo.Upsert(context.TODO(), updated)
	require.NoError(t, err)

	found, err := repo.FindByOriginal(context.TODO(), "R**e")
	require.NoError(t, err)
	assert.Equal(t, "Raped", found.Replacement)
	assert.Equal(t, wr.ID, found.ID)
}

func TestWordReplacementRepository_FindByOriginal(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.WordReplacement{Original: "D***k", Replacement: "Drunk"}))

	found, err := repo.FindByOriginal(context.TODO(), "D***k")
	require.NoError(t, err)
	assert.Equal(t, "Drunk", found.Replacement)

	_, err = repo.FindByOriginal(context.TODO(), "nonexistent")
	assert.Error(t, err)
}

func TestWordReplacementRepository_Delete(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.WordReplacement{Original: "K**l", Replacement: "Kill"}))

	err := repo.Delete(context.TODO(), "K**l")
	require.NoError(t, err)

	_, err = repo.FindByOriginal(context.TODO(), "K**l")
	assert.Error(t, err)
}

func TestWordReplacementRepository_DeleteByID(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "B***d", Replacement: "Blood"}
	require.NoError(t, repo.Create(context.TODO(), wr))

	err := repo.DeleteByID(context.TODO(), wr.ID)
	require.NoError(t, err)

	_, err = repo.FindByID(context.TODO(), wr.ID)
	assert.Error(t, err)
}

func TestWordReplacementRepository_GetReplacementMap(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.WordReplacement{Original: "F***", Replacement: "Fuck"}))
	require.NoError(t, repo.Create(context.TODO(), &models.WordReplacement{Original: "R**e", Replacement: "Rape"}))

	m, err := repo.GetReplacementMap(context.TODO())
	require.NoError(t, err)
	assert.Equal(t, "Fuck", m["F***"])
	assert.Equal(t, "Rape", m["R**e"])
	assert.Len(t, m, 2)
}

func TestWordReplacementRepository_GetReplacementMap_Empty(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	m, err := repo.GetReplacementMap(context.TODO())
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestIsDefaultWordReplacement(t *testing.T) {
	assert.True(t, IsDefaultWordReplacement("R**e"))
	assert.True(t, IsDefaultWordReplacement("F***"))
	assert.False(t, IsDefaultWordReplacement("nonexistent"))
	assert.False(t, IsDefaultWordReplacement(""))
}

func TestSeedDefaultWordReplacements(t *testing.T) {
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	SeedDefaultWordReplacements(context.TODO(), repo)

	list, err := repo.List(context.TODO())
	require.NoError(t, err)
	assert.NotEmpty(t, list)

	m, err := repo.GetReplacementMap(context.TODO())
	require.NoError(t, err)
	assert.Equal(t, "Rape", m["R**e"])
	assert.Equal(t, "Fuck", m["F***"])
}

func TestSeedDefaultWordReplacements_Idempotent(t *testing.T) {
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	SeedDefaultWordReplacements(context.TODO(), repo)
	count1, err := repo.List(context.TODO())
	require.NoError(t, err)

	SeedDefaultWordReplacements(context.TODO(), repo)
	count2, err := repo.List(context.TODO())
	require.NoError(t, err)

	assert.Equal(t, len(count1), len(count2), "seeding twice should not duplicate entries")
}
