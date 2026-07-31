package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type metadataOnlyActressScraper struct {
	name         string
	calls        int
	searchCalls  int
	searchResult *models.ScraperResult
	info         models.ActressInfo
	lastInput    models.ActressInfo
}

func (s *metadataOnlyActressScraper) Name() string {
	if s.name != "" {
		return s.name
	}
	return "dmm"
}
func (s *metadataOnlyActressScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	s.searchCalls++
	return s.searchResult, nil
}
func (s *metadataOnlyActressScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *metadataOnlyActressScraper) IsEnabled() bool                                { return true }
func (s *metadataOnlyActressScraper) Config() *models.ScraperSettings                { return nil }
func (s *metadataOnlyActressScraper) Close() error                                   { return nil }
func (s *metadataOnlyActressScraper) ResolveActressMetadata(_ context.Context, input models.ActressInfo) models.ActressInfo {
	s.calls++
	s.lastInput = input
	return s.info
}

func TestSyncActressMetadataMergesCacheAliasIntoExistingDMMActress(t *testing.T) {
	_, actressRepo, movieRepo, duplicate := newActressSyncFixture(t, &models.Actress{JapaneseName: "高橋りこ"})
	canonical := &models.Actress{DMMID: 943, JapaneseName: "今井絵理", FirstName: "Eri", LastName: "Imai", ThumbURL: "https://cache.example/eri.jpg"}
	require.NoError(t, actressRepo.Create(context.Background(), canonical))

	result, err := SyncActressMetadata(context.Background(), duplicate.ID, actressRepo, movieRepo, scraperutil.NewScraperRegistry(), ActressSyncOptions{
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{DMMID: 943, FirstName: "Eri", LastName: "Imai", JapaneseName: "今井絵理", ThumbURL: "https://cache.example/eri.jpg"}, true
		},
	})
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "merged_duplicate")
	_, err = actressRepo.FindByID(context.Background(), duplicate.ID)
	require.Error(t, err)
	merged, err := actressRepo.FindByID(context.Background(), canonical.ID)
	require.NoError(t, err)
	require.Contains(t, merged.Aliases, "高橋りこ")
}

func TestSyncActressMetadataRecoversDMMFromUnambiguousCacheHit(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "今井絵理"})
	resolver := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 943, FirstName: "Network", LastName: "Result"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{DMMID: 943, FirstName: "Eri", LastName: "Imai", JapaneseName: "今井絵理", ThumbURL: "https://cache.example/eri.jpg"}, true
		},
	})
	require.NoError(t, err)
	require.Zero(t, resolver.calls)
	require.Contains(t, result.UpdatedFields, "dmm_id")
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, 943, updated.DMMID)
	require.Equal(t, "Eri", updated.FirstName)
	require.Equal(t, "Imai", updated.LastName)
}

func TestSyncActressMetadataUsesNameOnlyCacheForKnownDMM(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 7805, JapaneseName: "相馬美雨"})
	resolver := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 7805, FirstName: "Network", LastName: "Result"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{FirstName: "Miu", LastName: "Soma", JapaneseName: "相馬美雨", ThumbURL: "https://cache.example/miu.jpg"}, true
		},
	})
	require.NoError(t, err)
	require.Zero(t, resolver.calls)
	require.ElementsMatch(t, []string{"first_name", "last_name", "thumb_url"}, result.UpdatedFields)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, 7805, updated.DMMID)
	require.Equal(t, "Miu", updated.FirstName)
	require.Equal(t, "Soma", updated.LastName)
}

func TestSyncActressMetadataUsesCompleteCacheHitBeforeResolvers(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 943, JapaneseName: "今井絵理"})
	resolver := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 943, FirstName: "Network", LastName: "Result"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			return models.ActressInfo{DMMID: 943, FirstName: "Eri", LastName: "Imai", JapaneseName: "今井絵理", ThumbURL: "https://cache.example/eri.jpg"}, true
		},
	})
	require.NoError(t, err)
	require.Zero(t, resolver.calls)
	require.ElementsMatch(t, []string{"first_name", "last_name", "thumb_url"}, result.UpdatedFields)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Eri", updated.FirstName)
	require.Equal(t, "Imai", updated.LastName)
	require.Equal(t, "https://cache.example/eri.jpg", updated.ThumbURL)
}

type sessionValidatingActressScraper struct {
	*metadataOnlyActressScraper
	validationCalls int
}

func (s *sessionValidatingActressScraper) ValidateActressThumbnail(context.Context, string) error {
	s.validationCalls++
	return nil
}

