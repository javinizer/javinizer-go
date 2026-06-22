package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobRepository_Create(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{
		ID:         "test-job-1",
		Status:     models.JobStatusRunning,
		TotalFiles: 10,
		Completed:  5,
		Failed:     0,
		Progress:   50.0,
		Files:      `["file1.mp4","file2.mp4"]`,
		StartedAt:  time.Now(),
	}

	err := repo.Create(context.TODO(), job)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "test-job-1")
	require.NoError(t, err)
	assert.Equal(t, "test-job-1", found.ID)
	assert.Equal(t, 10, found.TotalFiles)
}

func TestJobRepository_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	now := time.Now()
	jobs := []*models.Job{
		{ID: "job-1", Status: models.JobStatusRunning, TotalFiles: 5, Files: "[]", StartedAt: now.Add(-1 * time.Hour)},
		{ID: "job-2", Status: models.JobStatusCompleted, TotalFiles: 3, Files: "[]", StartedAt: now},
	}

	for _, j := range jobs {
		require.NoError(t, repo.Create(context.TODO(), j))
	}

	list, err := repo.List(context.TODO())
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "job-2", list[0].ID)
}

func TestJobRepository_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{
		ID:         "to-delete",
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Files:      "[]",
		StartedAt:  time.Now(),
	}
	require.NoError(t, repo.Create(context.TODO(), job))

	err := repo.Delete(context.TODO(), "to-delete")
	require.NoError(t, err)

	_, err = repo.FindByID(context.TODO(), "to-delete")
	assert.Error(t, err)
}

func TestJobRepository_DeleteOrganizedOlderThan(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	now := time.Now()
	twoDaysAgo := now.Add(-48 * time.Hour)

	organizedOld := &models.Job{
		ID:          "organized-old",
		Status:      models.JobStatusOrganized,
		TotalFiles:  1,
		Files:       "[]",
		StartedAt:   twoDaysAgo.Add(-1 * time.Hour),
		OrganizedAt: &twoDaysAgo,
	}
	organizedRecent := &models.Job{
		ID:          "organized-recent",
		Status:      models.JobStatusOrganized,
		TotalFiles:  1,
		Files:       "[]",
		StartedAt:   now.Add(-1 * time.Hour),
		OrganizedAt: ptrTime(now.Add(-12 * time.Hour)),
	}

	require.NoError(t, repo.Create(context.TODO(), organizedOld))
	require.NoError(t, repo.Create(context.TODO(), organizedRecent))

	err := repo.DeleteOrganizedOlderThan(context.TODO(), now.Add(-24*time.Hour))
	require.NoError(t, err)

	list, err := repo.List(context.TODO())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "organized-recent", list[0].ID)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
