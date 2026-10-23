package history

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCanceledHistoryCommandsPreserveRecords(t *testing.T) {
	path, db := setupMiss2TestDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	repo := database.NewHistoryRepository(db)
	record := &models.History{MovieID: "TEST-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: time.Now().Add(-365 * 24 * time.Hour)}
	require.NoError(t, repo.Create(context.Background(), record))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		run  func(*cobra.Command, []string, string) error
		args []string
		want string
	}{
		{"list", runHistoryList, nil, "failed to retrieve history: find recent history: context canceled"},
		{"stats", runHistoryStats, nil, "failed to retrieve stats: count historys: context canceled"},
		{"movie", runHistoryMovie, []string{"TEST-001"}, "failed to retrieve history: find history for movie TEST-001: context canceled"},
		{"clean", runHistoryClean, nil, "failed to count records: count historys: context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			cmd.Flags().Int("days", 30, "")
			err := tc.run(cmd, tc.args, path)
			require.ErrorIs(t, err, context.Canceled)
			require.EqualError(t, err, tc.want)
			got, err := repo.FindByID(context.Background(), record.ID)
			require.NoError(t, err)
			require.Equal(t, record.MovieID, got.MovieID)
			require.Equal(t, record.Status, got.Status)
		})
	}
}

func TestHistoryCleanupDatabaseRejectionPreservesRecord(t *testing.T) {
	path, db := setupMiss2TestDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	repo := database.NewHistoryRepository(db)
	record := &models.History{MovieID: "protected", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: time.Now().Add(-365 * 24 * time.Hour)}
	require.NoError(t, repo.Create(context.Background(), record))
	// A database-enforced retention policy rejects deletion of this record.
	require.NoError(t, db.Exec(`CREATE TRIGGER retain_history BEFORE DELETE ON history WHEN OLD.movie_id = 'protected' BEGIN SELECT RAISE(ABORT, 'retention policy'); END`).Error)
	cmd := &cobra.Command{}
	cmd.Flags().Int("days", 30, "")
	err = runHistoryClean(cmd, nil, path)
	require.EqualError(t, err, "failed to clean up history: delete history older than date: retention policy")
	got, err := repo.FindByID(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, record.MovieID, got.MovieID)
	count, err := repo.Count(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestHistoryReadCommandsWithoutExplicitContext(t *testing.T) {
	path, db := setupMiss2TestDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	// Direct command callers may omit a context; these read-only paths must
	// use background context rather than passing nil into the database.
	require.NoError(t, runHistoryList(&cobra.Command{}, nil, path))
	require.EqualError(t, runHistoryRevert(&cobra.Command{}, []string{"missing"}, path), "batch job not found: missing")
	count, err := database.NewHistoryRepository(db).Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}
