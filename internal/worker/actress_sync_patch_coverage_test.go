package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type fixedScraperSet struct{ enabled []models.Scraper }

func (s *fixedScraperSet) RegisterInstance(models.Scraper)                  {}
func (s *fixedScraperSet) GetInstance(string) (models.Scraper, bool)        { return nil, false }
func (s *fixedScraperSet) GetAllInstances() []models.Scraper                { return s.enabled }
func (s *fixedScraperSet) GetEnabledInstances() []models.Scraper            { return s.enabled }
func (s *fixedScraperSet) GetInstancesByPriority([]string) []models.Scraper { return s.enabled }
func (s *fixedScraperSet) GetInstancesByPriorityForInput([]string, string) []models.Scraper {
	return s.enabled
}

type urlActressSyncScraper struct {
	actressSyncScraper
	canHandle bool
	urlResult *models.ScraperResult
	urlCalls  int
}

func (s *urlActressSyncScraper) CanHandleURL(string) bool                { return s.canHandle }
func (s *urlActressSyncScraper) ExtractIDFromURL(string) (string, error) { return "PATCH-77", nil }
func (s *urlActressSyncScraper) ScrapeURL(context.Context, string) (*models.ScraperResult, error) {
	s.urlCalls++
	return s.urlResult, nil
}

func TestActressSyncPatchHelpers(t *testing.T) {
	t.Run("validators", func(t *testing.T) {
		fallbackErr := errors.New("fallback")
		require.ErrorIs(t, validateActressThumbnail(context.Background(), &actressSyncScraper{}, func(context.Context, string) error { return fallbackErr }, "url"), fallbackErr)
		require.NoError(t, validateActressThumbnail(context.Background(), &actressSyncScraper{}, nil, "url"))
	})

	t.Run("scraper filtering", func(t *testing.T) {
		dmm := &actressSyncScraper{name: " DMM "}
		javdb := &actressSyncScraper{name: "javdb"}
		other := &actressSyncScraper{name: "other"}
		set := &fixedScraperSet{enabled: []models.Scraper{nil, dmm, javdb, other}}
		require.Equal(t, []models.Scraper{dmm}, authoritativeActressScrapers(set))
		require.Equal(t, []models.Scraper{dmm, javdb}, actressMetadataScrapers(set))
	})

	t.Run("cache lookup and aliases", func(t *testing.T) {
		_, ok := lookupActressCache(&models.Actress{}, func(int, string, string, string) (models.ActressInfo, bool) { return models.ActressInfo{}, false })
		require.False(t, ok)
		_, ok = lookupActressCache(&models.Actress{}, func(int, string, string, string) (models.ActressInfo, bool) { return models.ActressInfo{}, true })
		require.False(t, ok)
		match, ok := lookupActressCache(&models.Actress{DMMID: 7}, func(int, string, string, string) (models.ActressInfo, bool) { return models.ActressInfo{}, true })
		require.True(t, ok)
		require.Equal(t, 7, match.DMMID)
		require.True(t, cacheMatchesCanonical(models.ActressInfo{DMMID: 7, JapaneseName: "新名"}, &models.Actress{DMMID: 7, JapaneseName: "旧名", Aliases: " | 新名 |"}))
	})

	t.Run("identity parsing", func(t *testing.T) {
		names := actressIdentityNames(&models.Actress{JapaneseName: " A (A), ;\nB", Aliases: "B|C"})
		require.Equal(t, []string{"A", "B", "C"}, names)
		require.False(t, identityNameMatches(names, "missing"))
	})

	t.Run("fallback and resolving", func(t *testing.T) {
		require.False(t, needsLinkedActressFallback(nil, nil))
		require.False(t, needsLinkedActressFallback(&models.Actress{}, nil))
		actress := &models.Actress{DMMID: 1}
		require.False(t, needsLinkedActressFallback(actress, []models.ActressInfo{{DMMID: 1, FirstName: "A"}, {DMMID: 1, FirstName: "B"}}))
		candidate, conflict := resolveActressInfo(nil, nil)
		require.False(t, conflict)
		require.Empty(t, candidate)
		candidate, conflict = resolveActressInfo(&models.Actress{DMMID: 1}, []models.ActressInfo{{DMMID: 2, FirstName: "wrong"}, {DMMID: 1, FirstName: "right"}})
		require.False(t, conflict)
		require.Equal(t, "right", candidate.FirstName)
	})

	t.Run("URLs", func(t *testing.T) {
		require.False(t, scraperThumbnailCanRefresh(":"))
		require.Equal(t, 3, actressThumbnailSourcePriority("other"))
	})
}

