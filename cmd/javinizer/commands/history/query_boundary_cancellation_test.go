package history

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"
)

// Observe completed real SQL rather than returning synthetic database errors.
// LogMode must retain the observer when database.New configures its own logger.
type queryBoundaryLogger struct {
	gormlogger.Interface
	after func(context.Context, string, int64, error)
}

func (l queryBoundaryLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return queryBoundaryLogger{Interface: l.Interface.LogMode(level), after: l.after}
}

func (l queryBoundaryLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
	l.after(ctx, sql, rows, err)
}

func cancelAfterHistoryQuery(t *testing.T, ctx context.Context, cancel context.CancelFunc, match func(string) bool) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	original := gormlogger.Default
	// These tests deliberately remain serial: GORM's default logger is global.
	gormlogger.Default = queryBoundaryLogger{Interface: original, after: func(queryCtx context.Context, sql string, _ int64, err error) {
		if queryCtx == ctx && err == nil && match(sql) {
			hits.Add(1)
			cancel()
		}
	}}
	t.Cleanup(func() { gormlogger.Default = original })
	return &hits
}

func setupQueryBoundaryHistory(t *testing.T) (string, *database.DB) {
	t.Helper()
	path, db := setupMiss2TestDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	return path, db
}

func TestHistoryQueryBoundaryCleanupCancellation(t *testing.T) {
	path, db := setupQueryBoundaryHistory(t)
	repo := database.NewHistoryRepository(db)
	old := &models.History{MovieID: "OLD-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: time.Now().Add(-365 * 24 * time.Hour)}
	recent := &models.History{MovieID: "RECENT-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: time.Now()}
	require.NoError(t, repo.Create(context.Background(), old))
	require.NoError(t, repo.Create(context.Background(), recent))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hits := cancelAfterHistoryQuery(t, ctx, cancel, func(sql string) bool {
		return strings.HasPrefix(sql, "DELETE FROM `history`")
	})
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.Flags().Int("days", 30, "")
	var runErr error
	output := captureMiss2Output(t, func() { runErr = runHistoryClean(cmd, nil, path) })
	require.EqualValues(t, 1, hits.Load(), "cancel only after the real cleanup DELETE succeeds")
	require.ErrorIs(t, runErr, context.Canceled)
	require.ErrorContains(t, runErr, "failed to count records")
	require.NotContains(t, output, "Cleaned up")
	require.NotContains(t, output, "Remaining:")
	require.NotContains(t, output, "No records older")
	// Cleanup committed before cancellation; only the following count failed.
	records, err := repo.List(context.Background(), 10, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, recent.ID, records[0].ID)
}

func TestHistoryQueryBoundaryBatchStatusCancellation(t *testing.T) {
	for _, selectiveRevert := range []bool{false, true} {
		name := "list_pending_count"
		if selectiveRevert {
			name = "revert_failed_count"
		}
		t.Run(name, func(t *testing.T) {
			path, db := setupQueryBoundaryHistory(t)
			jobRepo := database.NewJobRepository(db)
			opRepo := database.NewBatchFileOperationRepository(db)
			batchID := "query-boundary-" + name
			job := &models.Job{ID: batchID, Status: models.JobStatusOrganized, TotalFiles: 1, StartedAt: time.Now().UTC()}
			require.NoError(t, jobRepo.Create(context.Background(), job))
			dir := t.TempDir()
			op := &models.BatchFileOperation{BatchJobID: batchID, MovieID: "MOV-001", OriginalPath: filepath.Join(dir, "source.mp4"), NewPath: filepath.Join(dir, "installed.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusReverted, GeneratedFiles: "[]"}
			require.NoError(t, opRepo.Create(context.Background(), op))
			beforeJob, err := jobRepo.FindByID(context.Background(), batchID)
			require.NoError(t, err)
			beforeOps, err := opRepo.FindByBatchJobID(context.Background(), batchID)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			status := string(models.RevertStatusReverted)
			if selectiveRevert {
				status = string(models.RevertStatusApplied)
			}
			hits := cancelAfterHistoryQuery(t, ctx, cancel, func(sql string) bool {
				return strings.HasPrefix(sql, "SELECT count(*) FROM `batch_file_operations`") && strings.Contains(sql, `batch_job_id = "`+batchID+`"`) && strings.Contains(sql, `revert_status = "`+status+`"`)
			})
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			cmd.Flags().String("scrape-ids", op.MovieID, "")
			var runErr error
			output := captureMiss2Output(t, func() {
				if selectiveRevert {
					runErr = runHistoryRevert(cmd, []string{batchID}, path)
				} else {
					runErr = runHistoryListBatch(cmd, batchID, path)
				}
			})
			require.EqualValues(t, 1, hits.Load(), "cancel after the preceding status count completes successfully")
			if selectiveRevert {
				// The command reports count failures as warnings, without marking the job reverted.
				require.NoError(t, runErr)
				require.Contains(t, output, "Failed to verify revert completion:")
				require.Contains(t, output, "with status failed: context canceled")
			} else {
				require.ErrorIs(t, runErr, context.Canceled)
				require.ErrorContains(t, runErr, "failed to count pending operations")
				require.NotContains(t, output, "=== Batch Job:")
			}
			afterJob, err := jobRepo.FindByID(context.Background(), batchID)
			require.NoError(t, err)
			require.Equal(t, beforeJob, afterJob)
			afterOps, err := opRepo.FindByBatchJobID(context.Background(), batchID)
			require.NoError(t, err)
			require.Equal(t, beforeOps, afterOps, "status checks must not change the operation ledger")
		})
	}
}
