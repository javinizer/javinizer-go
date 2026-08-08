package worker

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type actressSyncScraper struct {
	name              string
	result            *models.ScraperResult
	thumbnail         string
	thumbnailRequests []models.ActressInfo
}

func (s *actressSyncScraper) Name() string {
	if s.name == "" {
		return "dmm"
	}
	return s.name
}
func (s *actressSyncScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return s.result, nil
}
func (s *actressSyncScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *actressSyncScraper) IsEnabled() bool                                { return true }
func (s *actressSyncScraper) Config() *models.ScraperSettings                { return nil }
func (s *actressSyncScraper) Close() error                                   { return nil }
func (s *actressSyncScraper) ResolveActressThumbnail(_ context.Context, actress models.ActressInfo) string {
	s.thumbnailRequests = append(s.thumbnailRequests, actress)
	return s.thumbnail
}

func TestCacheMatchesCanonicalUsesJapaneseIdentityAcrossRomanizationChanges(t *testing.T) {
	cached := models.ActressInfo{DMMID: 1084111, FirstName: "Miyuu", LastName: "Kohinata", JapaneseName: "小日向みゆう"}
	require.True(t, cacheMatchesCanonical(cached, &models.Actress{DMMID: 1084111, FirstName: "Miyū", LastName: "Kiyohara", JapaneseName: "小日向みゆう"}))
	require.True(t, cacheMatchesCanonical(cached, &models.Actress{DMMID: 1084111, JapaneseName: "miru"}))
	require.False(t, cacheMatchesCanonical(cached, &models.Actress{DMMID: 1084111, JapaneseName: "別人"}))
	require.False(t, cacheMatchesCanonical(cached, &models.Actress{DMMID: 1084111}))
	require.False(t, cacheMatchesCanonical(models.ActressInfo{DMMID: 1084111}, &models.Actress{DMMID: 1084111, JapaneseName: "小日向みゆう"}))
	require.False(t, cacheMatchesCanonical(cached, &models.Actress{DMMID: 999, JapaneseName: "小日向みゆう"}))
}

func TestCacheMatchesCanonicalAcceptsKnownAlias(t *testing.T) {
	cached := models.ActressInfo{DMMID: 1078618, JapaneseName: "尾崎えりか", Aliases: []string{"与田さくら"}}
	require.True(t, cacheMatchesCanonical(cached, &models.Actress{DMMID: 1078618, JapaneseName: "与田さくら"}))
	require.False(t, cacheMatchesCanonical(cached, &models.Actress{DMMID: 1078618, JapaneseName: "別人"}))
	require.False(t, cacheMatchesCanonical(models.ActressInfo{DMMID: 1078618, JapaneseName: "尾崎えりか"}, &models.Actress{DMMID: 1078618, JapaneseName: "与田さくら"}))
}

func TestSyncActressMetadataRequiresExactDMMAndFillsBlanks(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 42, LastName: "Keep"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	movie := &models.Movie{ContentID: "movie", ID: "ABC-123", DisplayTitle: "Movie", Actresses: []models.Actress{*actress}}
	_, err = movieRepo.Upsert(context.Background(), movie)
	require.NoError(t, err)
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{
		{DMMID: 99, FirstName: "Wrong", JapaneseName: "誤り", ThumbURL: "wrong"},
		{DMMID: 42, FirstName: "Right", LastName: "Overwrite", JapaneseName: "正しい", ThumbURL: "thumb"},
	}}})
	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"thumb_url", "japanese_name", "first_name"}, result.UpdatedFields)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Right", updated.FirstName)
	require.Equal(t, "Keep", updated.LastName)
	require.Equal(t, "正しい", updated.JapaneseName)
	require.Equal(t, "thumb", updated.ThumbURL)
}

