package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Delays the actual store lock after the DB fence has admitted publication.
// The old Atomic method reproduces the original bug; the staged seam exercises
// the identical scheduling point without making candidate state observable.
type blockedCleanupUpdater struct {
	resultstore.Store
	entered chan struct{}
	release chan struct{}
}

func (u *blockedCleanupUpdater) AtomicUpdateFileResultWithProvenance(path string, update func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	close(u.entered)
	<-u.release
	return u.Store.AtomicUpdateFileResultWithProvenance(path, update)
}

func (u *blockedCleanupUpdater) AtomicUpdateFileResultWithProvenanceAfter(path string, transaction func(func() error) error, update func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	return u.Store.AtomicUpdateFileResultWithProvenanceAfter(path, func(prepare func() error) error {
		return transaction(func() error {
			close(u.entered)
			<-u.release
			return prepare()
		})
	}, update)
}

func TestApplyFailureWritebackDiscardsExpiredTransaction(t *testing.T) {
	for _, mode := range []string{"before-store-lock", "after-prepare"} {
		t.Run(mode, func(t *testing.T) {
			inputs, store, db, path := newFailureWritebackFixture(t)
			before, beforeCounts := store.SnapshotForStatus()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			fenceCtx := make(chan context.Context, 1)
			repo := database.NewMovieRepository(db)
			inputs.MovieRepo = &cleanupFenceRepository{MovieRepositoryInterface: repo,
				fence: func(writeCtx context.Context, id string, gen int64, publish func(*models.Movie) error) error {
					fenceCtx <- writeCtx
					return repo.WithApplyPublicationFence(writeCtx, id, gen, func(movie *models.Movie) error {
						err := publish(movie)
						if mode == "after-prepare" {
							close(entered)
							<-release
						}
						return err
					})
				}}
			if mode == "before-store-lock" {
				inputs.Updater = &blockedCleanupUpdater{Store: store, entered: entered, release: release}
			}
			done := make(chan applyFileOutcome, 1)
			go func() { done <- interpretCleanupFailure(ctx, inputs, path) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("publication was not admitted")
			}
			// Synchronize on actual expiry rather than sleeping for an estimate.
			cleanupCtx := <-fenceCtx
			<-cleanupCtx.Done()
			require.ErrorIs(t, cleanupCtx.Err(), context.DeadlineExceeded)
			writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
			defer writeCancel()
			original := before.Results[path].Movie
			require.NoError(t, db.WithContext(writeCtx).Model(&models.Movie{}).Where("content_id = ?", original.ContentID).
				UpdateColumn("render_generation", original.RenderGeneration+1).Error, "the expired transaction must have released its DB writer")
			unblock()
			select {
			case outcome := <-done:
				assert.True(t, outcome.Cancelled)
			case <-time.After(time.Second):
				t.Fatal("cleanup did not return after releasing the callback")
			}
			after, afterCounts := store.SnapshotForStatus()
			t.Logf("generation advanced after actual cleanup expiry; result revision %d -> %d, status %s -> %s", before.Results[path].Revision, after.Results[path].Revision, before.Results[path].Status, after.Results[path].Status)
			assert.Equal(t, before, after, "rolled-back transaction must never expose provisional result/provenance/revision")
			assert.Equal(t, beforeCounts, afterCounts)
		})
	}
}

func TestApplyFailureWritebackDiscardsTransactionOutcomeFailure(t *testing.T) {
	inputs, store, db, path := newFailureWritebackFixture(t)
	before, counts := store.SnapshotForStatus()
	repo := database.NewMovieRepository(db)
	inputs.MovieRepo = &cleanupFenceRepository{MovieRepositoryInterface: repo,
		fence: func(ctx context.Context, id string, gen int64, publish func(*models.Movie) error) error {
			return repo.WithApplyPublicationFence(ctx, id, gen, func(movie *models.Movie) error {
				if err := publish(movie); err != nil {
					return err
				}
				return errors.New("transaction commit refused")
			})
		}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.True(t, interpretCleanupFailure(ctx, inputs, path).Cancelled)
	after, afterCounts := store.SnapshotForStatus()
	assert.Equal(t, before, after)
	assert.Equal(t, counts, afterCounts)
}

func TestApplyFailureWritebackDoesNotInvertDatabaseStoreLocks(t *testing.T) {
	inputs, store, db, path := newFailureWritebackFixture(t)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	held, err := pool.Conn(context.Background())
	require.NoError(t, err)
	defer held.Close()
	entered := make(chan struct{})
	repo := database.NewMovieRepository(db)
	inputs.MovieRepo = &cleanupFenceRepository{MovieRepositoryInterface: repo,
		fence: func(ctx context.Context, id string, gen int64, publish func(*models.Movie) error) error {
			close(entered)
			return repo.WithApplyPublicationFence(ctx, id, gen, publish)
		}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan applyFileOutcome, 1)
	go func() { done <- interpretCleanupFailure(ctx, inputs, path) }()
	<-entered
	readDone := make(chan struct{})
	go func() { _ = store.SnapshotData(); close(readDone) }()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("DB wait must not hold the store lock needed by another transaction's envelope snapshot")
	}
	require.NoError(t, held.Close())
	select {
	case outcome := <-done:
		assert.True(t, outcome.Cancelled)
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish after the DB connection became available")
	}
}

