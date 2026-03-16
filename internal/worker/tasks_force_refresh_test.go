package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockScraper for testing
type MockScraper struct {
	name    string
	results *models.ScraperResult
	err     error
}

func (m *MockScraper) Name() string                                    { return m.name }
func (m *MockScraper) IsEnabled() bool                                 { return true }
func (m *MockScraper) GetURL(id string) (string, error)                { return "", nil }
func (m *MockScraper) Search(id string) (*models.ScraperResult, error) { return m.results, m.err }

// TestScrapeTask_ForceRefresh tests that forceRefresh deletes from cache before scraping
func TestScrapeTask_ForceRefresh(t *testing.T) {
	// Setup test database
	testDBPath := filepath.Join(os.TempDir(), "test_force_refresh.db")
	defer func() { _ = os.Remove(testDBPath) }()

	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  testDBPath,
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:          []string{"dmm"},
				ContentID:   []string{"dmm"},
				Title:       []string{"dmm"},
				Maker:       []string{"dmm"},
				Description: []string{"dmm"},
				Actress:     []string{"dmm"},
				Genre:       []string{"dmm"},
			},
		},
	}

	db, err := database.New(dbCfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Run migrations
	err = db.AutoMigrate()
	require.NoError(t, err)

	movieRepo := database.NewMovieRepository(db)

	// Setup aggregator config
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:          []string{"dmm"},
				ContentID:   []string{"dmm"},
				Title:       []string{"dmm"},
				Maker:       []string{"dmm"},
				Description: []string{"dmm"},
				Actress:     []string{"dmm"},
				Genre:       []string{"dmm"},
			},
		},
	}

	agg := aggregator.New(cfg)
	progressChan := make(chan ProgressUpdate, 100)
	progressTracker := NewProgressTracker(progressChan)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	// Create mock scraper with Japanese data
	dmmScraper := &MockScraper{
		name: "dmm",
		results: &models.ScraperResult{
			Source:      "dmm",
			Language:    "ja",
			ID:          "TEST-001",
			ContentID:   "test001",
			Title:       "日本語タイトル",
			Maker:       "日本のメーカー",
			Description: "日本語の説明",
			Genres:      []string{"ドラマ"},
			ReleaseDate: &releaseDate,
			Runtime:     120,
		},
	}

	registry := models.NewScraperRegistry()
	registry.Register(dmmScraper)

	// Pre-populate database with old English data
	oldMovie := &models.Movie{
		ID:          "TEST-001",
		Title:       "Old English Title",
		Maker:       "Old English Maker",
		Description: "Old English Description",
	}
	err = movieRepo.Upsert(oldMovie)
	require.NoError(t, err)

	// Verify old data is in cache
	cachedMovie, err := movieRepo.FindByID("TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "Old English Title", cachedMovie.Title)

	t.Run("Without forceRefresh - uses cached data", func(t *testing.T) {
		// Create scrape task without force refresh
		task := NewScrapeTask(
			"TEST-001",
			registry,
			agg,
			movieRepo,
			progressTracker,
			false, // dryRun
			false, // forceRefresh
			nil,   // no custom scraper priority
		)

		// Execute task
		ctx := context.Background()
		err := task.Execute(ctx)
		require.NoError(t, err)

		// Should still have old data (from cache)
		movie, err := movieRepo.FindByID("TEST-001")
		require.NoError(t, err)
		assert.Equal(t, "Old English Title", movie.Title, "Should use cached data")
	})

	t.Run("With forceRefresh - deletes cache and rescrapes", func(t *testing.T) {
		// Create scrape task with force refresh
		task := NewScrapeTask(
			"TEST-001",
			registry,
			agg,
			movieRepo,
			progressTracker,
			false, // dryRun
			true,  // forceRefresh
			nil,   // no custom scraper priority
		)

		// Execute task
		ctx := context.Background()
		err := task.Execute(ctx)
		require.NoError(t, err)

		// Should have new Japanese data (from scraper)
		movie, err := movieRepo.FindByID("TEST-001")
		require.NoError(t, err)
		assert.Equal(t, "日本語タイトル", movie.Title, "Should have new data from scraper")
		assert.Equal(t, "日本のメーカー", movie.Maker, "Should have new maker from scraper")
		assert.Equal(t, "日本語の説明", movie.Description, "Should have new description from scraper")
	})
}