func TestSyncActressMetadataRepairsMalformedThumbnailWithResolver(t *testing.T) {
	malformed := "https://pics.dmm.co.jp/mono/actjpgs/iseya_takami"
	resolved := "https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg"
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{
		DMMID: 43, FirstName: "Takami", LastName: "Iseya", JapaneseName: "伊勢谷たかみ", ThumbURL: malformed,
	})
	scraper := &actressSyncScraper{
		thumbnail: resolved,
		result: &models.ScraperResult{Actresses: []models.ActressInfo{
			{DMMID: 99, ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/wrong.jpg"},
			{DMMID: 43, ThumbURL: malformed},
		}},
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	require.False(t, result.Conflict)
	require.Equal(t, []string{"thumb_url"}, result.UpdatedFields)
	require.Len(t, scraper.thumbnailRequests, 1)
	require.Equal(t, actress.DMMID, scraper.thumbnailRequests[0].DMMID)
	require.Equal(t, malformed, scraper.thumbnailRequests[0].ThumbURL)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, resolved, updated.ThumbURL)
}

func TestActressSyncManagerUsesBuiltinCacheForMissingScope(t *testing.T) {
	db, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 943, JapaneseName: "今井絵理"})
	nameOnly := &models.Actress{DMMID: 7805, JapaneseName: "相馬美雨"}
	require.NoError(t, actressRepo.Create(context.Background(), nameOnly))
	resolver := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 943, FirstName: "Network", LastName: "Result"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	cfg := &config.Config{}
	cfg.Scrapers.ScrapeActress = true
	cfg.Performance.MaxWorkers = 1
	cfg.Scrapers.RequestTimeoutSeconds = 2
	manager := NewActressSyncManager(ActressSyncManagerDeps{
		DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo,
		Snapshot: func() (*config.Config, *scraperutil.ScraperRegistry) { return cfg, registry },
	})
	t.Cleanup(manager.Stop)

	job, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		fresh, findErr := manager.GetJob(job.ID)
		return findErr == nil && fresh.Status == models.ActressSyncJobCompleted
	}, 5*time.Second, 10*time.Millisecond)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Eri", updated.FirstName)
	require.Equal(t, "Imai", updated.LastName)
	require.NotEmpty(t, updated.ThumbURL)
	updatedNameOnly, err := actressRepo.FindByID(context.Background(), nameOnly.ID)
	require.NoError(t, err)
	require.Equal(t, "Miu", updatedNameOnly.FirstName)
	require.Equal(t, "Soma", updatedNameOnly.LastName)
	require.NotEmpty(t, updatedNameOnly.ThumbURL)
	require.Zero(t, resolver.calls)
}

func TestActressSyncManagerUsesSingleSnapshotEpochPerTask(t *testing.T) {
	db, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 42})
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 42, ThumbURL: "thumb"}}}})
	emptyRegistry := scraperutil.NewScraperRegistry()
	cfg := &config.Config{}
	cfg.Scrapers.ScrapeActress = true
	cfg.Performance.MaxWorkers = 1
	cfg.Scrapers.RequestTimeoutSeconds = 2
	var snapshots atomic.Int32
	manager := NewActressSyncManager(ActressSyncManagerDeps{
		DB:          db,
		ActressRepo: actressRepo,
		MovieRepo:   movieRepo,
		Snapshot: func() (*config.Config, *scraperutil.ScraperRegistry) {
			if snapshots.Add(1) == 1 {
				return cfg, registry
			}
			return cfg, emptyRegistry
		},
	})
	t.Cleanup(manager.Stop)
	job, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{actress.ID}})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		fresh, findErr := manager.GetJob(job.ID)
		return findErr == nil && fresh.Status == models.ActressSyncJobCompleted
	}, 5*time.Second, 10*time.Millisecond)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "thumb", updated.ThumbURL)
}