// An older updater is still usable without a DB fence, but must never be used
// as an unfenced fallback for a persisted movie's detached cleanup.
type legacyCleanupUpdater struct{ resultstore.ResultUpdater }

func TestApplyFailureWritebackUnsupportedAndMissingRowsFailClosed(t *testing.T) {
	for _, mode := range []string{"unsupported", "missing-row"} {
		t.Run(mode, func(t *testing.T) {
			inputs, store, _, path := newFailureWritebackFixture(t)
			if mode == "unsupported" {
				inputs.Updater = &legacyCleanupUpdater{ResultUpdater: store}
			} else {
				store.LoadResultsRaw(nil, nil)
			}
			before, counts := store.SnapshotForStatus()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			assert.True(t, interpretCleanupFailure(ctx, inputs, path).Cancelled)
			after, afterCounts := store.SnapshotForStatus()
			assert.Equal(t, before, after)
			assert.Equal(t, counts, afterCounts)
		})
	}
}

func TestApplyFailureWritebackDiscardsNativeCommitFailure(t *testing.T) {
	inputs, store, db, path := newFailureWritebackFixture(t)
	before, counts := store.SnapshotForStatus()
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	setHook := func(hook func() int) {
		conn, err := pool.Conn(context.Background())
		require.NoError(t, err)
		require.NoError(t, conn.Raw(func(raw any) error {
			raw.(*sqlite3.SQLiteConn).RegisterCommitHook(hook)
			return nil
		}))
		require.NoError(t, conn.Close())
	}
	commits := 0
	setHook(func() int { commits++; return 1 })
	t.Cleanup(func() { setHook(nil) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.True(t, interpretCleanupFailure(ctx, inputs, path).Cancelled)
	assert.Equal(t, 1, commits, "actual SQLite COMMIT must be rejected after prepare succeeds")
	after, afterCounts := store.SnapshotForStatus()
	assert.Equal(t, before, after)
	assert.Equal(t, counts, afterCounts)
	setHook(nil)
	assert.True(t, interpretCleanupFailure(ctx, inputs, path).Cancelled)
	retried, err := store.GetMovieResult(path)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCancelled, retried.Status, "rollback must release the writer and store lock for retry")
}

func TestStagedApplyFailureFenceFallbacksAndStaleOutcome(t *testing.T) {
	for _, mode := range []string{"nil-movie", "blank-content-id", "unpersisted-missing", "persisted-missing", "stale-after-prepare"} {
		t.Run(mode, func(t *testing.T) {
			inputs, store, db, path := newFailureWritebackFixture(t)
			movie := inputs.Results[path].Movie.Clone()
			var afc *ApplyFileContext
			switch mode {
			case "nil-movie":
				movie = nil
			case "blank-content-id":
				movie.ContentID = " "
			case "unpersisted-missing", "persisted-missing":
				require.NoError(t, db.Where("content_id = ?", movie.ContentID).Delete(&models.Movie{}).Error)
				if mode == "persisted-missing" {
					afc = &ApplyFileContext{PersistedMovie: true, PublicationGeneration: movie.RenderGeneration}
				}
			case "stale-after-prepare":
				repo := database.NewMovieRepository(db)
				inputs.MovieRepo = &cleanupFenceRepository{MovieRepositoryInterface: repo,
					fence: func(ctx context.Context, id string, gen int64, publish func(*models.Movie) error) error {
						return repo.WithApplyPublicationFence(ctx, id, gen, func(live *models.Movie) error {
							if err := publish(live); err != nil {
								return err
							}
							return database.ErrApplyPublicationStale
						})
					}}
			}
			before, beforeCounts := store.SnapshotForStatus()
			err := updateApplyFailureWithPublicationFence(context.Background(), inputs, movie, afc, path,
				func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
					current.Status = models.JobStatusFailed
					return current, prov, nil
				})
			after, counts := store.SnapshotForStatus()
			if mode == "persisted-missing" {
				require.ErrorIs(t, err, errApplyPublicationFence)
			} else {
				require.NoError(t, err)
			}
			if mode == "persisted-missing" || mode == "stale-after-prepare" {
				assert.Equal(t, before, after)
				assert.Equal(t, beforeCounts, counts)
			} else {
				assert.Equal(t, models.JobStatusFailed, after.Results[path].Status)
				assert.Equal(t, before.Results[path].Revision+1, after.Results[path].Revision)
				assert.Equal(t, 1, counts.Failed)
			}
		})
	}
}
