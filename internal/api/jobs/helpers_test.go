package jobs

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// setupJobsTestDeps creates in-memory SQLite DB and ServerDependencies for jobs handler tests.
// It sets up JobQueue (needed by revert handlers) but NOT Reverter (set separately via setupJobsTestDepsWithReverter).
// Caller must defer db.Close().
func setupJobsTestDeps(t *testing.T) (*ServerDependencies, *database.DB) {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
		Output: config.OutputConfig{
			AllowRevert: true, // Enable revert for handler tests
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate())

	jobRepo := database.NewJobRepository(db)
	batchFileOpRepo := database.NewBatchFileOperationRepository(db)

	// JobQueue is needed by revert handlers (GetJobPointer call)
	jobQueue := worker.NewJobQueue(jobRepo, "data/temp", nil)

	deps := &ServerDependencies{
		DB:              db,
		JobRepo:         jobRepo,
		BatchFileOpRepo: batchFileOpRepo,
		JobQueue:        jobQueue,
		Runtime:         nil, // not needed for handler tests
	}
	deps.SetConfig(cfg)

	return deps, db
}

// seedJobsData creates 3 Job records (organized, completed, reverted) and
// associated BatchFileOperation records for the organized job (3 ops: 2 pending, 1 reverted).
// Returns the organized job ID for use in tests.
func seedJobsData(t *testing.T, deps *ServerDependencies) string {
	t.Helper()

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	// Job 1: organized (the one with operations)
	organizedJobID := uuid.New().String()
	organizedJob := &models.Job{
		ID:          organizedJobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  3,
		Completed:   3,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/organized",
		StartedAt:   twoHoursAgo,
		OrganizedAt: &oneHourAgo,
	}
	require.NoError(t, deps.JobRepo.Create(organizedJob))

	// BatchFileOperations for the organized job
	ops := []*models.BatchFileOperation{
		{
			BatchJobID:    organizedJobID,
			MovieID:       "ABC-001",
			OriginalPath:  "/src/ABC-001.mp4",
			NewPath:       "/dest/ABC-001.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			BatchJobID:    organizedJobID,
			MovieID:       "ABC-002",
			OriginalPath:  "/src/ABC-002.mp4",
			NewPath:       "/dest/ABC-002.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			BatchJobID:    organizedJobID,
			MovieID:       "ABC-003",
			OriginalPath:  "/src/ABC-003.mp4",
			NewPath:       "/dest/ABC-003.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusReverted,
			RevertedAt:    &now,
		},
	}
	for _, op := range ops {
		require.NoError(t, deps.BatchFileOpRepo.Create(op))
	}

	// Job 2: completed (no operations)
	completedJobID := uuid.New().String()
	completedJob := &models.Job{
		ID:          completedJobID,
		Status:      string(models.JobStatusCompleted),
		TotalFiles:  5,
		Completed:   4,
		Failed:      1,
		Progress:    0.8,
		Destination: "/dest/completed",
		StartedAt:   oneHourAgo,
		CompletedAt: &now,
	}
	require.NoError(t, deps.JobRepo.Create(completedJob))

	// Job 3: reverted
	revertedJobID := uuid.New().String()
	revertedJob := &models.Job{
		ID:          revertedJobID,
		Status:      string(models.JobStatusReverted),
		TotalFiles:  2,
		Completed:   2,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/reverted",
		StartedAt:   twoHoursAgo,
		RevertedAt:  &now,
	}
	require.NoError(t, deps.JobRepo.Create(revertedJob))

	return organizedJobID
}

// createTestJob is a minimal helper to create a single job with given status and no operations.
func createTestJob(t *testing.T, deps *ServerDependencies, status string) *models.Job {
	t.Helper()

	job := &models.Job{
		ID:          uuid.New().String(),
		Status:      status,
		TotalFiles:  0,
		Completed:   0,
		Failed:      0,
		Progress:    0,
		Destination: "/dest/test",
		StartedAt:   time.Now(),
	}
	require.NoError(t, deps.JobRepo.Create(job))
	return job
}

// setupJobsTestDepsWithReverter extends setupJobsTestDeps by also creating a Reverter
// backed by an in-memory filesystem. This is needed for revert endpoint tests.
func setupJobsTestDepsWithReverter(t *testing.T) (*ServerDependencies, *database.DB, afero.Fs) {
	t.Helper()

	deps, db := setupJobsTestDeps(t)

	memFs := afero.NewMemMapFs()
	reverter := history.NewReverter(memFs, deps.BatchFileOpRepo)
	deps.Reverter = reverter

	return deps, db, memFs
}

// seedRevertableJob creates an organized job with move-type BatchFileOperation records
// and seeds the files into the MemMapFs so the Reverter can find and move them.
// Returns the job ID.
func seedRevertableJob(t *testing.T, deps *ServerDependencies, fs afero.Fs, movieIDs []string) string {
	t.Helper()

	jobID := uuid.New().String()
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Create the organized job
	job := &models.Job{
		ID:          jobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  len(movieIDs),
		Completed:   len(movieIDs),
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest",
		StartedAt:   oneHourAgo,
		OrganizedAt: &now,
	}
	require.NoError(t, deps.JobRepo.Create(job))

	// Create operations + seed files in MemMapFs
	for i, movieID := range movieIDs {
		srcDir := filepath.Dir("/src/" + movieID + ".mp4")
		dstDir := filepath.Dir("/dest/" + movieID + "/" + movieID + ".mp4")

		require.NoError(t, fs.MkdirAll(srcDir, 0777))
		require.NoError(t, fs.MkdirAll(dstDir, 0777))

		// The Reverter moves from NewPath back to OriginalPath.
		// So we need the file at NewPath in MemMapFs.
		dstPath := "/dest/" + movieID + "/" + movieID + ".mp4"
		require.NoError(t, afero.WriteFile(fs, dstPath, []byte("test-content-"+movieID), 0666))

		op := &models.BatchFileOperation{
			BatchJobID:    jobID,
			MovieID:       movieID,
			OriginalPath:  "/src/" + movieID + ".mp4",
			NewPath:       dstPath,
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		}
		require.NoError(t, deps.BatchFileOpRepo.Create(op))

		// Avoid unused variable warning for i
		_ = i
	}

	return jobID
}