func TestActressSyncManagerPersistsConflictWithoutMutation(t *testing.T) {
	db, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 42, LastName: "Keep"})
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&actressSyncScraper{name: "dmm", result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 42, FirstName: "First", JapaneseName: "名前", ThumbURL: "thumb"}}}})
	registry.RegisterInstance(&actressSyncScraper{name: "r18dev", result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 42, FirstName: "Other", JapaneseName: "名前", ThumbURL: "thumb"}}}})
	manager, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)

	tasks, err := manager.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskConflict, tasks[0].Status)
	require.Equal(t, "conflict", tasks[0].Outcome)
	require.Equal(t, []string{"conflicting_metadata"}, tasks[0].Messages)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Empty(t, updated.FirstName)
	require.Equal(t, "Keep", updated.LastName)
	require.Empty(t, updated.JapaneseName)
	require.Empty(t, updated.ThumbURL)
	freshJob, err := manager.GetJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, 1, freshJob.Conflicts)
	require.Zero(t, freshJob.Updated)
}

func TestActressSyncManagerPersistsPartialMetadataWarning(t *testing.T) {
	db, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 42})
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 42, ThumbURL: "thumb"}}}})
	manager, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)

	tasks, err := manager.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskCompleted, tasks[0].Status)
	require.Equal(t, "updated_with_warning", tasks[0].Outcome)
	require.Equal(t, "partial_metadata", tasks[0].Warning)
	require.Equal(t, []string{"thumb_url"}, tasks[0].UpdatedFields)
	freshJob, err := manager.GetJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, 1, freshJob.Updated)
	require.Equal(t, 1, freshJob.Warnings)
	require.Zero(t, freshJob.Skipped)
}

func newActressSyncFixture(t *testing.T, actress *models.Actress) (*database.DB, *database.ActressRepository, *database.MovieRepository, *models.Actress) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	_, err = movieRepo.Upsert(context.Background(), &models.Movie{ContentID: "movie", ID: "ABC-123", DisplayTitle: "Movie", Actresses: []models.Actress{*actress}})
	require.NoError(t, err)
	return db, actressRepo, movieRepo, actress
}

func runActressSyncManagerTask(t *testing.T, db *database.DB, actressRepo *database.ActressRepository, movieRepo *database.MovieRepository, actressID uint, registry *scraperutil.ScraperRegistry) (*ActressSyncManager, *models.ActressSyncJob) {
	t.Helper()
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	manager.ctx = context.Background()
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-" + t.Name(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-" + t.Name(), JobID: job.ID, ActressID: &actressID, Label: "test", DedupeKey: "actress:" + t.Name(), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTask(claimed, time.Minute, registry)
	return manager, job
}

type mergeBlockingActressSyncScraper struct {
	started chan struct{}
	release chan struct{}
}

func (s *mergeBlockingActressSyncScraper) Name() string { return "dmm" }
func (s *mergeBlockingActressSyncScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	s.started <- struct{}{}
	<-s.release
	return &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 42, FirstName: "updated"}}}, nil
}
func (s *mergeBlockingActressSyncScraper) GetURL(context.Context, string) (string, error) {
	return "", nil
}
func (s *mergeBlockingActressSyncScraper) IsEnabled() bool                 { return true }
func (s *mergeBlockingActressSyncScraper) Config() *models.ScraperSettings { return nil }
func (s *mergeBlockingActressSyncScraper) Close() error                    { return nil }

func TestActressSyncManagerCancelsMergedSourceAfterCancellation(t *testing.T) {
	db, actressRepo, movieRepo, source := newActressSyncFixture(t, &models.Actress{DMMID: 42, JapaneseName: "source"})
	target := &models.Actress{JapaneseName: "target"}
	require.NoError(t, actressRepo.Create(context.Background(), target))
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "cancelled-merge-job", Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: "cancelled-merge-task", JobID: job.ID, ActressID: &source.ID, Label: "source", DedupeKey: "actress:source", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	scraper := &mergeBlockingActressSyncScraper{started: make(chan struct{}), release: make(chan struct{})}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)
	manager.active.Add(1)
	manager.wg.Add(1)
	done := make(chan struct{})
	go func() {
		manager.runTask(claimed, time.Minute, registry)
		close(done)
	}()
	<-scraper.started
	require.NoError(t, manager.CancelJob(job.ID))
	_, err = actressRepo.MergeWithSource(context.Background(), target.ID, source.ID, nil, models.Actress{})
	require.NoError(t, err)
	close(scraper.release)
	<-done

	stored, err := manager.repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, models.ActressSyncTaskCancelled, stored[0].Status)
	require.Equal(t, "cancelled", stored[0].Outcome)
	storedJob, err := manager.repo.FindJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncJobCancelled, storedJob.Status)
	reclaimed, err := manager.repo.ClaimNext("reclaimer", now.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, reclaimed)
}