func TestLinkedActressCandidatesPatchBranches(t *testing.T) {
	db, _, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 77})
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "linked-patch", ID: "PATCH-77", DisplayTitle: "Linked", SourceURL: "https://example.test/PATCH-77"}).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "linked-patch", actress.ID).Error)
	urlScraper := &urlActressSyncScraper{actressSyncScraper: actressSyncScraper{name: "dmm"}, canHandle: true, urlResult: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 77}}}}
	matches, err := linkedActressCandidates(context.Background(), movieRepo, actress.ID, []models.Scraper{urlScraper})
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	require.Equal(t, len(matches), urlScraper.urlCalls)

	movies, err := linkedActressMovies(context.Background(), nil, actress.ID)
	require.NoError(t, err)
	require.Nil(t, movies)

	closedDB, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "closed.db")})
	require.NoError(t, err)
	closedRepo := database.NewMovieRepository(closedDB)
	require.NoError(t, closedDB.Close())
	_, err = linkedActressMovies(context.Background(), closedRepo, actress.ID)
	require.Error(t, err)
	_, err = linkedActressCandidates(context.Background(), closedRepo, actress.ID, nil)
	require.Error(t, err)
	_, err = linkedActressMatches(context.Background(), closedRepo, actress.ID, 77, nil)
	require.Error(t, err)
}

func TestLinkedIdentityAssignmentRaceMergesCanonicalActress(t *testing.T) {
	uniqueErr := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}
	db, actressRepo, movieRepo, duplicate := newActressSyncFixture(t, &models.Actress{JapaneseName: "linked race"})
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "linked-race", ID: "RACE-914", DisplayTitle: "Linked", SourceURL: "https://example.test/RACE-914"}).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "linked-race", duplicate.ID).Error)
	canonical := &models.Actress{DMMID: 914, JapaneseName: "linked race"}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&urlActressSyncScraper{
		actressSyncScraper: actressSyncScraper{name: "dmm"},
		canHandle:          true,
		urlResult:          &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 914, JapaneseName: "linked race"}}},
	})

	result, err := SyncActressMetadata(t.Context(), duplicate.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
		AssignDMMID: func(uint, int) (bool, error) {
			require.NoError(t, actressRepo.Create(t.Context(), canonical))
			return false, uniqueErr
		},
	})
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "merged_duplicate")
	stored, err := actressRepo.FindByDMMID(t.Context(), 914)
	require.NoError(t, err)
	require.Equal(t, canonical.ID, stored.ID)
	_, err = actressRepo.FindByID(t.Context(), duplicate.ID)
	require.True(t, database.IsNotFound(err))
}