func TestSyncActressMetadataPrefersScraperSessionForThumbnailValidation(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 943, JapaneseName: "今井絵理"})
	resolver := &sessionValidatingActressScraper{metadataOnlyActressScraper: &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 943, FirstName: "Eri", LastName: "Imai", JapaneseName: "今井絵理", ThumbURL: "https://session.example/eri.jpg"}}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	fallbackCalls := 0
	_, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{ValidateThumbnail: func(context.Context, string) error {
		fallbackCalls++
		return nil
	}})
	require.NoError(t, err)
	require.Equal(t, 1, resolver.validationCalls)
	require.Zero(t, fallbackCalls)
}

func TestSyncActressMetadataSkipsReplacementValidationForExistingThumbnail(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 944, JapaneseName: "既存", ThumbURL: "https://existing.example/thumb.jpg"})
	resolver := &sessionValidatingActressScraper{metadataOnlyActressScraper: &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 944, FirstName: "New", ThumbURL: "https://replacement.example/thumb.jpg"}}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	fallbackCalls := 0
	_, err := SyncActressMetadata(t.Context(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{ValidateThumbnail: func(context.Context, string) error {
		fallbackCalls++
		return nil
	}})
	require.NoError(t, err)
	require.Zero(t, resolver.validationCalls)
	require.Zero(t, fallbackCalls)
	updated, err := actressRepo.FindByID(t.Context(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "https://existing.example/thumb.jpg", updated.ThumbURL)
	require.Equal(t, "New", updated.FirstName)
}

func TestSyncActressMetadataUsesDirectResolverWithoutLinkedMovies(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	actress := &models.Actress{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	scraper := &metadataOnlyActressScraper{info: models.ActressInfo{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)
	movieRepo := database.NewMovieRepository(db)
	_, err = movieRepo.Upsert(context.Background(), &models.Movie{ContentID: "linked", ID: "BMD-284", DisplayTitle: "Linked", Actresses: []models.Actress{*actress}})
	require.NoError(t, err)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	require.Equal(t, 1, scraper.calls)
	require.Zero(t, scraper.searchCalls)
	require.ElementsMatch(t, []string{"first_name", "last_name"}, result.UpdatedFields)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Asami", updated.FirstName)
	require.Equal(t, "Abe", updated.LastName)
}

func TestSyncActressMetadataIgnoresConflictsOnExistingFields(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	actress := &models.Actress{
		DMMID: 19244, JapaneseName: "安倍亜沙美",
		ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg",
	}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{
		DMMID: 19244, JapaneseName: "安倍亜沙美",
		ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/abe_asami.jpg",
	}})
	registry.RegisterInstance(&metadataOnlyActressScraper{name: "javdb", info: models.ActressInfo{
		DMMID: 19244, JapaneseName: "安倍亜沙美",
		ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg",
	}})
	registry.RegisterInstance(&metadataOnlyActressScraper{name: "minnanoav", info: models.ActressInfo{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg",
	}})

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, database.NewMovieRepository(db), registry)
	require.NoError(t, err)
	require.False(t, result.Conflict)
	require.ElementsMatch(t, []string{"first_name", "last_name"}, result.UpdatedFields)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Asami", updated.FirstName)
	require.Equal(t, "Abe", updated.LastName)
	require.Equal(t, actress.ThumbURL, updated.ThumbURL)
}

func TestSyncActressMetadataPreservesPriorMutationOutcomeOnReplay(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{
		DMMID: 943, FirstName: "Eri", LastName: "Imai", JapaneseName: "今井絵理", ThumbURL: "https://example.test/eri.jpg",
	})
	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, scraperutil.NewScraperRegistry(), ActressSyncOptions{PriorUpdatedFields: []string{"first_name", "last_name", "thumb_url"}})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"first_name", "last_name", "thumb_url"}, result.UpdatedFields)
	require.Empty(t, result.Messages)
}

func TestSyncActressMetadataReportsAlreadyComplete(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	actress := &models.Actress{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg",
	}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &metadataOnlyActressScraper{name: "minnanoav"}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, database.NewMovieRepository(db), registry)
	require.NoError(t, err)
	require.Equal(t, []string{"already_complete"}, result.Messages)
	require.Empty(t, result.UpdatedFields)
	require.Zero(t, resolver.calls)
}

