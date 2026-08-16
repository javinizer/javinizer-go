package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestSyncActressMetadataAdditionalBranches(t *testing.T) {
	boom := errors.New("boom")
	cache := func(dmmID int, japaneseName, firstName, lastName string) (models.ActressInfo, bool) {
		return models.ActressInfo{DMMID: 44, JapaneseName: japaneseName, FirstName: "Cache", LastName: "Name", ThumbURL: "cache-thumb"}, true
	}

	t.Run("cache canonical mismatch", func(t *testing.T) {
		_, repo, movies, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "source"})
		require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 44, JapaneseName: "other"}))
		result, err := SyncActressMetadata(context.Background(), source.ID, repo, movies, nil, ActressSyncOptions{LookupCache: cache})
		require.NoError(t, err)
		require.Contains(t, result.Messages, "missing_dmm_id")
	})

	t.Run("assignment reload error", func(t *testing.T) {
		db, repo, movies, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "source"})
		_, err := SyncActressMetadata(context.Background(), source.ID, repo, movies, nil, ActressSyncOptions{
			LookupCache: cache,
			AssignDMMID: func(uint, int) (bool, error) { require.NoError(t, db.Close()); return true, nil },
		})
		require.Error(t, err)
	})

	t.Run("revalidate cache error after recovery miss", func(t *testing.T) {
		_, repo, movies, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "source"})
		_, err := SyncActressMetadata(context.Background(), source.ID, repo, movies, nil, ActressSyncOptions{
			Revalidate: true, LookupCache: cache,
			AssignDMMID: func(uint, int) (bool, error) { return false, boom },
		})
		require.ErrorIs(t, err, boom)
	})

	t.Run("cache complete and reload error", func(t *testing.T) {
		for _, failReload := range []bool{false, true} {
			t.Run(map[bool]string{false: "already complete", true: "reload error"}[failReload], func(t *testing.T) {
				db, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 44, JapaneseName: "名", FirstName: "A", LastName: "B", ThumbURL: "thumb"})
				result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, nil, ActressSyncOptions{
					LookupCache: cache,
					FillMetadata: func(uint, int, models.ActressInfo) ([]string, error) {
						if failReload {
							require.NoError(t, db.Close())
							return []string{"first_name"}, nil
						}
						return nil, nil
					},
				})
				if failReload {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.Contains(t, result.Messages, "already_complete")
				}
			})
		}
	})

	t.Run("empty metadata and thumbnail resolver branches", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			actress   *models.Actress
			scraper   models.Scraper
			validator func(context.Context, string) error
		}{
			{name: "empty metadata", actress: &models.Actress{DMMID: 44}, scraper: &metadataOnlyActressScraper{name: "dmm"}},
			{name: "complete thumbnail skips resolver", actress: &models.Actress{DMMID: 44, ThumbURL: "thumb"}, scraper: &actressSyncScraper{name: "dmm", thumbnail: "new"}},
			{name: "thumbnail validation rejects", actress: &models.Actress{DMMID: 44, ThumbURL: ""}, scraper: &actressSyncScraper{name: "dmm", thumbnail: "new"}, validator: func(context.Context, string) error { return boom }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, repo, movies, actress := newActressSyncFixture(t, tc.actress)
				registry := scraperutil.NewScraperRegistry()
				registry.RegisterInstance(tc.scraper)
				_, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, registry, ActressSyncOptions{ValidateThumbnail: tc.validator, FillMetadata: func(uint, int, models.ActressInfo) ([]string, error) { return nil, nil }})
				require.NoError(t, err)
			})
		}
	})

	t.Run("linked movie error", func(t *testing.T) {
		_, repo, _, actress := newActressSyncFixture(t, &models.Actress{DMMID: 44})
		closed, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "closed.db")})
		require.NoError(t, err)
		movies := database.NewMovieRepository(closed)
		require.NoError(t, closed.Close())
		_, err = SyncActressMetadata(context.Background(), actress.ID, repo, movies, nil)
		require.Error(t, err)
	})

	t.Run("replace thumbnail error", func(t *testing.T) {
		_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 44, JapaneseName: "名", FirstName: "A", LastName: "B", ThumbURL: "https://c0.jdbstatic.com/old.jpg"})
		registry := scraperutil.NewScraperRegistry()
		registry.RegisterInstance(&metadataOnlyActressScraper{name: "dmm", info: models.ActressInfo{DMMID: 44, JapaneseName: "名", FirstName: "A", LastName: "B", ThumbURL: "https://pics.dmm.co.jp/new.jpg"}})
		_, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, registry, ActressSyncOptions{
			Revalidate:       true,
			FillMetadata:     func(uint, int, models.ActressInfo) ([]string, error) { return nil, nil },
			ReplaceThumbnail: func(uint, int, string, string) (bool, error) { return false, boom },
		})
		require.ErrorIs(t, err, boom)
	})
}