func TestActressSyncManagerStopsUsingMergedSourceLease(t *testing.T) {
	db, actressRepo, movieRepo, source := newActressSyncFixture(t, &models.Actress{DMMID: 42, JapaneseName: "source"})
	target := &models.Actress{JapaneseName: "target"}
	require.NoError(t, actressRepo.Create(context.Background(), target))
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "merge-source-job", Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: "merge-source-task", JobID: job.ID, ActressID: &source.ID, Label: "source", DedupeKey: "actress:source", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	scraper := &mergeBlockingActressSyncScraper{started: make(chan struct{}), release: make(chan struct{})}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)
	manager.active.Add(1)
	manager.wg.Add(1)
	done := make(chan struct{})
	go func() {
		manager.runTask(claimed, time.Minute, registry)
		close(done)
	}()
	<-scraper.started
	_, err = actressRepo.MergeWithSource(context.Background(), target.ID, source.ID, nil, models.Actress{})
	require.NoError(t, err)
	close(scraper.release)
	<-done

	stored, err := manager.repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, models.ActressSyncTaskPending, stored[0].Status)
	require.Equal(t, target.ID, *stored[0].ActressID)
	reclaimed, err := manager.repo.ClaimNext("reclaimer", now.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, reclaimed)
	require.Equal(t, target.ID, *reclaimed.ActressID)
}

type blockingActressSyncScraper struct {
	calls chan struct{}
}

func (s *blockingActressSyncScraper) Name() string { return "dmm" }
func (s *blockingActressSyncScraper) Search(ctx context.Context, _ string) (*models.ScraperResult, error) {
	select {
	case s.calls <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s *blockingActressSyncScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *blockingActressSyncScraper) IsEnabled() bool                                { return true }
func (s *blockingActressSyncScraper) Config() *models.ScraperSettings                { return nil }
func (s *blockingActressSyncScraper) Close() error                                   { return nil }

func TestActressSyncManagerStopReleasesAndCancelsInFlightTask(t *testing.T) {
	db, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 42})
	blocking := &blockingActressSyncScraper{calls: make(chan struct{}, 2)}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(blocking)
	cfg := &config.Config{}
	cfg.Scrapers.ScrapeActress = true
	cfg.Performance.MaxWorkers = 1
	manager := NewActressSyncManager(ActressSyncManagerDeps{
		DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo,
		Snapshot: func() (*config.Config, *scraperutil.ScraperRegistry) { return cfg, registry },
	})
	t.Cleanup(manager.Stop)

	job, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{actress.ID}})
	require.NoError(t, err)
	select {
	case <-blocking.calls:
	case <-time.After(5 * time.Second):
		t.Fatal("actress sync task did not start")
	}

	manager.Stop()
	tasks, err := manager.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskPending, tasks[0].Status)
	require.Empty(t, tasks[0].LeaseOwner)
	require.Empty(t, tasks[0].LeaseToken)

	manager.Start()
	select {
	case <-blocking.calls:
	case <-time.After(5 * time.Second):
		t.Fatal("released actress sync task was not resumed")
	}
	require.NoError(t, manager.CancelJob(job.ID))
	manager.Stop()

	tasks, err = manager.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncTaskCancelled, tasks[0].Status)
	fresh, err := manager.GetJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncJobCancelled, fresh.Status)
	require.Equal(t, 1, fresh.Cancelled)
}
