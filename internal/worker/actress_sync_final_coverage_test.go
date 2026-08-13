package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type callbackActressScraper struct {
	actressSyncScraper
	search func(context.Context, string) (*models.ScraperResult, error)
}

func (s *callbackActressScraper) Search(ctx context.Context, query string) (*models.ScraperResult, error) {
	return s.search(ctx, query)
}

func TestSyncActressMetadataFinalErrorBranches(t *testing.T) {
	boom := errors.New("boom")

	t.Run("cached duplicate merge fails", func(t *testing.T) {
		_, repo, movies, duplicate := newActressSyncFixture(t, &models.Actress{JapaneseName: "duplicate"})
		require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 71, JapaneseName: "canonical"}))
		_, err := SyncActressMetadata(context.Background(), duplicate.ID, repo, movies, nil, ActressSyncOptions{
			LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
				return models.ActressInfo{DMMID: 71, JapaneseName: "canonical", Aliases: []string{"duplicate"}}, true
			},
			MergeActresses: func(uint, uint) (*database.ActressMergeResult, error) { return nil, boom },
		})
		require.ErrorIs(t, err, boom)
	})

	t.Run("cached identity lookup fails", func(t *testing.T) {
		db, repo, movies, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "name"})
		_, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, nil, ActressSyncOptions{
			LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
				require.NoError(t, db.Close())
				return models.ActressInfo{DMMID: 710, JapaneseName: "name"}, true
			},
		})
		require.Error(t, err)
	})

	t.Run("identity recovery fails", func(t *testing.T) {
		_, repo, _, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "name"})
		closed, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "closed-movies.db")})
		require.NoError(t, err)
		movies := database.NewMovieRepository(closed)
		require.NoError(t, closed.Close())
		_, err = SyncActressMetadata(context.Background(), actress.ID, repo, movies, nil)
		require.Error(t, err)
	})

	t.Run("reload after final fill fails", func(t *testing.T) {
		db, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 72})
		_, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, nil, ActressSyncOptions{
			FillMetadata: func(uint, int, models.ActressInfo) ([]string, error) {
				require.NoError(t, db.Close())
				return []string{"first_name"}, nil
			},
		})
		require.Error(t, err)
	})
}

func TestRecoverMissingDMMIdentityFinalBranches(t *testing.T) {
	boom := errors.New("boom")

	t.Run("linked movie lookup fails", func(t *testing.T) {
		_, repo, _, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "target"})
		closed, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "closed-linked.db")})
		require.NoError(t, err)
		movies := database.NewMovieRepository(closed)
		require.NoError(t, closed.Close())
		_, _, _, err = recoverMissingDMMIdentity(context.Background(), actress, repo, movies, nil, nil, nil)
		require.Error(t, err)
	})

	t.Run("linked canonical merge fails", func(t *testing.T) {
		_, repo, _, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "target"})
		require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 73, JapaneseName: "other"}))
		movies := newRecoveryMovieRepo(t, actress.ID)
		scraper := &actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 73, JapaneseName: "target"}}}}
		_, _, _, err := recoverMissingDMMIdentity(context.Background(), actress, repo, movies, []models.Scraper{scraper},
			func(uint, uint) (*database.ActressMergeResult, error) { return nil, boom }, nil)
		require.ErrorIs(t, err, boom)
	})

	t.Run("linked canonical merges", func(t *testing.T) {
		_, repo, _, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "target"})
		existing := &models.Actress{DMMID: 731, JapaneseName: "other"}
		require.NoError(t, repo.Create(context.Background(), existing))
		movies := newRecoveryMovieRepo(t, actress.ID)
		scraper := &actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 731, JapaneseName: "target"}}}}
		mergedActress := *existing
		got, matches, fields, err := recoverMissingDMMIdentity(context.Background(), actress, repo, movies, []models.Scraper{scraper},
			func(uint, uint) (*database.ActressMergeResult, error) {
				return &database.ActressMergeResult{MergedActress: mergedActress}, nil
			}, nil)
		require.NoError(t, err)
		require.Equal(t, existing.ID, got.ID)
		require.Len(t, matches, 1)
		require.Equal(t, []string{"merged_duplicate"}, fields)
	})

	t.Run("DMM lookup fails after scraping", func(t *testing.T) {
		db, repo, _, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "target"})
		movies := newRecoveryMovieRepo(t, actress.ID)
		scraper := &callbackActressScraper{search: func(context.Context, string) (*models.ScraperResult, error) {
			require.NoError(t, db.Close())
			return &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 74, JapaneseName: "target"}}}, nil
		}}
		_, _, _, err := recoverMissingDMMIdentity(context.Background(), actress, repo, movies, []models.Scraper{scraper}, nil, nil)
		require.Error(t, err)
	})
}