func TestLinkedIdentityAssignmentRaceBranches(t *testing.T) {
	uniqueErr := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}
	setup := func(t *testing.T, source *models.Actress) (*database.DB, *database.ActressRepository, *database.MovieRepository, *models.Actress, *scraperutil.ScraperRegistry) {
		t.Helper()
		db, actressRepo, movieRepo, duplicate := newActressSyncFixture(t, source)
		require.NoError(t, db.DB.Create(&models.Movie{ContentID: "linked-race", ID: "RACE-914", DisplayTitle: "Linked", SourceURL: "https://example.test/RACE-914"}).Error)
		require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "linked-race", duplicate.ID).Error)
		registry := scraperutil.NewScraperRegistry()
		registry.RegisterInstance(&urlActressSyncScraper{
			actressSyncScraper: actressSyncScraper{name: "dmm"},
			canHandle:          true,
			urlResult:          &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 914, JapaneseName: "linked race"}}},
		})
		return db, actressRepo, movieRepo, duplicate, registry
	}

	t.Run("same source assigned concurrently", func(t *testing.T) {
		_, actressRepo, movieRepo, duplicate, registry := setup(t, &models.Actress{JapaneseName: "linked race"})
		result, err := SyncActressMetadata(t.Context(), duplicate.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
			AssignDMMID: func(id uint, dmmID int) (bool, error) {
				assigned, assignErr := actressRepo.AssignDMMIDIfMissing(t.Context(), id, dmmID)
				require.NoError(t, assignErr)
				require.True(t, assigned)
				return false, uniqueErr
			},
		})
		require.NoError(t, err)
		require.Contains(t, result.UpdatedFields, "dmm_id")
	})

	t.Run("canonical reload fails", func(t *testing.T) {
		db, actressRepo, movieRepo, duplicate, registry := setup(t, &models.Actress{JapaneseName: "linked race"})
		canonical := &models.Actress{DMMID: 914, JapaneseName: "linked race"}
		_, err := SyncActressMetadata(t.Context(), duplicate.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
			AssignDMMID: func(uint, int) (bool, error) {
				require.NoError(t, actressRepo.Create(t.Context(), canonical))
				require.NoError(t, db.Close())
				return false, uniqueErr
			},
		})
		require.Error(t, err)
	})

	t.Run("incompatible canonical", func(t *testing.T) {
		_, actressRepo, movieRepo, duplicate, registry := setup(t, &models.Actress{FirstName: "Source", JapaneseName: "linked race"})
		canonical := &models.Actress{DMMID: 914, FirstName: "Other", JapaneseName: "linked race"}
		result, err := SyncActressMetadata(t.Context(), duplicate.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
			AssignDMMID: func(uint, int) (bool, error) {
				require.NoError(t, actressRepo.Create(t.Context(), canonical))
				return false, uniqueErr
			},
		})
		require.NoError(t, err)
		require.Contains(t, result.Messages, "missing_dmm_id")
	})

	t.Run("merge fails", func(t *testing.T) {
		mergeErr := errors.New("linked merge failure")
		_, actressRepo, movieRepo, duplicate, registry := setup(t, &models.Actress{JapaneseName: "linked race"})
		canonical := &models.Actress{DMMID: 914, JapaneseName: "linked race"}
		_, err := SyncActressMetadata(t.Context(), duplicate.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
			AssignDMMID: func(uint, int) (bool, error) {
				require.NoError(t, actressRepo.Create(t.Context(), canonical))
				return false, uniqueErr
			},
			MergeActresses: func(uint, uint) (*database.ActressMergeResult, error) {
				return nil, mergeErr
			},
		})
		require.ErrorIs(t, err, mergeErr)
	})
}

