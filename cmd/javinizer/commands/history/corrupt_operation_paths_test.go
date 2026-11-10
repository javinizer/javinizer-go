package history

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestHistoryCommandsRejectUnreadableBatchOperations(t *testing.T) {
	path, db := setupRevertMissDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	const batchID = "corrupt-operation"
	createOrganizedJob(t, db, batchID)
	operation := &models.BatchFileOperation{BatchJobID: batchID, MovieID: "TEST-001", OriginalPath: "/original", NewPath: "/organized", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied}
	require.NoError(t, database.NewBatchFileOperationRepository(db).Create(t.Context(), operation))
	// SQLite permits text in a BOOLEAN column. A damaged or externally edited
	// ledger must be reported rather than displayed as empty or marked reverted.
	require.NoError(t, db.Exec("UPDATE batch_file_operations SET in_place_renamed = ? WHERE id = ?", "not-a-boolean", operation.ID).Error)
	for _, tc := range []struct {
		name, prefix string
		run          func(*cobra.Command) error
	}{
		{"list", "failed to load operations:", func(cmd *cobra.Command) error { return runHistoryListBatch(cmd, batchID, path) }},
		{"revert", "failed to revert batch:", func(cmd *cobra.Command) error { return runHistoryRevert(cmd, []string{batchID}, path) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			err := tc.run(cmd)
			require.ErrorContains(t, err, tc.prefix)
			require.ErrorContains(t, err, "not-a-boolean")
			job, err := database.NewJobRepository(db).FindByID(t.Context(), batchID)
			require.NoError(t, err)
			require.Equal(t, models.JobStatusOrganized, job.Status)
			require.Nil(t, job.RevertedAt)
			var status string
			require.NoError(t, db.Raw("SELECT revert_status FROM batch_file_operations WHERE id = ?", operation.ID).Scan(&status).Error)
			require.Equal(t, string(models.RevertStatusApplied), status)
		})
	}
}

func TestHistoryCommandsReportMissingRevertStatusColumn(t *testing.T) {
	path, db := setupRevertMissDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	const batchID = "incomplete-ledger"
	createOrganizedJob(t, db, batchID)
	// Simulate an externally damaged ledger schema. SELECT * can still read the
	// empty ledger, but status-filtered counts must not turn query failures into
	// zero counts and falsely report a completed revert.
	require.NoError(t, db.Exec("DROP INDEX idx_bfo_batch_job_revert_status").Error)
	require.NoError(t, db.Exec("ALTER TABLE batch_file_operations DROP COLUMN revert_status").Error)
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	err = runHistoryListBatch(cmd, batchID, path)
	require.ErrorContains(t, err, "failed to count reverted operations:")
	require.ErrorContains(t, err, "no such column: revert_status")
	cmd.Flags().String("scrape-ids", "TEST-001", "")
	var revertErr error
	output := captureMiss2Output(t, func() {
		revertErr = runHistoryRevert(cmd, []string{batchID}, path)
	})
	require.NoError(t, revertErr)
	require.Contains(t, output, "Failed to verify revert completion:")
	require.Contains(t, output, "no such column: revert_status")
	job, err := database.NewJobRepository(db).FindByID(t.Context(), batchID)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusOrganized, job.Status)
	require.Nil(t, job.RevertedAt)
}