func TestLinkedActressCandidatesUsesURLAndContentID(t *testing.T) {
	_, _, movies, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "target"})
	db := movies.GetDB()
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "CONTENT-ONLY", SourceURL: "https://example.test/movie", DisplayTitle: "Linked"}).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "CONTENT-ONLY", actress.ID).Error)

	urlScraper := &urlActressSyncScraper{canHandle: true, urlResult: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 1}}}}
	var queries []string
	searchScraper := &callbackActressScraper{search: func(_ context.Context, value string) (*models.ScraperResult, error) {
		queries = append(queries, value)
		return nil, nil
	}}
	candidates, err := linkedActressCandidates(context.Background(), movies, actress.ID, []models.Scraper{urlScraper, searchScraper})
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, 2, urlScraper.urlCalls)
	require.Contains(t, queries, "CONTENT-ONLY")

	// A malformed legacy row with neither movie ID nor content ID is ignored.
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "", DisplayTitle: "No ID"}).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "", actress.ID).Error)
	_, err = linkedActressCandidates(context.Background(), movies, actress.ID, []models.Scraper{searchScraper})
	require.NoError(t, err)
}

func newFinalManagerFixture(t *testing.T, actress *models.Actress) (*database.DB, *database.ActressRepository, *database.MovieRepository, *ActressSyncManager) {
	t.Helper()
	db, repo, movies, _ := newActressSyncFixture(t, actress)
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: repo, MovieRepo: movies})
	return db, repo, movies, manager
}

