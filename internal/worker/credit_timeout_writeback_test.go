package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type cleanupDeadlineWorkflow struct {
	stubApplyWorkflow
	started func()
}

func (s *cleanupDeadlineWorkflow) Apply(ctx context.Context, _ workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	if s.started != nil {
		s.started()
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func newFailureWritebackFixture(t *testing.T) (applyPhaseInputs, resultstore.Store, *database.DB, string) {
	t.Helper()
	db, err := database.New(&database.Config{
		Type: "sqlite", DSN: filepath.Join(t.TempDir(), "writeback.db") + "?_busy_timeout=25", LogLevel: "silent",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	movie := &models.Movie{ContentID: "cleanup-movie", ID: "CLEANUP-001", Title: "Original title", RenderGeneration: 3}
	require.NoError(t, db.Create(movie).Error)
	filePath := filepath.Join(t.TempDir(), "CLEANUP-001.mp4")
	store := resultstore.New(1, []string{filePath})
	store.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID: "cleanup-result", Status: models.JobStatusCompleted, Movie: movie,
		PersistedMovie: true, FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movie.ID},
	})
	store.SetProvenance(filePath, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "original"}})
	inputs := makeApplyInputs(&stubApplyWorkflow{})
	inputs.Updater = store
	inputs.MovieRepo = database.NewMovieRepository(db)
	inputs.Results = store.CloneResults()
	inputs.Provenance = map[string]*resultstore.ProvenanceData{filePath: store.GetProvenance(filePath)}
	return inputs, store, db, filePath
}

func interpretCleanupFailure(ctx context.Context, inputs applyPhaseInputs, filePath string) applyFileOutcome {
	snapshot := inputs.Results[filePath]
	afc := &ApplyFileContext{
		FilePath: filePath, Movie: snapshot.Movie, MovieResult: snapshot,
		Match: snapshot.FileMatchInfo, reviewBaseline: snapshot.Movie.Clone(),
		PersistedMovie: true, PublicationGeneration: snapshot.Movie.RenderGeneration,
	}
	return interpretApplyResult(filePath, snapshot.Movie, time.Now(), 20*time.Millisecond,
		inputs, ApplyPhaseConfig{}, ctx, afc, nil, ctx.Err())
}

func TestApplyFailureWritebackPersistsTerminalStatus(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		for _, fenced := range []bool{false, true} {
			name := map[bool]string{false: "deadline", true: "cancelled"}[cancelled] + "/" +
				map[bool]string{false: "legacy", true: "persisted-fence"}[fenced]
			t.Run(name, func(t *testing.T) {
				inputs, store, _, filePath := newFailureWritebackFixture(t)
				if !fenced {
					inputs.MovieRepo = nil
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				wf := &cleanupDeadlineWorkflow{}
				if cancelled {
					wf.started = cancel
				} else {
					inputs.Concurrency.WorkerTimeout = 20 * time.Millisecond
				}
				inputs.WF = wf
				failures, phaseCalls, organized, failed := 0, 0, 0, 0
				NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{
					Destination:     t.TempDir(),
					OnFileFailed:    func(string, string) { failures++ },
					OnPhaseComplete: func(org, fail int) { phaseCalls++; organized, failed = org, fail },
				})
				final, err := store.GetMovieResult(filePath)
				require.NoError(t, err)
				assert.Equal(t, "Original title", final.Movie.Title)
				assert.Equal(t, "cleanup-result", final.ResultID)
				assert.Equal(t, uint64(2), final.Revision)
				assert.False(t, final.StartedAt.IsZero())
				require.NotNil(t, final.EndedAt)
				assert.False(t, final.EndedAt.Before(final.StartedAt))
				assert.Equal(t, "original", store.GetProvenance(filePath).FieldSources["title"])
				_, progress := store.SnapshotForStatus()
				assert.Zero(t, progress.Completed)
				lc := inputs.Lifecycle.(*stubLifecycle)
				assert.False(t, lc.organized)
				if cancelled {
					assert.Equal(t, models.JobStatusCancelled, final.Status)
					assert.Equal(t, "organization canceled", final.Error)
					assert.Zero(t, failures)
					assert.Zero(t, phaseCalls)
					assert.Zero(t, progress.Failed)
					assert.True(t, lc.cancelled)
					assert.False(t, lc.completed)
				} else {
					assert.Equal(t, models.JobStatusFailed, final.Status)
					assert.Equal(t, "apply timed out after 20ms", final.Error)
					assert.Equal(t, 1, failures)
					assert.Equal(t, 1, phaseCalls)
					assert.Zero(t, organized)
					assert.Equal(t, 1, failed)
					assert.Equal(t, 1, progress.Failed)
					assert.True(t, lc.completed, "completed job retains retryable per-file failures")
					assert.False(t, lc.cancelled)
				}
			})
		}
	}
}