func TestLinkedIdentityRecoveryFencesSourceChanges(t *testing.T) {
	uniqueErr := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}
	setup := func(t *testing.T, source *models.Actress, dmmID int) (*database.DB, *database.ActressRepository, *database.MovieRepository, *models.Actress, *scraperutil.ScraperRegistry) {
		t.Helper()
		db, actressRepo, movieRepo, duplicate := newActressSyncFixture(t, source)
		require.NoError(t, db.DB.Create(&models.Movie{ContentID: "linked-fence", ID: "FENCE-1", DisplayTitle: "Linked", SourceURL: "https://example.test/FENCE-1"}).Error)
		require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "linked-fence", duplicate.ID).Error)
		registry := scraperutil.NewScraperRegistry()
		registry.RegisterInstance(&urlActressSyncScraper{
			actressSyncScraper: actressSyncScraper{name: "dmm"},
			canHandle:          true,
			urlResult:          &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: dmmID, JapaneseName: source.JapaneseName}}},
		})
		return db, actressRepo, movieRepo, duplicate, registry
	}

	t.Run("assignment", func(t *testing.T) {
		db, actressRepo, movieRepo, source, registry := setup(t, &models.Actress{JapaneseName: "assignment fence"}, 915)
		result, err := SyncActressMetadata(t.Context(), source.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
			AssignDMMIDWithSource: func(id uint, dmmID int, expectedSource models.Actress) (bool, error) {
				require.NoError(t, actressRepo.RenameNameFields(t.Context(), id, "", "", "edited identity"))
				return actressRepo.AssignDMMIDIfMissingWithSource(t.Context(), id, dmmID, expectedSource)
			},
		})
		require.NoError(t, err)
		require.Contains(t, result.Messages, "missing_dmm_id")
		stored, err := actressRepo.FindByID(t.Context(), source.ID)
		require.NoError(t, err)
		require.Zero(t, stored.DMMID)
		_ = db
	})

	t.Run("cache assignment", func(t *testing.T) {
		db, actressRepo, movieRepo, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "cache fence"})
		result, err := SyncActressMetadata(t.Context(), source.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
			LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
				return models.ActressInfo{DMMID: 918, JapaneseName: "cache fence"}, true
			},
			AssignDMMIDWithSource: func(id uint, dmmID int, expectedSource models.Actress) (bool, error) {
				require.NoError(t, actressRepo.RenameNameFields(t.Context(), id, "", "", "edited cache identity"))
				return actressRepo.AssignDMMIDIfMissingWithSource(t.Context(), id, dmmID, expectedSource)
			},
		})
		require.NoError(t, err)
		require.Contains(t, result.Messages, "missing_dmm_id")
		stored, err := actressRepo.FindByID(t.Context(), source.ID)
		require.NoError(t, err)
		require.Zero(t, stored.DMMID)
		_ = db
	})

	t.Run("manager assignment", func(t *testing.T) {
		db, actressRepo, movieRepo, source, registry := setup(t, &models.Actress{JapaneseName: "manager fence"}, 917)
		manager, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, source.ID, registry)
		manager.Stop()
		updated, err := actressRepo.FindByID(t.Context(), source.ID)
		require.NoError(t, err)
		require.Equal(t, 917, updated.DMMID)
		tasks, err := manager.ListTasks(job.ID)
		require.NoError(t, err)
		require.Contains(t, tasks[0].UpdatedFields, "dmm_id")
	})

	t.Run("merge", func(t *testing.T) {
		db, actressRepo, movieRepo, source, registry := setup(t, &models.Actress{JapaneseName: "merge fence"}, 916)
		canonical := &models.Actress{DMMID: 916, JapaneseName: "merge fence"}
		_, err := SyncActressMetadata(t.Context(), source.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
			AssignDMMIDWithSource: func(uint, int, models.Actress) (bool, error) {
				require.NoError(t, actressRepo.Create(t.Context(), canonical))
				require.NoError(t, actressRepo.RenameNameFields(t.Context(), source.ID, "", "", "edited identity"))
				return false, uniqueErr
			},
			MergeActressesWithSource: func(targetID, sourceID uint, expectedSource models.Actress) (*database.ActressMergeResult, error) {
				return actressRepo.MergeWithSource(t.Context(), targetID, sourceID, nil, expectedSource)
			},
		})
		require.ErrorIs(t, err, database.ErrActressSyncIdentityChanged)
		_, err = actressRepo.FindByID(t.Context(), source.ID)
		require.NoError(t, err)
		_, err = actressRepo.FindByID(t.Context(), canonical.ID)
		require.NoError(t, err)
		_ = db
	})
}