func TestActressSyncManagerStartAndDispatchFinalBranches(t *testing.T) {
	t.Run("start ignores empty queue", func(t *testing.T) {
		_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 80})
		manager.Start()
		require.False(t, manager.started)
	})

	t.Run("start schedules repository retry", func(t *testing.T) {
		db, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 81})
		require.NoError(t, db.Close())
		manager.Start()
		require.False(t, manager.started)
		require.NotNil(t, manager.retryTimer)
		manager.scheduleStartRetry()
		manager.Stop()
		require.Nil(t, manager.retryTimer)
	})

	t.Run("stale retry callback does not restart manager", func(t *testing.T) {
		manager := &ActressSyncManager{retryDelay: 10 * time.Millisecond}
		manager.mu.Lock()
		manager.scheduleStartRetry()
		manager.retryGeneration++
		manager.mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		manager.mu.Lock()
		require.NotNil(t, manager.retryTimer)
		manager.retryTimer = nil
		manager.mu.Unlock()
	})

	t.Run("transient startup failure recovers persisted job", func(t *testing.T) {
		db, repo, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 83})
		var failed atomic.Bool
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail_active_jobs_once", func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*[]models.ActressSyncJob); ok && failed.CompareAndSwap(false, true) {
				tx.AddError(errors.New("transient active jobs failure"))
			}
		}))
		t.Cleanup(manager.Stop)

		actress, err := repo.FindByDMMID(t.Context(), 83)
		require.NoError(t, err)
		job, _, err := manager.CreateJob(t.Context(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{actress.ID}})
		require.NoError(t, err)
		require.False(t, manager.started)
		require.Eventually(t, func() bool {
			current, findErr := manager.GetJob(job.ID)
			return findErr == nil && current.Status == models.ActressSyncJobCompleted
		}, 4*time.Second, 20*time.Millisecond)
	})

	t.Run("start tolerates recovery error", func(t *testing.T) {
		db, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 82})
		now := time.Now().UTC()
		job := &models.ActressSyncJob{ID: "recovery-error", Scope: "selected", Status: models.ActressSyncJobPending, CreatedAt: now}
		actressID := uint(1)
		task := models.ActressSyncTask{ID: "recovery-error-task", JobID: job.ID, ActressID: &actressID, DedupeKey: "recovery-error", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
		sqlDB, err := db.DB.DB()
		require.NoError(t, err)
		var closed atomic.Bool
		require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:close_after_active_jobs", func(*gorm.DB) {
			if closed.CompareAndSwap(false, true) {
				require.NoError(t, sqlDB.Close())
			}
		}))
		manager.Start()
		require.True(t, manager.started)
		manager.Stop()
	})

	t.Run("dispatch hits worker limit", func(t *testing.T) {
		_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 83})
		ctx, cancel := context.WithCancel(context.Background())
		manager.active.Store(5)
		manager.wg.Add(1)
		go func() { defer manager.wg.Done(); manager.dispatchLoop(ctx) }()
		manager.signal()
		require.Eventually(t, func() bool { return len(manager.wake) == 0 }, time.Second, time.Millisecond)
		cancel()
		manager.wg.Wait()
	})

	t.Run("dispatch handles empty queue", func(t *testing.T) {
		_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 84})
		ctx, cancel := context.WithCancel(context.Background())
		manager.wg.Add(1)
		go func() { defer manager.wg.Done(); manager.dispatchLoop(ctx) }()
		manager.signal()
		require.Eventually(t, func() bool { return len(manager.wake) == 0 }, time.Second, time.Millisecond)
		cancel()
		manager.wg.Wait()
	})

	t.Run("dispatch handles claim error", func(t *testing.T) {
		db, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 85})
		require.NoError(t, db.Close())
		ctx, cancel := context.WithCancel(context.Background())
		manager.wg.Add(1)
		go func() { defer manager.wg.Done(); manager.dispatchLoop(ctx) }()
		manager.signal()
		require.Eventually(t, func() bool { return len(manager.wake) == 0 }, time.Second, time.Millisecond)
		cancel()
		manager.wg.Wait()
	})
}

func TestActressSyncManagerDispatchRunsPeriodicRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("periodic recovery interval is 15 seconds")
	}
	_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 88})
	ctx, cancel := context.WithCancel(context.Background())
	manager.wg.Add(1)
	go func() { defer manager.wg.Done(); manager.dispatchLoop(ctx) }()
	<-time.After(15*time.Second + 100*time.Millisecond)
	cancel()
	manager.wg.Wait()
}

func TestActressSyncManagerCreateJobFinalBranches(t *testing.T) {
	t.Run("request validation", func(t *testing.T) {
		_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 89})
		_, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "invalid"})
		require.ErrorContains(t, err, "scope must")
		_, _, err = manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected"})
		require.ErrorContains(t, err, "actress_ids")
	})

	t.Run("missing candidate query error", func(t *testing.T) {
		db, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 90})
		require.NoError(t, db.Close())
		_, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
		require.Error(t, err)
	})

	t.Run("selected actress lookup error", func(t *testing.T) {
		_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 91})
		_, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{999999}})
		require.Error(t, err)
	})

	t.Run("empty name uses numeric label", func(t *testing.T) {
		_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 92})
		actress, err := manager.deps.ActressRepo.FindByDMMID(context.Background(), 92)
		require.NoError(t, err)
		job, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{actress.ID}})
		require.NoError(t, err)
		t.Cleanup(manager.Stop)
		tasks, err := manager.repo.ListTasks(job.ID, 0)
		require.NoError(t, err)
		require.Equal(t, "#"+strconv.FormatUint(uint64(actress.ID), 10), tasks[0].Label)
	})

	t.Run("job persistence error", func(t *testing.T) {
		db, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 93, JapaneseName: "name"})
		sqlDB, err := db.DB.DB()
		require.NoError(t, err)
		var closed atomic.Bool
		require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:close_after_candidates", func(tx *gorm.DB) {
			if tx.Statement.Table == "actresses" && closed.CompareAndSwap(false, true) {
				require.NoError(t, sqlDB.Close())
			}
		}))
		_, _, err = manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
		require.Error(t, err)
	})
}