type cleanupFenceRepository struct {
	database.MovieRepositoryInterface
	fence func(context.Context, string, int64, func(*models.Movie) error) error
}

func (r *cleanupFenceRepository) WithApplyPublicationFence(ctx context.Context, id string, generation int64, publish func(*models.Movie) error) error {
	return r.fence(ctx, id, generation, publish)
}

func TestApplyFailureWritebackCleanupDoesNotReviveArtifactContext(t *testing.T) {
	inputs, store, db, filePath := newFailureWritebackFixture(t)
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "trace"))
	cancel()
	var cleanup context.Context
	publishedArtifact := false
	repo := database.NewMovieRepository(db)
	inputs.MovieRepo = &cleanupFenceRepository{
		MovieRepositoryInterface: repo,
		fence: func(writeCtx context.Context, id string, generation int64, publish func(*models.Movie) error) error {
			cleanup = writeCtx
			assert.NoError(t, writeCtx.Err())
			assert.Equal(t, "trace", writeCtx.Value(contextKey{}))
			deadline, ok := writeCtx.Deadline()
			require.True(t, ok)
			assert.Positive(t, time.Until(deadline))
			assert.LessOrEqual(t, time.Until(deadline), applyFailureWritebackTimeout)
			assert.ErrorIs(t, repo.WithApplyArtifactPublicationFence(ctx, id, generation, func(*models.Movie) error {
				publishedArtifact = true
				return nil
			}), context.Canceled)
			return repo.WithApplyPublicationFence(writeCtx, id, generation, publish)
		},
	}
	outcome := interpretCleanupFailure(ctx, inputs, filePath)
	assert.True(t, outcome.Cancelled)
	require.NotNil(t, cleanup)
	assert.ErrorIs(t, cleanup.Err(), context.Canceled, "cleanup resources released on return")
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.False(t, publishedArtifact)
	final, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCancelled, final.Status)
}

func TestApplyFailureWritebackPreservesGuards(t *testing.T) {
	for _, mode := range []string{"generation", "rekey-before-fence", "rekey-inside-fence", "promote-witness"} {
		t.Run(mode, func(t *testing.T) {
			inputs, store, db, filePath := newFailureWritebackFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			changeLiveIdentity := func() {
				require.NoError(t, store.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
					current.Movie.ID = "NEW-002"
					current.Movie.ContentID = "new-content"
					current.Movie.Title = "Newer result"
					current.FileMatchInfo.MovieID = "NEW-002"
					return current, nil
				}))
			}
			switch mode {
			case "generation":
				require.NoError(t, db.Model(&models.Movie{}).Where("content_id = ?", "cleanup-movie").UpdateColumn("render_generation", gorm.Expr("render_generation + 1")).Error)
			case "rekey-before-fence":
				changeLiveIdentity()
			case "rekey-inside-fence":
				repo := database.NewMovieRepository(db)
				inputs.MovieRepo = &cleanupFenceRepository{
					MovieRepositoryInterface: repo,
					fence: func(writeCtx context.Context, id string, generation int64, publish func(*models.Movie) error) error {
						changeLiveIdentity()
						return repo.WithApplyPublicationFence(writeCtx, id, generation, publish)
					},
				}
			case "promote-witness":
				inputs.PromoteWitnessFn = func(string) bool { return true }
			}
			before, err := store.GetMovieResult(filePath)
			require.NoError(t, err)
			beforeProvenance := store.GetProvenance(filePath)
			outcome := interpretCleanupFailure(ctx, inputs, filePath)
			assert.True(t, outcome.Cancelled)
			final, err := store.GetMovieResult(filePath)
			require.NoError(t, err)
			if mode == "rekey-inside-fence" {
				assert.Equal(t, before.Revision+1, final.Revision, "only the concurrent rekey may advance the revision")
				assert.Equal(t, "NEW-002", final.Movie.ID)
				assert.Equal(t, "new-content", final.Movie.ContentID)
				assert.Equal(t, "Newer result", final.Movie.Title)
				assert.Equal(t, models.JobStatusCompleted, final.Status)
				assert.Empty(t, final.Error)
			} else {
				assert.Equal(t, before, final, "cleanup must not publish through a stale generation/identity or unresolved witness")
			}
			assert.Equal(t, beforeProvenance, store.GetProvenance(filePath))
		})
	}
}