func TestSyncActressMetadataSelectedUsesCacheFallbackAfterLiveResolver(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 943, JapaneseName: "今井絵理"})
	resolver := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 943, JapaneseName: "今井絵理"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	cacheCalls := 0

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
		Revalidate: true,
		LookupCache: func(int, string, string, string) (models.ActressInfo, bool) {
			cacheCalls++
			return models.ActressInfo{DMMID: 943, FirstName: "Eri", LastName: "Imai", JapaneseName: "今井絵理", ThumbURL: "https://cache.example/eri.jpg"}, true
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, cacheCalls)
	require.Equal(t, 1, resolver.calls)
	require.ElementsMatch(t, []string{"first_name", "last_name", "thumb_url"}, result.UpdatedFields)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Eri", updated.FirstName)
	require.Equal(t, "Imai", updated.LastName)
	require.Equal(t, "https://cache.example/eri.jpg", updated.ThumbURL)
}

func TestSyncActressMetadataRevalidatesCompleteSelection(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	actress := &models.Actress{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg",
	}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &metadataOnlyActressScraper{name: "minnanoav", info: models.ActressInfo{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg",
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, database.NewMovieRepository(db), registry, ActressSyncOptions{Revalidate: true})
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.Empty(t, result.Messages)
	require.Equal(t, []string{"thumb_url"}, result.UpdatedFields)
	require.Equal(t, 1, resolver.calls)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, resolver.info.ThumbURL, updated.ThumbURL)
}

func TestActressSyncManagerRevalidatesSelectedCompleteActress(t *testing.T) {
	db, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg",
	})
	resolver := &metadataOnlyActressScraper{name: "minnanoav", info: models.ActressInfo{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)

	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, 1, resolver.calls)
	require.Equal(t, models.ActressSyncTaskCompleted, tasks[0].Status)
	require.Equal(t, "verified", tasks[0].Outcome)
	require.Equal(t, []string{"verified_no_changes"}, tasks[0].Messages)
}

func TestSyncActressMetadataPreservesUserThumbnailDuringRevalidation(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	actress := &models.Actress{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://images.example.test/custom/asami.jpg",
	}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &metadataOnlyActressScraper{name: "minnanoav", info: models.ActressInfo{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg",
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, database.NewMovieRepository(db), registry, ActressSyncOptions{Revalidate: true})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, []string{"verified_no_changes"}, result.Messages)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, actress.ThumbURL, updated.ThumbURL)
}

func TestSyncActressMetadataRejectsInvalidResolverThumbnail(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	actress := &models.Actress{DMMID: 19244, JapaneseName: "安倍亜沙美"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &metadataOnlyActressScraper{name: "javdb", info: models.ActressInfo{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg",
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, database.NewMovieRepository(db), registry, ActressSyncOptions{
		ValidateThumbnail: func(context.Context, string) error { return fmt.Errorf("not an image") },
	})
	require.NoError(t, err)
	require.NotContains(t, result.UpdatedFields, "thumb_url")
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Empty(t, updated.ThumbURL)
}

func TestSyncActressMetadataMergesMissingDMMParentheticalDuplicate(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	canonical := &models.Actress{DMMID: 1084111, FirstName: "Miyū", LastName: "Kiyohara", JapaneseName: "小日向みゆう", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/kiyohara_miyuu.jpg"}
	duplicate := &models.Actress{JapaneseName: "小日向みゆう(清原みゆう）"}
	require.NoError(t, actressRepo.Create(context.Background(), canonical))
	require.NoError(t, actressRepo.Create(context.Background(), duplicate))

	result, err := SyncActressMetadata(context.Background(), duplicate.ID, actressRepo, database.NewMovieRepository(db), scraperutil.NewScraperRegistry())
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "merged_duplicate")
	_, err = actressRepo.FindByID(context.Background(), duplicate.ID)
	require.Error(t, err)
	merged, err := actressRepo.FindByID(context.Background(), canonical.ID)
	require.NoError(t, err)
	require.Equal(t, canonical.DMMID, merged.DMMID)
	require.Contains(t, merged.Aliases, duplicate.JapaneseName)
}

func TestSyncActressMetadataRecoversMissingDMMFromLinkedMovie(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "対象女優"})
	_, err := movieRepo.Upsert(context.Background(), &models.Movie{ContentID: "linked", ID: "ABC-123", DisplayTitle: "Linked", Actresses: []models.Actress{*actress}})
	require.NoError(t, err)
	scraper := &actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 4242, JapaneseName: "対象女優", FirstName: "Target", LastName: "Actress", ThumbURL: "thumb"}}}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "dmm_id")
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, 4242, updated.DMMID)
}

