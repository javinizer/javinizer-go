package database

import (
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
	err := repo.Create(wr)
	require.NoError(t, err)
	assert.NotZero(t, wr.ID)
}

func TestWordReplacementRepository_Upsert_Create(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "R**e", Replacement: "Rape"}
	err := repo.Upsert(wr)
	require.NoError(t, err)
	assert.NotZero(t, wr.ID)

	found, err := repo.FindByOriginal("R**e")
	require.NoError(t, err)
	assert.Equal(t, "Rape", found.Replacement)
}

func TestWordReplacementRepository_Upsert_Update(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "R**e", Replacement: "Rape"}
	require.NoError(t, repo.Create(wr))

	updated := &models.WordReplacement{Original: "R**e", Replacement: "Raped"}
	err := repo.Upsert(updated)
	require.NoError(t, err)

	found, err := repo.FindByOriginal("R**e")
	require.NoError(t, err)
	assert.Equal(t, "Raped", found.Replacement)
	assert.Equal(t, wr.ID, found.ID)
}

func TestWordReplacementRepository_FindByOriginal(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(&models.WordReplacement{Original: "D***k", Replacement: "Drunk"}))

	found, err := repo.FindByOriginal("D***k")
	require.NoError(t, err)
	assert.Equal(t, "Drunk", found.Replacement)

	_, err = repo.FindByOriginal("nonexistent")
	assert.Error(t, err)
}

func TestWordReplacementRepository_Delete(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(&models.WordReplacement{Original: "K**l", Replacement: "Kill"}))

	err := repo.Delete("K**l")
	require.NoError(t, err)

	_, err = repo.FindByOriginal("K**l")
	assert.Error(t, err)
}

func TestWordReplacementRepository_DeleteByID(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "B***d", Replacement: "Blood"}
	require.NoError(t, repo.Create(wr))

	err := repo.DeleteByID(wr.ID)
	require.NoError(t, err)

	_, err = repo.FindByID(wr.ID)
	assert.Error(t, err)
}

func TestWordReplacementRepository_GetReplacementMap(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(&models.WordReplacement{Original: "F***", Replacement: "Fuck"}))
	require.NoError(t, repo.Create(&models.WordReplacement{Original: "R**e", Replacement: "Rape"}))

	m, err := repo.GetReplacementMap()
	require.NoError(t, err)
	assert.Equal(t, "Fuck", m["F***"])
	assert.Equal(t, "Rape", m["R**e"])
	assert.Len(t, m, 2)
}

func TestWordReplacementRepository_GetReplacementMap_Empty(t *testing.T) {
	t.Parallel()
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	m, err := repo.GetReplacementMap()
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestDefaultWordReplacements(t *testing.T) {
	defaults := DefaultWordReplacements()
	assert.NotEmpty(t, defaults)

	for _, wr := range defaults {
		assert.NotEmpty(t, wr.Original, "default word replacement should have non-empty Original")
	}

	modified := defaults
	modified[0] = models.WordReplacement{Original: "MODIFIED", Replacement: "modified"}
	original := DefaultWordReplacements()
	assert.NotEqual(t, "MODIFIED", original[0].Original, "DefaultWordReplacements should return a copy")
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

	SeedDefaultWordReplacements(repo)

	list, err := repo.List()
	require.NoError(t, err)
	assert.NotEmpty(t, list)

	m, err := repo.GetReplacementMap()
	require.NoError(t, err)
	assert.Equal(t, "Rape", m["R**e"])
	assert.Equal(t, "Fuck", m["F***"])
}

func TestSeedDefaultWordReplacements_Idempotent(t *testing.T) {
	db := newTestWordReplacementDB(t)
	repo := NewWordReplacementRepository(db)

	SeedDefaultWordReplacements(repo)
	count1, err := repo.List()
	require.NoError(t, err)

	SeedDefaultWordReplacements(repo)
	count2, err := repo.List()
	require.NoError(t, err)

	assert.Equal(t, len(count1), len(count2), "seeding twice should not duplicate entries")
}