func TestApplyFailureWritebackPreservesNewerReviewEdits(t *testing.T) {
	inputs, store, _, filePath := newFailureWritebackFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, store.AtomicUpdateFileResultWithProvenance(filePath, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
		current.Movie.Title = "Live title"
		current.Movie.Actresses = []models.Actress{{FirstName: "Live"}}
		prov.FieldSources["title"] = "user"
		return current, prov, nil
	}))
	outcome := interpretCleanupFailure(ctx, inputs, filePath)
	assert.True(t, outcome.Cancelled)
	final, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "Live title", final.Movie.Title)
	require.Len(t, final.Movie.Actresses, 1)
	assert.Equal(t, "Live", final.Movie.Actresses[0].FirstName)
	assert.Equal(t, "user", store.GetProvenance(filePath).FieldSources["title"])
	assert.Equal(t, models.JobStatusCancelled, final.Status)
	assert.Equal(t, uint64(3), final.Revision)
}

type cleanupFailingUpdater struct {
	resultstore.Store
	updates int
	upserts int
}

func (u *cleanupFailingUpdater) AtomicUpdateFileResultWithProvenanceAfter(string, func(func() error) error, func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	u.updates++
	return errors.New("result write unavailable")
}

func (u *cleanupFailingUpdater) UpsertFileResultWithProvenance(path string, result *resultstore.MovieResult, prov *resultstore.ProvenanceData) {
	u.upserts++
	u.Store.UpdateFileResult(path, result)
	u.Store.SetProvenance(path, prov)
}

func TestApplyFailureWritebackFailsClosedOnPersistenceErrors(t *testing.T) {
	for _, mode := range []string{"database-closed", "database-locked", "updater-error"} {
		t.Run(mode, func(t *testing.T) {
			inputs, store, db, filePath := newFailureWritebackFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var updater *cleanupFailingUpdater
			switch mode {
			case "database-closed":
				require.NoError(t, db.Close())
			case "database-locked":
				tx := db.Begin()
				require.NoError(t, tx.Error)
				t.Cleanup(func() { _ = tx.Rollback().Error })
				require.NoError(t, tx.Model(&models.Movie{}).Where("content_id = ?", "cleanup-movie").UpdateColumn("render_generation", 3).Error)
			case "updater-error":
				updater = &cleanupFailingUpdater{Store: store}
				inputs.Updater = updater
			}
			before, err := store.GetMovieResult(filePath)
			require.NoError(t, err)
			beforeProvenance := store.GetProvenance(filePath)
			outcome := interpretCleanupFailure(ctx, inputs, filePath)
			assert.True(t, outcome.Cancelled)
			final, err := store.GetMovieResult(filePath)
			require.NoError(t, err)
			assert.Equal(t, before, final, "persistence errors must not trigger an unfenced replacement")
			assert.Equal(t, beforeProvenance, store.GetProvenance(filePath))
			if updater != nil {
				assert.Equal(t, 1, updater.updates)
				assert.Zero(t, updater.upserts)
			}
		})
	}
}

func TestApplyFailureWritebackDatabaseWaitIsBounded(t *testing.T) {
	inputs, store, db, filePath := newFailureWritebackFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	held, err := pool.Conn(context.Background())
	require.NoError(t, err)
	defer held.Close()
	before, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	start := time.Now()
	outcome := interpretCleanupFailure(ctx, inputs, filePath)
	elapsed := time.Since(start)
	assert.True(t, outcome.Cancelled)
	assert.GreaterOrEqual(t, elapsed, applyFailureWritebackTimeout, "cleanup must receive its own budget")
	assert.Less(t, elapsed, 2*applyFailureWritebackTimeout, "a blocked database must not hang cleanup indefinitely")
	final, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, before, final)
}

func TestApplySuccessWritebackRetainsCancelledContextFence(t *testing.T) {
	inputs, store, _, filePath := newFailureWritebackFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	phaseOut := before.Movie.Clone()
	phaseOut.Title = "Must not publish"
	afc := &ApplyFileContext{FilePath: filePath, Match: before.FileMatchInfo, MovieResult: before,
		PersistedMovie: true, PublicationGeneration: before.Movie.RenderGeneration}
	interpretApplyResult(filePath, before.Movie, time.Now(), time.Second, inputs, ApplyPhaseConfig{}, ctx, afc,
		&workflow.ApplyResult{Movie: phaseOut}, nil)
	final, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, before, final)
}