func TestSyncActressMetadataSelectedUsesVerifiedCacheAfterLiveRecoveryFails(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	canonical := &models.Actress{DMMID: 1084111, FirstName: "Miyū", LastName: "Kiyohara", JapaneseName: "小日向みゆう", ThumbURL: "https://www.minnano-av.com/p_actress_125_125/022/708761.jpg"}
	duplicate := &models.Actress{JapaneseName: "清原みゆう", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/kiyohara_miyuu.jpg"}
	require.NoError(t, actressRepo.Create(context.Background(), canonical))
	require.NoError(t, actressRepo.Create(context.Background(), duplicate))

	result, err := SyncActressMetadata(context.Background(), duplicate.ID, actressRepo, movieRepo, scraperutil.NewScraperRegistry(), ActressSyncOptions{
		Revalidate: true,
		LookupCache: func(dmmID int, japaneseName, firstName, lastName string) (models.ActressInfo, bool) {
			record, ok := actresscache.Lookup(dmmID, japaneseName, firstName, lastName)
			return models.ActressInfo{DMMID: record.DMMID, FirstName: record.FirstName, LastName: record.LastName, JapaneseName: record.JapaneseName, ThumbURL: record.ThumbURL}, ok
		},
	})
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "merged_duplicate")
	merged, err := actressRepo.FindByID(context.Background(), canonical.ID)
	require.NoError(t, err)
	require.Equal(t, 1084111, merged.DMMID)
	_, err = actressRepo.FindByID(context.Background(), duplicate.ID)
	require.Error(t, err)
}

func TestActressSyncManagerSelectedUsesCacheAliasForMissingDMM(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	canonical := &models.Actress{DMMID: 1084111, FirstName: "Miyū", LastName: "Kiyohara", JapaneseName: "小日向みゆう", ThumbURL: "https://www.minnano-av.com/p_actress_125_125/022/708761.jpg"}
	duplicate := &models.Actress{JapaneseName: "清原みゆう", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/kiyohara_miyuu.jpg"}
	require.NoError(t, actressRepo.Create(context.Background(), canonical))
	require.NoError(t, actressRepo.Create(context.Background(), duplicate))

	manager, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, duplicate.ID, scraperutil.NewScraperRegistry())
	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskCompleted, tasks[0].Status)
	require.Equal(t, "updated", tasks[0].Outcome)
	require.Contains(t, tasks[0].UpdatedFields, "merged_duplicate")
	require.NotNil(t, tasks[0].ActressID)
	require.Equal(t, canonical.ID, *tasks[0].ActressID)
	_, err = actressRepo.FindByID(context.Background(), duplicate.ID)
	require.Error(t, err)
}

func TestActressSyncManagerQueuesMissingDMMForCanonicalRecovery(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	canonical := &models.Actress{DMMID: 1084111, JapaneseName: "小日向みゆう"}
	duplicate := &models.Actress{JapaneseName: "小日向みゆう(清原みゆう）"}
	require.NoError(t, actressRepo.Create(context.Background(), canonical))
	require.NoError(t, actressRepo.Create(context.Background(), duplicate))

	manager, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, duplicate.ID, scraperutil.NewScraperRegistry())
	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskCompleted, tasks[0].Status)
	require.Equal(t, "updated", tasks[0].Outcome)
	require.Contains(t, tasks[0].UpdatedFields, "merged_duplicate")
	require.NotNil(t, tasks[0].ActressID)
	require.Equal(t, canonical.ID, *tasks[0].ActressID)
}

func TestSyncActressMetadataPrefersValidDMMThumbnailWithoutConflict(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{
		DMMID: 1009314, FirstName: "Yu", LastName: "Yasuda", JapaneseName: "安田ゆう",
		ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/yasuda_yuu",
	})
	dmm := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{
		DMMID: 1009314, JapaneseName: "安田ゆう", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/yasuda_yuu.jpg",
	}}
	minnano := &metadataOnlyActressScraper{name: "minnanoav", info: models.ActressInfo{
		DMMID: 1009314, FirstName: "Yu", LastName: "Yasuda", JapaneseName: "安田ゆう",
		ThumbURL: "https://www.minnano-av.com/p_actress_125_125/004/263840.jpg",
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmm)
	registry.RegisterInstance(minnano)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{Revalidate: true})
	require.NoError(t, err)
	require.False(t, result.Conflict)
	require.Contains(t, result.UpdatedFields, "thumb_url")
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, dmm.info.ThumbURL, updated.ThumbURL)
	require.Empty(t, dmm.lastInput.ThumbURL)
}