func TestSyncActressMetadataCallbackFailures(t *testing.T) {
	callbackErr := errors.New("callback failure")

	t.Run("missing actress", func(t *testing.T) {
		_, actressRepo, movieRepo, _ := newActressSyncFixture(t, &models.Actress{DMMID: 1})
		_, err := SyncActressMetadata(context.Background(), 999999, actressRepo, movieRepo, nil)
		require.Error(t, err)
	})

	t.Run("cache assignment", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			assign  func(uint, int) (bool, error)
			wantErr bool
		}{
			{name: "error", assign: func(uint, int) (bool, error) { return false, callbackErr }, wantErr: true},
			{name: "not assigned", assign: func(uint, int) (bool, error) { return false, nil }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "名前"})
				result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
					AssignDMMID: tc.assign,
					LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
						return models.ActressInfo{DMMID: 9, JapaneseName: "名前"}, true
					},
				})
				if tc.wantErr {
					require.ErrorIs(t, err, callbackErr)
				} else {
					require.NoError(t, err)
					require.Contains(t, result.Messages, "missing_dmm_id")
				}
			})
		}
	})

	t.Run("cache merge", func(t *testing.T) {
		_, actressRepo, movieRepo, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "同名"})
		canonical := &models.Actress{DMMID: 9, JapaneseName: "同名"}
		require.NoError(t, actressRepo.Create(context.Background(), canonical))
		_, err := SyncActressMetadata(context.Background(), source.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
			MergeActresses: func(uint, uint) (*database.ActressMergeResult, error) { return nil, callbackErr },
			LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
				return models.ActressInfo{DMMID: 9, JapaneseName: "同名"}, true
			},
		})
		require.ErrorIs(t, err, callbackErr)
	})

	t.Run("cache fill", func(t *testing.T) {
		_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 9, JapaneseName: "名前"})
		_, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
			FillMetadata: func(uint, int, models.ActressInfo) ([]string, error) { return nil, callbackErr },
			LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
				return models.ActressInfo{DMMID: 9, JapaneseName: "名前", FirstName: "A"}, true
			},
		})
		require.ErrorIs(t, err, callbackErr)
	})

	t.Run("no verified metadata", func(t *testing.T) {
		_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 9})
		result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, nil, ActressSyncOptions{FillMetadata: func(uint, int, models.ActressInfo) ([]string, error) { return nil, nil }})
		require.NoError(t, err)
		require.Equal(t, []string{"no_verified_metadata"}, result.Messages)
	})
}

