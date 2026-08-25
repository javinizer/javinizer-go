package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func installStatusFailureTriggerP6(t *testing.T, db *database.DB) {
	t.Helper()
	require.NoError(t, db.DB.Exec(`CREATE TRIGGER p6_block_job_status BEFORE UPDATE OF status ON jobs BEGIN SELECT RAISE(ABORT, 'status update wedged'); END;`).Error)
}

func seedStatusFailureOperationP6(t *testing.T, db *database.DB, batchID, movieID string) {
	t.Helper()
	tmpDir := t.TempDir()
	oldDir := filepath.Join(tmpDir, "old")
	newDir := filepath.Join(tmpDir, "new")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.MkdirAll(newDir, 0o755))
	newPath := filepath.Join(newDir, movieID+".mp4")
	require.NoError(t, os.WriteFile(newPath, []byte("revert-me"), 0o644))
	batchRepo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, batchRepo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: batchID, MovieID: movieID,
		OriginalPath: filepath.Join(oldDir, movieID+".mp4"), NewPath: newPath,
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))
}

func TestRunHistoryRevert_StatusPersistFailureBranches(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "batch", args: []string{"p6-cli-status-batch"}},
		{name: "scrape", args: []string{"p6-cli-status-scrape", "--scrape-ids", "MOV-001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath, db := setupRevertMissDB(t)
			defer db.Close()
			createOrganizedJob(t, db, tc.args[0])
			seedStatusFailureOperationP6(t, db, tc.args[0], "MOV-001")
			installStatusFailureTriggerP6(t, db)

			rootCmd := &cobra.Command{Use: "root"}
			rootCmd.PersistentFlags().String("config", configPath, "config file")
			cmd := NewCommand()
			rootCmd.AddCommand(cmd)
			rootCmd.SetArgs(append([]string{"history", "revert"}, tc.args...))
			output := captureMissOutput(t, func() { require.NoError(t, rootCmd.Execute()) })
			require.Contains(t, output, "Failed to update job status")
		})
	}
}