func TestSyncActressMetadataFallsBackToMinnanoWhenDMMThumbnailIsInvalid(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{
		DMMID: 1009314, FirstName: "Yu", LastName: "Yasuda", JapaneseName: "安田ゆう",
		ThumbURL: "https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/yasuda_yuu.jpg",
	})
	dmmThumbnail := "https://pics.dmm.co.jp/mono/actjpgs/yasuda_yuu.jpg"
	minnanoThumbnail := "https://www.minnano-av.com/p_actress_125_125/004/263840.jpg"
	dmm := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 1009314, JapaneseName: "安田ゆう", ThumbURL: dmmThumbnail}}
	minnano := &metadataOnlyActressScraper{name: "minnanoav", info: models.ActressInfo{DMMID: 1009314, FirstName: "Yu", LastName: "Yasuda", JapaneseName: "安田ゆう", ThumbURL: minnanoThumbnail}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmm)
	registry.RegisterInstance(minnano)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, ActressSyncOptions{
		Revalidate: true,
		ValidateThumbnail: func(_ context.Context, thumbnail string) error {
			if thumbnail == dmmThumbnail {
				return fmt.Errorf("invalid DMM image")
			}
			return nil
		},
	})
	require.NoError(t, err)
	require.False(t, result.Conflict)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, minnanoThumbnail, updated.ThumbURL)
}

func TestSyncActressMetadataDoesNotMergeAmbiguousDMMDuplicate(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "同名女優(別名)"})
	canonicalOne := &models.Actress{DMMID: 1001, JapaneseName: "同名女優", FirstName: "One"}
	canonicalTwo := &models.Actress{DMMID: 1002, JapaneseName: "同名女優", FirstName: "Two"}
	require.NoError(t, actressRepo.Create(context.Background(), canonicalOne))
	require.NoError(t, actressRepo.Create(context.Background(), canonicalTwo))

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, scraperutil.NewScraperRegistry())
	require.NoError(t, err)
	require.Equal(t, []string{"missing_dmm_id"}, result.Messages)
	_, err = actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
}

func TestSyncActressMetadataFallsBackToLinkedMovieAfterDirectResolverMiss(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 4242, JapaneseName: "対象女優"})
	_, err := movieRepo.Upsert(context.Background(), &models.Movie{ContentID: "linked", ID: "ABC-123", DisplayTitle: "Linked", Actresses: []models.Actress{*actress}})
	require.NoError(t, err)
	scraper := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 4242, JapaneseName: "対象女優"}, searchResult: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 4242, FirstName: "Target", LastName: "Actress", JapaneseName: "対象女優", ThumbURL: "thumb"}}}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	require.Equal(t, 1, scraper.calls)
	require.Equal(t, 1, scraper.searchCalls)
	require.ElementsMatch(t, []string{"first_name", "last_name", "thumb_url"}, result.UpdatedFields)
}

func TestSyncActressMetadataUsesDMMThumbnailPriorityForMissingScope(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 4243, JapaneseName: "優先女優"})
	dmm := &metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 4243, JapaneseName: "優先女優", FirstName: "DMM", LastName: "Name", ThumbURL: "https://dmm.example/thumb.jpg"}}
	minnano := &metadataOnlyActressScraper{name: "minnanoav", info: models.ActressInfo{DMMID: 4243, JapaneseName: "優先女優", FirstName: "DMM", LastName: "Name", ThumbURL: "https://minnano.example/thumb.jpg"}}
	javdb := &metadataOnlyActressScraper{name: "javdb", info: models.ActressInfo{DMMID: 4243, JapaneseName: "優先女優", FirstName: "DMM", LastName: "Name", ThumbURL: "https://javdb.example/thumb.jpg"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(javdb)
	registry.RegisterInstance(minnano)
	registry.RegisterInstance(dmm)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "thumb_url")
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, dmm.info.ThumbURL, updated.ThumbURL)
}

func TestSyncActressMetadataDoesNotMergeAmbiguousDMMDuplicateWithLinkedMatch(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{JapaneseName: "同名女優(別名)"})
	canonicalOne := &models.Actress{DMMID: 1001, JapaneseName: "同名女優", FirstName: "One"}
	canonicalTwo := &models.Actress{DMMID: 1002, JapaneseName: "同名女優", FirstName: "Two"}
	require.NoError(t, actressRepo.Create(context.Background(), canonicalOne))
	require.NoError(t, actressRepo.Create(context.Background(), canonicalTwo))
	_, err := movieRepo.Upsert(context.Background(), &models.Movie{ContentID: "linked-ambiguous", ID: "AMB-001", DisplayTitle: "Linked", Actresses: []models.Actress{*actress}})
	require.NoError(t, err)
	scraper := &actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 1001, JapaneseName: "同名女優", FirstName: "One", LastName: "Canonical"}}}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	require.Equal(t, []string{"missing_dmm_id"}, result.Messages)
	_, err = actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
}
