package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDBV2(t *testing.T) *DB {
	t.Helper()
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return db
}

// TestActressRepositoryListAllV2 tests ListAll
func TestActressRepositoryListAllV2(t *testing.T) {
	db := setupTestDBV2(t)
	defer func() { _ = db.Close() }()
	repo := NewActressRepository(db)

	// Create test actresses
	err := repo.Create(context.Background(), &models.Actress{JapaneseName: "Test Actress 1"})
	require.NoError(t, err)
	err = repo.Create(context.Background(), &models.Actress{JapaneseName: "Test Actress 2"})
	require.NoError(t, err)

	// ListAll should return all actresses
	actresses, err := repo.ListAll(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(actresses), 2)
}

// TestGenreReplacementRepositoryFindByIDV2 tests FindByID
func TestGenreReplacementRepositoryFindByIDV2(t *testing.T) {
	db := setupTestDBV2(t)
	defer func() { _ = db.Close() }()
	repo := NewGenreReplacementRepository(db)

	// Create a genre replacement
	gr := &models.GenreReplacement{Original: "test_genre", Replacement: "replaced_genre"}
	err := repo.Create(context.Background(), gr)
	require.NoError(t, err)
	require.NotZero(t, gr.ID)

	// FindByID should return the replacement
	found, err := repo.FindByID(context.Background(), gr.ID)
	require.NoError(t, err)
	assert.Equal(t, "test_genre", found.Original)
	assert.Equal(t, "replaced_genre", found.Replacement)
}

// TestGenreReplacementRepositoryDeleteByIDV2 tests DeleteByID
func TestGenreReplacementRepositoryDeleteByIDV2(t *testing.T) {
	db := setupTestDBV2(t)
	defer func() { _ = db.Close() }()
	repo := NewGenreReplacementRepository(db)

	// Create a genre replacement
	gr := &models.GenreReplacement{Original: "to_delete", Replacement: "deleted_genre"}
	err := repo.Create(context.Background(), gr)
	require.NoError(t, err)
	require.NotZero(t, gr.ID)

	// DeleteByID should succeed
	err = repo.DeleteByID(context.Background(), gr.ID)
	require.NoError(t, err)

	// Verify it's deleted
	_, err = repo.FindByID(context.Background(), gr.ID)
	assert.Error(t, err)
}

// TestJobRepositoryUpdateV2 tests Update
func TestJobRepositoryUpdateV2(t *testing.T) {
	db := setupTestDBV2(t)
	defer func() { _ = db.Close() }()
	repo := NewJobRepository(db)

	// Create a job
	job := &models.Job{ID: "test-job-update", Status: models.JobStatusPending, TotalFiles: 10}
	err := repo.Create(context.Background(), job)
	require.NoError(t, err)

	// Update the job
	job.Status = models.JobStatusCompleted
	err = repo.Update(context.Background(), job)
	require.NoError(t, err)

	// Verify update
	found, err := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, found.Status)
}

// TestJobRepositoryUpsertV2 tests Upsert
func TestJobRepositoryUpsertV2(t *testing.T) {
	db := setupTestDBV2(t)
	defer func() { _ = db.Close() }()
	repo := NewJobRepository(db)

	// Create a job first
	job := &models.Job{ID: "test-job-upsert", Status: models.JobStatusPending, TotalFiles: 10}
	err := repo.Create(context.Background(), job)
	require.NoError(t, err)

	// Upsert should update the existing record
	job.Status = models.JobStatusRunning
	err = repo.Upsert(context.Background(), job)
	require.NoError(t, err)

	found, err := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusRunning, found.Status)
}

// TestRepositoriesV2 tests Repositories function
func TestRepositoriesV2(t *testing.T) {
	db := setupTestDBV2(t)
	defer func() { _ = db.Close() }()
	repos := db.Repositories()
	require.NotNil(t, repos)
	assert.NotNil(t, repos.MovieRepo)
	assert.NotNil(t, repos.ActressRepo)
	assert.NotNil(t, repos.GenreRepo)
	assert.NotNil(t, repos.HistoryRepo)
	assert.NotNil(t, repos.EventRepo)
	assert.NotNil(t, repos.JobRepo)
	assert.NotNil(t, repos.ApiTokenRepo)
	assert.NotNil(t, repos.GenreReplacementRepo)
	assert.NotNil(t, repos.WordReplacementRepo)
	assert.NotNil(t, repos.ContentIDMappingRepo)
	assert.NotNil(t, repos.BatchFileOpRepo)
	assert.NotNil(t, repos.MovieTagRepo)
}
