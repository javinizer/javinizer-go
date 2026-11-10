package scrape_test

import (
	"context"
	"os"
	"regexp/syntax"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestRunMatcherFailurePreservesBorrowedDatabase(t *testing.T) {
	path, db := setupTestDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	cfg, err := config.Load(path)
	require.NoError(t, err)
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "[unterminated"
	require.NoError(t, config.Save(cfg, path))
	_, err = config.LoadOrCreate(path)
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(NewMockScraper("mock1"))
	registry.RegisterInstance(NewMockScraper("mock2"))
	t.Cleanup(func() { require.NoError(t, registry.Close()) })
	deps := &commandutil.CoreDeps{DB: db, ScraperRegistry: registry}
	repo := database.NewMovieRepository(db)
	original := createTestMovie("TEST-001", "Existing cached movie")
	require.NoError(t, repo.Create(context.Background(), original))

	movie, results, err := scrape.Run(context.Background(), scrape.NewCommand(), []string{"TEST-001"}, path, deps)
	require.Nil(t, movie)
	require.Nil(t, results)
	require.ErrorContains(t, err, "failed to create factory config: buildMatcher:")
	var syntaxErr *syntax.Error
	require.ErrorAs(t, err, &syntaxErr)
	require.Equal(t, syntax.ErrMissingBracket, syntaxErr.Code)
	require.Equal(t, cfg.Matching.RegexPattern, syntaxErr.Expr)
	got, err := repo.FindByID(context.Background(), original.ID)
	require.NoError(t, err, "a failed command must not close its caller's database")
	require.Equal(t, original.Title, got.Title)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "runtime workflow validation must not rewrite the invalid regex")
}