// TestMovieRepository_DeleteWithTranslations tests that Delete removes translations
func TestMovieRepository_DeleteWithTranslations(t *testing.T) {
	testDBPath := filepath.Join(os.TempDir(), "test_delete_translations.db")
	defer func() { _ = os.Remove(testDBPath) }()

	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  testDBPath,
		},
	}

	db, err := database.New(dbCfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err)

	movieRepo := database.NewMovieRepository(db)

	// Create a movie with translations
	movie := &models.Movie{
		ContentID: "testdelete", // ContentID is the primary key
		ID:        "TEST-DELETE",
		Title:     "Test Movie",
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "English Title"}, // MovieID will be set by Upsert
			{Language: "ja", Title: "日本語タイトル"},       // MovieID will be set by Upsert
		},
	}

	err = movieRepo.Upsert(movie)
	require.NoError(t, err)

	// Verify movie and translations exist
	found, err := movieRepo.FindByID("TEST-DELETE")
	require.NoError(t, err)
	assert.Equal(t, "Test Movie", found.Title)

	// Check translations exist in database directly
	var translationCount int64
	db.DB.Model(&models.MovieTranslation{}).Where("movie_id = ?", "testdelete").Count(&translationCount)
	assert.Equal(t, int64(2), translationCount, "Should have 2 translations")

	// Delete the movie
	err = movieRepo.Delete("TEST-DELETE")
	require.NoError(t, err)

	// Verify movie is gone
	_, err = movieRepo.FindByID("TEST-DELETE")
	assert.Error(t, err, "Movie should not exist after delete")

	// Verify translations are also gone
	db.DB.Model(&models.MovieTranslation{}).Where("movie_id = ?", "testdelete").Count(&translationCount)
	assert.Equal(t, int64(0), translationCount, "Translations should be deleted with movie")
}

// TestScrapeTask_ForceRefresh_NotInCache tests forceRefresh when movie doesn't exist in cache
func TestScrapeTask_ForceRefresh_NotInCache(t *testing.T) {
	// Setup test database
	testDBPath := filepath.Join(os.TempDir(), "test_force_refresh_not_in_cache.db")
	defer func() { _ = os.Remove(testDBPath) }()

	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  testDBPath,
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:          []string{"dmm"},
				ContentID:   []string{"dmm"},
				Title:       []string{"dmm"},
				Maker:       []string{"dmm"},
				Description: []string{"dmm"},
				Actress:     []string{"dmm"},
				Genre:       []string{"dmm"},
			},
		},
	}

	db, err := database.New(dbCfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Run migrations
	err = db.AutoMigrate()
	require.NoError(t, err)

	movieRepo := database.NewMovieRepository(db)

	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:          []string{"dmm"},
				ContentID:   []string{"dmm"},
				Title:       []string{"dmm"},
				Maker:       []string{"dmm"},
				Description: []string{"dmm"},
				Actress:     []string{"dmm"},
				Genre:       []string{"dmm"},
			},
		},
	}

	agg := aggregator.New(cfg)
	progressChan := make(chan ProgressUpdate, 100)
	progressTracker := NewProgressTracker(progressChan)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	dmmScraper := &MockScraper{
		name: "dmm",
		results: &models.ScraperResult{
			Source:      "dmm",
			Language:    "ja",
			ID:          "TEST-002",
			Title:       "新しい日本語タイトル",
			Maker:       "新しいメーカー",
			ReleaseDate: &releaseDate,
			Runtime:     120,
		},
	}

	registry := models.NewScraperRegistry()
	registry.Register(dmmScraper)

	// Verify movie doesn't exist in cache
	_, err = movieRepo.FindByID("TEST-002")
	assert.Error(t, err, "Should not exist in cache")

	// Create scrape task with force refresh
	task := NewScrapeTask(
		"TEST-002",
		registry,
		agg,
		movieRepo,
		progressTracker,
		false, // dryRun
		true,  // forceRefresh
		nil,   // no custom scraper priority
	)

	// Execute task - should not fail even though movie doesn't exist
	ctx := context.Background()
	err = task.Execute(ctx)
	require.NoError(t, err)

	// Should have scraped new data
	movie, err := movieRepo.FindByID("TEST-002")
	require.NoError(t, err)
	assert.Equal(t, "新しい日本語タイトル", movie.Title)
	assert.Equal(t, "新しいメーカー", movie.Maker)
}
