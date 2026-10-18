package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryFilteredRepository(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.AutoMigrate(&models.History{}))
	repo := NewHistoryRepository(db)
	ctx := context.Background()
	filter := HistoryFilter{MovieID: "A", Operation: models.HistoryOpScrape, Status: models.HistoryStatusFailed}
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(ctx, &models.History{MovieID: "A", Operation: filter.Operation, Status: filter.Status, CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC)}))
	}
	require.NoError(t, repo.Create(ctx, &models.History{MovieID: "B", Operation: filter.Operation, Status: filter.Status}))
	for _, limit := range []int{0, -1, 2} {
		rows, err := repo.ListFiltered(ctx, filter, limit, 1)
		require.NoError(t, err)
		want := 3
		if limit > 0 {
			want = 2
		}
		require.Len(t, rows, want)
		for i := 1; i < len(rows); i++ {
			assert.True(t, rows[i-1].CreatedAt.After(rows[i].CreatedAt))
		}
	}
	count, err := repo.CountFiltered(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	count, err = repo.CountFiltered(canceled, filter)
	assert.Zero(t, count)
	assert.ErrorIs(t, err, context.Canceled)
	rows, err := repo.ListFiltered(canceled, filter, 2, 0)
	assert.Nil(t, rows)
	assert.ErrorIs(t, err, context.Canceled)
	require.NoError(t, db.Close())
	_, err = repo.CountFiltered(ctx, filter)
	require.Error(t, err)
	_, err = repo.ListFiltered(ctx, filter, 2, 0)
	require.Error(t, err)
}