func TestActressSyncManagerRunTaskFinalBranches(t *testing.T) {
	t.Run("cache identity uses leased assignment", func(t *testing.T) {
		_, repo, _, manager := newFinalManagerFixture(t, &models.Actress{JapaneseName: "今井絵理"})
		actress, err := repo.FindByJapaneseName(context.Background(), "今井絵理")
		require.NoError(t, err)
		task := claimFinalTask(t, manager, actress.ID, "assign-run", "selected")
		manager.active.Add(1)
		manager.wg.Add(1)
		manager.runTaskWithContext(context.Background(), task, 3*time.Second, nil, scraperutil.NewScraperRegistry())
		require.Contains(t, task.UpdatedFields, "dmm_id")
	})

	t.Run("revalidation uses leased thumbnail replacement", func(t *testing.T) {
		_, repo, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 101, FirstName: "A", LastName: "B", JapaneseName: "名", ThumbURL: "https://c0.jdbstatic.com/old.jpg"})
		actress, err := repo.FindByDMMID(context.Background(), 101)
		require.NoError(t, err)
		resolver := &sessionValidatingActressScraper{metadataOnlyActressScraper: &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 101, FirstName: "A", LastName: "B", JapaneseName: "名", ThumbURL: "https://pics.dmm.co.jp/new.jpg"}}}
		registry := scraperutil.NewScraperRegistry()
		registry.RegisterInstance(resolver)
		task := claimFinalTask(t, manager, actress.ID, "replace-run", "selected")
		manager.active.Add(1)
		manager.wg.Add(1)
		manager.runTaskWithContext(context.Background(), task, 3*time.Second, nil, registry)
		require.Contains(t, task.UpdatedFields, "thumb_url")
	})

	t.Run("canonical contention requeues task", func(t *testing.T) {
		db, repo, movies, duplicate := newActressSyncFixture(t, &models.Actress{JapaneseName: "同名"})
		manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: repo, MovieRepo: movies})
		t.Cleanup(manager.Stop)
		canonical := &models.Actress{DMMID: 200, JapaneseName: "同名"}
		require.NoError(t, repo.Create(context.Background(), canonical))
		now := time.Now().UTC()
		job := &models.ActressSyncJob{ID: "contention-job", Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
		duplicateID := duplicate.ID
		canonicalTask := models.ActressSyncTask{ID: "canonical-task", JobID: job.ID, ActressID: &canonical.ID, Label: "canonical", DedupeKey: fmt.Sprintf("actress:%d", canonical.ID), Status: models.ActressSyncTaskRunning, Stage: "resolving", Messages: []string{}, UpdatedFields: []string{}, LeaseOwner: "other", LeaseToken: "canonical-token", CreatedAt: now}
		expires := now.Add(time.Hour)
		canonicalTask.LeaseExpiresAt = &expires
		duplicateTask := models.ActressSyncTask{ID: "duplicate-task", JobID: job.ID, ActressID: &duplicateID, Label: "duplicate", DedupeKey: fmt.Sprintf("actress:%d", duplicateID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{canonicalTask, duplicateTask}))
		claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, duplicateTask.ID, claimed.ID)
		manager.active.Add(1)
		manager.wg.Add(1)
		manager.runTaskWithContext(context.Background(), claimed, 3*time.Second, nil, scraperutil.NewScraperRegistry())
		tasks, err := manager.repo.ListTasks(job.ID, 0)
		require.NoError(t, err)
		byID := map[string]models.ActressSyncTask{tasks[0].ID: tasks[0], tasks[1].ID: tasks[1]}
		require.Equal(t, models.ActressSyncTaskPending, byID[duplicateTask.ID].Status)
		require.Zero(t, byID[duplicateTask.ID].Attempts)
		require.Equal(t, models.ActressSyncTaskRunning, byID[canonicalTask.ID].Status)
		// CON-04: gate removed — ClaimNext deferral owns the wait; duplicate is pending above with attempts handed back
		require.NotContains(t, byID[duplicateTask.ID].Stage, "running")
	})

	t.Run("canonical contention requeue failure", func(t *testing.T) {
		db, _, _, manager := newFinalManagerFixture(t, &models.Actress{JapaneseName: "同名"})
		task := claimFinalTask(t, manager, 1, "requeue-failure", "missing")
		require.NoError(t, db.Close())
		require.True(t, manager.requeueCanonicalTask(task, database.ErrActressSyncCanonicalTaskRunning))
	})

	t.Run("task timeout records failure", func(t *testing.T) {
		db, repo, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 103})
		actress, err := repo.FindByDMMID(context.Background(), 103)
		require.NoError(t, err)
		task := claimFinalTask(t, manager, actress.ID, "timeout-run", "selected")
		callbackName := "test:block_actress_query_" + t.Name()
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "actresses" {
				<-tx.Statement.Context.Done()
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Query().Before("gorm:query").Remove(callbackName) })
		manager.active.Add(1)
		manager.wg.Add(1)
		manager.runTaskWithContext(context.Background(), task, 10*time.Millisecond, nil, scraperutil.NewScraperRegistry())
		require.NotEqual(t, models.ActressSyncTaskFailed, task.Status)
	})

	t.Run("cancelled run leaves lease for recovery", func(t *testing.T) {
		_, repo, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 100})
		actress, err := repo.FindByDMMID(context.Background(), 100)
		require.NoError(t, err)
		task := claimFinalTask(t, manager, actress.ID, "cancelled-run", "selected")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		manager.active.Add(1)
		manager.wg.Add(1)
		manager.runTaskWithContext(ctx, task, time.Second, nil, nil)
		require.Equal(t, models.ActressSyncTaskRunning, task.Status)
	})

	t.Run("missing identity is skipped", func(t *testing.T) {
		_, repo, _, manager := newFinalManagerFixture(t, &models.Actress{JapaneseName: "not cached"})
		actress, err := repo.FindByJapaneseName(context.Background(), "not cached")
		require.NoError(t, err)
		task := claimFinalTask(t, manager, actress.ID, "skipped-run", "selected")
		manager.active.Add(1)
		manager.wg.Add(1)
		manager.runTaskWithContext(context.Background(), task, time.Second, nil, scraperutil.NewScraperRegistry())
		require.Equal(t, models.ActressSyncTaskSkipped, task.Status)
	})

	t.Run("completion lease error is tolerated", func(t *testing.T) {
		_, repo, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 102, FirstName: "A", LastName: "B", JapaneseName: "名", ThumbURL: "https://example.test/a.jpg"})
		actress, err := repo.FindByDMMID(context.Background(), 102)
		require.NoError(t, err)
		task := claimFinalTask(t, manager, actress.ID, "completion-error", "missing")
		task.LeaseToken = "wrong-token"
		manager.active.Add(1)
		manager.wg.Add(1)
		manager.runTaskWithContext(context.Background(), task, time.Second, nil, scraperutil.NewScraperRegistry())
		require.Equal(t, models.ActressSyncTaskSkipped, task.Status)
	})
}

func claimFinalTask(t *testing.T, manager *ActressSyncManager, actressID uint, id, scope string) *models.ActressSyncTask {
	t.Helper()
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: id + "-job", Scope: scope, Status: models.ActressSyncJobPending, CreatedAt: now}
	task := models.ActressSyncTask{ID: id, JobID: job.ID, ActressID: &actressID, DedupeKey: "actress:" + id, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	return claimed
}

func TestActressSyncManagerHeartbeatStopsOnDoneAndContext(t *testing.T) {
	_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 110})
	done := make(chan struct{})
	close(done)
	manager.heartbeat(context.Background(), "id", "token", time.Second, done, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manager.heartbeat(ctx, "id", "token", time.Second, make(chan struct{}), func() {})
}
