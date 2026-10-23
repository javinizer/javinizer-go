package history

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestHistoryRevertReportsMixedFilesystemOutcomes(t *testing.T) {
	path, db := setupRevertMissDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	ctx := context.Background()
	jobRepo := database.NewJobRepository(db)
	const batchID = "mixed-filesystem-outcomes"
	require.NoError(t, jobRepo.Create(ctx, &models.Job{ID: batchID, Status: models.JobStatusOrganized, TotalFiles: 2, StartedAt: time.Now().UTC()}))
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "blocked-parent")
	require.NoError(t, os.WriteFile(blockedParent, []byte("preserved parent"), 0600))
	library := filepath.Join(dir, "library")
	require.NoError(t, os.Mkdir(library, 0700))
	installed := filepath.Join(library, "installed.mp4")
	require.NoError(t, os.WriteFile(installed, []byte("preserved video"), 0600))
	failed := &models.BatchFileOperation{BatchJobID: batchID, MovieID: "FAILED-001", OriginalPath: filepath.Join(blockedParent, "source.mp4"), NewPath: installed, OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied}
	skipped := &models.BatchFileOperation{BatchJobID: batchID, MovieID: "SKIPPED-001", OriginalPath: filepath.Join(dir, "source.mp4"), NewPath: filepath.Join(library, "missing.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied}
	repo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, repo.Create(ctx, failed))
	require.NoError(t, repo.Create(ctx, skipped))
	// The real filesystem produces this portable error without requiring chmod
	// restrictions (which would be ineffective when tests run as root).
	mkdirErr := os.MkdirAll(blockedParent, config.DirPerm)
	require.Error(t, mkdirErr)
	var commandErr error
	output, _ := testutil.CaptureOutput(t, func() { commandErr = runHistoryRevert(&cobra.Command{}, []string{batchID}, path) })
	require.NoError(t, commandErr)
	require.Contains(t, output, "Reverted batch "+batchID+": 0 succeeded, 1 skipped, 1 failed")
	require.Contains(t, output, fmt.Sprintf("Movie FAILED-001: failed to recreate original directory: %v", mkdirErr))
	require.Contains(t, output, "Movie SKIPPED-001: skipped (anchor_missing)")
	persistedFailed, err := repo.FindByID(ctx, failed.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusFailed, persistedFailed.RevertStatus)
	persistedSkipped, err := repo.FindByID(ctx, skipped.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusApplied, persistedSkipped.RevertStatus)
	job, err := jobRepo.FindByID(ctx, batchID)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusOrganized, job.Status)
	require.Nil(t, job.RevertedAt)
	for name, want := range map[string]string{blockedParent: "preserved parent", installed: "preserved video"} {
		got, err := os.ReadFile(name)
		require.NoError(t, err)
		require.Equal(t, want, string(got))
	}
	require.NoFileExists(t, skipped.OriginalPath)
}
