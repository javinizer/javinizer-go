package commandutil

import (
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapeMetadata_Success(t *testing.T) {
	_, testCfg := createTestConfig(t)
	deps := createTestDependencies(t, testCfg)
	defer func() { _ = deps.Close() }()

	// Create test matches
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
	}

	// Setup mock scraper with result
	mockResult := &models.ScraperResult{
		ID:        "IPX-123",
		ContentID: "ipx00123",
		Title:     "Test Movie",
	}
	mockScraper := NewMockScraper("testscraper")
	mockScraper.AddResult("IPX-123", mockResult)

	registry := models.NewScraperRegistry()
	registry.Register(mockScraper)

	agg := aggregator.NewWithDatabase(testCfg, deps.DB)
	movieRepo := database.NewMovieRepository(deps.DB)

	movies, scrapedCount, cachedCount, err := ScrapeMetadata(
		matches, movieRepo, registry, agg, []string{"testscraper"}, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, len(movies))
	assert.Equal(t, 1, scrapedCount)
	assert.Equal(t, 0, cachedCount)
	assert.NotNil(t, movies["IPX-123"], "movie should be in result map")
}

// TestScrapeMetadata_CacheHit tests metadata retrieval from cache
func TestScrapeMetadata_CacheHit(t *testing.T) {
	_, testCfg := createTestConfig(t)
	deps := createTestDependencies(t, testCfg)
	defer func() { _ = deps.Close() }()

	// Pre-populate cache
	movieRepo := database.NewMovieRepository(deps.DB)
	cachedMovie := createTestMovie("IPX-123", "Cached Movie")
	err := movieRepo.Upsert(cachedMovie)
	require.NoError(t, err)

	// Create test matches
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
	}

	registry := models.NewScraperRegistry()
	agg := aggregator.NewWithDatabase(testCfg, deps.DB)

	movies, scrapedCount, cachedCount, err := ScrapeMetadata(
		matches, movieRepo, registry, agg, []string{}, false, // forceRefresh = false
	)

	require.NoError(t, err)
	assert.Equal(t, 1, len(movies))
	assert.Equal(t, 0, scrapedCount, "should not scrape when cache hit")
	assert.Equal(t, 1, cachedCount, "should return cached movie")
	assert.Equal(t, "Cached Movie", movies["IPX-123"].Title)
}

// TestScrapeMetadata_ForceRefresh tests cache clearing with force refresh
func TestScrapeMetadata_ForceRefresh(t *testing.T) {
	_, testCfg := createTestConfig(t)
	deps := createTestDependencies(t, testCfg)
	defer func() { _ = deps.Close() }()

	// Pre-populate cache
	movieRepo := database.NewMovieRepository(deps.DB)
	cachedMovie := createTestMovie("IPX-123", "Cached Movie")
	err := movieRepo.Upsert(cachedMovie)
	require.NoError(t, err)

	// Create test matches
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
	}

	// Setup mock scraper with fresh result
	mockResult := &models.ScraperResult{
		ID:        "IPX-123",
		ContentID: "ipx00123",
		Title:     "Fresh Movie",
	}
	mockScraper := NewMockScraper("testscraper")
	mockScraper.AddResult("IPX-123", mockResult)

	registry := models.NewScraperRegistry()
	registry.Register(mockScraper)

	agg := aggregator.NewWithDatabase(testCfg, deps.DB)

	movies, scrapedCount, cachedCount, err := ScrapeMetadata(
		matches, movieRepo, registry, agg, []string{"testscraper"}, true, // forceRefresh = true
	)

	require.NoError(t, err)
	assert.Equal(t, 1, len(movies))
	assert.Equal(t, 1, scrapedCount, "should scrape when force refresh")
	assert.Equal(t, 0, cachedCount, "should not use cache when force refresh")
	assert.NotNil(t, movies["IPX-123"], "movie should be freshly scraped")
}

// TestScrapeMetadata_EmptyMatches tests handling of empty matches
func TestScrapeMetadata_EmptyMatches(t *testing.T) {
	_, testCfg := createTestConfig(t)
	deps := createTestDependencies(t, testCfg)
	defer func() { _ = deps.Close() }()

	movieRepo := database.NewMovieRepository(deps.DB)
	registry := models.NewScraperRegistry()
	agg := aggregator.NewWithDatabase(testCfg, deps.DB)

	stdout, _ := captureOutput(t, func() {
		movies, scrapedCount, cachedCount, err := ScrapeMetadata(
			[]matcher.MatchResult{}, // Empty matches
			movieRepo, registry, agg, []string{}, false,
		)

		require.NoError(t, err)
		assert.Nil(t, movies)
		assert.Equal(t, 0, scrapedCount)
		assert.Equal(t, 0, cachedCount)
	})

	assert.Contains(t, stdout, "No metadata found")
}

