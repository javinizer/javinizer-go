package worker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestSyncActressMetadataUsesJavDBResolverWithoutLinkedMovies(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	actressRepo := database.NewActressRepository(db)
	actress := &models.Actress{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	scraper := &metadataOnlyActressScraper{name: "javdb", info: models.ActressInfo{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(scraper)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, database.NewMovieRepository(db), registry)
	require.NoError(t, err)
	require.Equal(t, 1, scraper.calls)
	require.ElementsMatch(t, []string{"first_name", "last_name"}, result.UpdatedFields)
}
