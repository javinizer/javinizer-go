package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestApplyFailureWritebackWaitingOnRealStoreMutexNeverPublishesExpiredTransaction(t *testing.T) {
	inputs, store, db, path := newFailureWritebackFixture(t)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	// The generation update must wait for database/sql to finish rollback,
	// not race the canceled transaction's writer on another connection.
	pool.SetMaxOpenConns(1)
	before, counts := store.SnapshotForStatus()
	held, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	fenceContext := make(chan context.Context, 1)
	repo := database.NewMovieRepository(db)
	inputs.MovieRepo = &cleanupFenceRepository{MovieRepositoryInterface: repo, fence: func(ctx context.Context, id string, generation int64, publish func(*models.Movie) error) error {
		return repo.WithApplyPublicationFence(ctx, id, generation, func(m *models.Movie) error {
			go func() {
				holderDone <- store.AtomicUpdateFileResultWithProvenance(path, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
					close(held)
					<-release
					return current, prov, nil
				})
			}()
			<-held
			fenceContext <- ctx
			return publish(m)
		})
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan applyFileOutcome, 1)
	go func() { done <- interpretCleanupFailure(ctx, inputs, path) }()
	var writeCtx context.Context
	select {
	case writeCtx = <-fenceContext:
	case <-time.After(2 * time.Second):
		t.Fatal("fence not entered")
	}
	select {
	case <-writeCtx.Done():
	case <-time.After(6 * time.Second):
		t.Fatal("failure cleanup context did not expire")
	}
	require.ErrorIs(t, writeCtx.Err(), context.DeadlineExceeded)
	// Rollback may finish after cancellation. Give the generation write its
	// own budget only after the sole transaction connection is released.
	released, err := pool.Conn(t.Context())
	require.NoError(t, err)
	require.NoError(t, released.Close())
	advanceCtx, advanceCancel := context.WithTimeout(t.Context(), time.Second)
	defer advanceCancel()
	require.NoError(t, db.WithContext(advanceCtx).Model(&models.Movie{}).Where("content_id = ?", before.Results[path].Movie.ContentID).UpdateColumn("render_generation", 4).Error)
	unblock()
	select {
	case err := <-holderDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("store mutex holder did not return")
	}
	select {
	case result := <-done:
		require.True(t, result.Cancelled)
	case <-time.After(time.Second):
		t.Fatal("late cleanup did not return")
	}
	after, afterCounts := store.SnapshotForStatus()
	require.Equal(t, before, after)
	require.Equal(t, counts, afterCounts)
}