// TestScrapeMetadata_NoResults tests when no scrapers return results
func TestScrapeMetadata_NoResults(t *testing.T) {
	_, testCfg := createTestConfig(t)
	deps := createTestDependencies(t, testCfg)
	defer func() { _ = deps.Close() }()

	// Create test matches
	matches := []matcher.MatchResult{
		{ID: "IPX-999", File: scanner.FileInfo{Name: "IPX-999.mp4", Path: "/test/IPX-999.mp4"}},
	}

	// Setup mock scraper that returns error (no results)
	mockScraper := NewMockScraper("testscraper")
	mockScraper.AddError("IPX-999", fmt.Errorf("not found"))

	registry := models.NewScraperRegistry()
	registry.Register(mockScraper)

	agg := aggregator.NewWithDatabase(testCfg, deps.DB)
	movieRepo := database.NewMovieRepository(deps.DB)

	stdout, _ := captureOutput(t, func() {
		movies, scrapedCount, cachedCount, err := ScrapeMetadata(
			matches, movieRepo, registry, agg, []string{"testscraper"}, false,
		)

		require.NoError(t, err)
		assert.Nil(t, movies)
		assert.Equal(t, 0, scrapedCount)
		assert.Equal(t, 0, cachedCount)
	})

	assert.Contains(t, stdout, "not found")
}

// TestScrapeMetadata_MultipleIDs tests scraping multiple movies
func TestScrapeMetadata_MultipleIDs(t *testing.T) {
	_, testCfg := createTestConfig(t)
	deps := createTestDependencies(t, testCfg)
	defer func() { _ = deps.Close() }()

	// Create test matches for multiple IDs
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
		{ID: "IPX-456", File: scanner.FileInfo{Name: "IPX-456.mp4", Path: "/test/IPX-456.mp4"}},
		{ID: "IPX-789", File: scanner.FileInfo{Name: "IPX-789.mp4", Path: "/test/IPX-789.mp4"}},
	}

	// Setup mock scraper with results for 2 out of 3
	mockScraper := NewMockScraper("testscraper")
	mockScraper.AddResult("IPX-123", &models.ScraperResult{ID: "IPX-123", ContentID: "ipx00123", Title: "Movie 1"})
	mockScraper.AddResult("IPX-456", &models.ScraperResult{ID: "IPX-456", ContentID: "ipx00456", Title: "Movie 2"})
	mockScraper.AddError("IPX-789", fmt.Errorf("not found"))

	registry := models.NewScraperRegistry()
	registry.Register(mockScraper)

	agg := aggregator.NewWithDatabase(testCfg, deps.DB)
	movieRepo := database.NewMovieRepository(deps.DB)

	movies, scrapedCount, cachedCount, err := ScrapeMetadata(
		matches, movieRepo, registry, agg, []string{"testscraper"}, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 2, len(movies), "should find 2 out of 3")
	assert.Equal(t, 2, scrapedCount)
	assert.Equal(t, 0, cachedCount)
	assert.NotNil(t, movies["IPX-123"])
	assert.NotNil(t, movies["IPX-456"])
	assert.Nil(t, movies["IPX-789"], "failed movie should not be in map")
}

// TestScrapeMetadata_MixedCacheAndScrape tests mix of cached and fresh scrapes
func TestScrapeMetadata_MixedCacheAndScrape(t *testing.T) {
	_, testCfg := createTestConfig(t)
	deps := createTestDependencies(t, testCfg)
	defer func() { _ = deps.Close() }()

	// Pre-populate cache with one movie
	movieRepo := database.NewMovieRepository(deps.DB)
	cachedMovie := createTestMovie("IPX-123", "Cached Movie")
	err := movieRepo.Upsert(cachedMovie)
	require.NoError(t, err)

	// Create test matches for multiple IDs
	matches := []matcher.MatchResult{
		{ID: "IPX-123", File: scanner.FileInfo{Name: "IPX-123.mp4", Path: "/test/IPX-123.mp4"}},
		{ID: "IPX-456", File: scanner.FileInfo{Name: "IPX-456.mp4", Path: "/test/IPX-456.mp4"}},
	}

	// Setup mock scraper for the uncached movie
	mockScraper := NewMockScraper("testscraper")
	mockScraper.AddResult("IPX-456", &models.ScraperResult{ID: "IPX-456", ContentID: "ipx00456", Title: "Fresh Movie"})

	registry := models.NewScraperRegistry()
	registry.Register(mockScraper)

	agg := aggregator.NewWithDatabase(testCfg, deps.DB)

	movies, scrapedCount, cachedCount, err := ScrapeMetadata(
		matches, movieRepo, registry, agg, []string{"testscraper"}, false,
	)

	require.NoError(t, err)
	assert.Equal(t, 2, len(movies))
	assert.Equal(t, 1, scrapedCount, "one movie scraped fresh")
	assert.Equal(t, 1, cachedCount, "one movie from cache")
	// Verify both movies are present
	assert.NotNil(t, movies["IPX-123"])
	assert.NotNil(t, movies["IPX-456"])
	// Cached movie should preserve its title
	assert.Equal(t, "Cached Movie", movies["IPX-123"].Title)
}