func TestActressSyncManagerPatchBranches(t *testing.T) {
	var nilManager *ActressSyncManager
	nilManager.Start()
	nilManager.Stop()

	manager := NewActressSyncManager(ActressSyncManagerDeps{})
	manager.Start()
	manager.Stop()
	manager.started = true
	manager.Start()
	manager.started = false
	runtimeConfig, runtimeRegistry := manager.runtimeSnapshot()
	require.Nil(t, runtimeConfig)
	require.Nil(t, runtimeRegistry)
	require.Equal(t, 5, manager.maxWorkers(nil))
	cfg := &config.Config{}
	cfg.Performance.MaxWorkers = 3
	require.Equal(t, 3, manager.maxWorkers(cfg))

	_, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "bad"})
	require.Error(t, err)
	_, err = manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected"})
	require.Error(t, err)
	require.Equal(t, []uint{2, 1}, uniqueActressIDs([]uint{0, 2, 2, 1, 0}))

	db, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 5, FirstName: "A", LastName: "B", JapaneseName: "名", ThumbURL: "thumb"})
	manager = NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	_, err = manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{999999}})
	require.Error(t, err)
	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{actress.ID}})
	require.NoError(t, err)
	manager.Start()
	manager.Start()
	require.Eventually(t, func() bool {
		current, findErr := manager.GetJob(job.ID)
		return findErr == nil && current.Status == models.ActressSyncJobCompleted
	}, 3*time.Second, 10*time.Millisecond)
	manager.Stop()
	manager.Stop()

	_, err = manager.ListTasks("missing")
	require.Error(t, err)
	require.Error(t, manager.CancelJob("missing"))

	manager.ctx = nil
	task := &models.ActressSyncTask{ID: "missing-task", JobID: "missing-job", LeaseToken: "token"}
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTask(task, 10*time.Millisecond, nil)
	require.Equal(t, models.ActressSyncTaskFailed, task.Status)
}
func TestCachedIdentityAssignmentRaceMergesCanonicalActress(t *testing.T) {
	_, actressRepo, movieRepo, duplicate := newActressSyncFixture(t, &models.Actress{JapaneseName: "同一女優"})
	canonical := &models.Actress{DMMID: 909, JapaneseName: "同一女優"}
	result, err := SyncActressMetadata(t.Context(), duplicate.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{DMMID: 909, JapaneseName: "同一女優"}, true
		},
		AssignDMMID: func(uint, int) (bool, error) {
			require.NoError(t, actressRepo.Create(t.Context(), canonical))
			return false, sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}
		},
	})
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "merged_duplicate")
	stored, err := actressRepo.FindByDMMID(t.Context(), 909)
	require.NoError(t, err)
	require.Equal(t, canonical.ID, stored.ID)
	_, err = actressRepo.FindByID(t.Context(), duplicate.ID)
	require.True(t, database.IsNotFound(err))
}
func TestCachedIdentityAssignmentRaceBranches(t *testing.T) {
	uniqueErr := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}
	t.Run("same actress assigned concurrently", func(t *testing.T) {
		_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "同一"})
		result, err := SyncActressMetadata(t.Context(), actress.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
			LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
				return models.ActressInfo{DMMID: 910, JapaneseName: "同一"}, true
			},
			AssignDMMID: func(id uint, dmmID int) (bool, error) {
				assigned, assignErr := actressRepo.AssignDMMIDIfMissing(t.Context(), id, dmmID)
				require.NoError(t, assignErr)
				require.True(t, assigned)
				return false, uniqueErr
			},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		stored, err := actressRepo.FindByDMMID(t.Context(), 910)
		require.NoError(t, err)
		require.Equal(t, actress.ID, stored.ID)
	})
	t.Run("canonical reload fails", func(t *testing.T) {
		_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "未登録"})
		_, err := SyncActressMetadata(t.Context(), actress.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
			LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
				return models.ActressInfo{DMMID: 911, JapaneseName: "未登録"}, true
			},
			AssignDMMID: func(uint, int) (bool, error) { return false, uniqueErr },
		})
		require.Error(t, err)
		require.True(t, database.IsNotFound(err))
	})
}
func TestCachedIdentityMergeRejectsConcurrentSourceAssignment(t *testing.T) {
	_, actressRepo, movieRepo, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "duplicate"})
	canonical := &models.Actress{DMMID: 912, JapaneseName: "canonical"}
	require.NoError(t, actressRepo.Create(t.Context(), canonical))
	_, err := SyncActressMetadata(t.Context(), source.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{DMMID: 912, JapaneseName: "canonical", Aliases: []string{"duplicate"}}, true
		},
		MergeCachedIdentityWithSource: func(targetID, sourceID uint, expectedDMMID int, expectedSource models.Actress) (*database.ActressMergeResult, error) {
			assigned, assignErr := actressRepo.AssignDMMIDIfMissing(t.Context(), sourceID, 913)
			require.NoError(t, assignErr)
			require.True(t, assigned)
			return actressRepo.MergeCachedIdentityWithSource(t.Context(), targetID, sourceID, expectedDMMID, expectedSource)
		},
	})
	require.ErrorIs(t, err, database.ErrActressSyncIdentityChanged)
	stored, err := actressRepo.FindByID(t.Context(), source.ID)
	require.NoError(t, err)
	require.Equal(t, 913, stored.DMMID)
}

func TestCachedIdentityMergeUsesLegacyCallback(t *testing.T) {
	boom := errors.New("legacy merge")
	_, actressRepo, movieRepo, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "duplicate"})
	canonical := &models.Actress{DMMID: 913, JapaneseName: "canonical"}
	require.NoError(t, actressRepo.Create(t.Context(), canonical))

	_, err := SyncActressMetadata(t.Context(), source.ID, actressRepo, movieRepo, nil, ActressSyncOptions{
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{DMMID: 913, JapaneseName: "canonical", Aliases: []string{"duplicate"}}, true
		},
		MergeCachedIdentity: func(uint, uint, int) (*database.ActressMergeResult, error) {
			return nil, boom
		},
	})
	require.ErrorIs(t, err, boom)
}
