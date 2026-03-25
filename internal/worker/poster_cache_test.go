package worker

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPosterRegenerationOnCacheHit verifies that temp posters are regenerated
// when a movie is retrieved from cache in a subsequent scrape job.
//
// Test scenario:
// 1. First scrape: Downloads metadata, generates temp poster for job1
// 2. Save to database (should NOT include temp poster URL)
// 3. Second scrape (same movie, different job): Retrieve from cache
// 4. Verify temp poster is regenerated for job2
// 5. Verify database doesn't contain temp poster URLs
func TestPosterRegenerationOnCacheHit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	chdirToTempDir(t)

	// Setup: Create temp directories
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	tempPosterDir := filepath.Join("data", "temp", "posters")

	// Ensure temp poster directory exists
	err := os.MkdirAll(tempPosterDir, 0755)
	require.NoError(t, err, "Failed to create temp poster directory")

	// Create test HTTP server for poster downloads with an in-memory JPEG.
	var imageBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	for y := 0; y < 1500; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{R: 80, G: 120, B: 180, A: 255})
		}
	}
	require.NoError(t, jpeg.Encode(&imageBuf, img, &jpeg.Options{Quality: 90}))
	testImageData := imageBuf.Bytes()

	posterDownloadCount := 0
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posterDownloadCount++
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(testImageData)
	}))
	defer testServer.Close()

	// Setup database
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mock-poster-test"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				// Empty priority = use scraper priority order
			},
		},
		Matching: config.MatchingConfig{
			Extensions:   []string{".mp4", ".mkv"},
			RegexPattern: `(?i)([a-z]{2,5})-?(\d{2,5})`,
			RegexEnabled: true,
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err, "Failed to create database")
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err, "Failed to migrate database")

	movieRepo := database.NewMovieRepository(db)

	// Create mock scraper
	mockScraper := &mockScraperForPosterTest{
		posterURL: testServer.URL + "/poster.jpg",
	}
	registry := models.NewScraperRegistry()
	registry.Register(mockScraper)

	agg := aggregator.NewWithDatabase(cfg, db)
	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err, "Failed to create matcher")

	httpClient := testServer.Client()
	userAgent := "test-agent"
	referer := "test-referer"

	// ==========================================
	// STEP 1: First scrape (fresh, not from cache)
	// ==========================================
	t.Log("STEP 1: First scrape - should generate temp poster for job1")

	job1 := &BatchJob{
		ID: "job1-test-cache-poster",
	}
	ctx1 := context.Background()
	filePath := "/fake/video/TEST-001.mp4"

	movie1, result1, err := RunBatchScrapeOnce(
		ctx1,
		job1,
		filePath,
		0,
		"", // no query override
		registry,
		agg,
		movieRepo,
		mat,
		httpClient,
		userAgent,
		referer,
		false,            // force=false
		false,            // updateMode=false
		nil,              // no selected scrapers
		nil,              // no processedMovieIDs tracking
		cfg,              // config
		"prefer-scraper", // scalarStrategy
		"merge",          // arrayStrategy
	)

	require.NoError(t, err, "First scrape should succeed")
	require.NotNil(t, movie1, "Movie should be returned")
	require.NotNil(t, result1, "FileResult should be returned")
	assert.Equal(t, JobStatusCompleted, result1.Status, "First scrape should complete successfully")

	// Verify temp poster was generated for job1
	tempPoster1Path := filepath.Join(tempPosterDir, job1.ID, movie1.ID+".jpg")
	_, err = os.Stat(tempPoster1Path)
	assert.NoError(t, err, "Temp poster for job1 should exist: %s", tempPoster1Path)

	// Verify movie has temp poster URL in FileResult
	assert.Contains(t, movie1.CroppedPosterURL, job1.ID, "FileResult should have temp poster URL with job1 ID")

	// ==========================================
	// STEP 2: Verify database doesn't have temp poster URL
	// ==========================================
	t.Log("STEP 2: Verify database has no temp poster URL")

	// Retrieve movie from database
	dbMovie, err := movieRepo.FindByID(movie1.ID)
	require.NoError(t, err, "Should be able to retrieve movie from database")
	assert.Empty(t, dbMovie.CroppedPosterURL, "Database should NOT contain temp poster URL (should be cleared)")

	// ==========================================
	// STEP 3: Second scrape (from cache, different job)
	// ==========================================
	t.Log("STEP 3: Second scrape - should retrieve from cache and regenerate temp poster for job2")

	job2 := &BatchJob{
		ID: "job2-test-cache-poster",
	}
	ctx2 := context.Background()

	// Reset poster download count to track if poster is re-downloaded
	posterDownloadCountBefore := posterDownloadCount

	movie2, result2, err := RunBatchScrapeOnce(
		ctx2,
		job2,
		filePath,
		0,
		"", // no query override
		registry,
		agg,
		movieRepo,
		mat,
		httpClient,
		userAgent,
		referer,
		false,            // force=false (use cache)
		false,            // updateMode=false
		nil,              // no selected scrapers
		nil,              // no processedMovieIDs tracking
		cfg,              // config
		"prefer-scraper", // scalarStrategy
		"merge",          // arrayStrategy
	)

	require.NoError(t, err, "Second scrape should succeed (cache hit)")
	require.NotNil(t, movie2, "Movie should be returned from cache")
	require.NotNil(t, result2, "FileResult should be returned")
	assert.Equal(t, JobStatusCompleted, result2.Status, "Second scrape should complete successfully")

	// ==========================================
	// STEP 4: Verify temp poster was regenerated for job2
	// ==========================================
	t.Log("STEP 4: Verify temp poster was regenerated for job2")

	// Verify temp poster exists for job2 (different directory than job1)
	tempPoster2Path := filepath.Join(tempPosterDir, job2.ID, movie2.ID+".jpg")
	_, err = os.Stat(tempPoster2Path)
	assert.NoError(t, err, "Temp poster for job2 should exist: %s", tempPoster2Path)

	// Verify movie has temp poster URL with job2 ID (not job1)
	assert.Contains(t, movie2.CroppedPosterURL, job2.ID, "FileResult should have temp poster URL with job2 ID (not job1)")
	assert.NotContains(t, movie2.CroppedPosterURL, job1.ID, "FileResult should NOT have job1 ID in poster URL")

	// Verify poster was downloaded (should be re-downloaded for temp poster generation)
	assert.Greater(t, posterDownloadCount, posterDownloadCountBefore, "Poster should be downloaded again for job2 temp poster")

	// ==========================================
	// STEP 5: Verify database still has no temp poster URL
	// ==========================================
	t.Log("STEP 5: Verify database still has no temp poster URL after cache hit")

	dbMovie2, err := movieRepo.FindByID(movie2.ID)
	require.NoError(t, err, "Should be able to retrieve movie from database")
	assert.Empty(t, dbMovie2.CroppedPosterURL, "Database should STILL not contain temp poster URL (cleared on both saves)")

	// ==========================================
	// STEP 6: Simulate cleanup (as would happen on job completion)
	// ==========================================
	t.Log("STEP 6: Simulate cleanup - remove temp posters for completed jobs")

	// Cleanup job1 temp posters
	job1TempDir := filepath.Join(tempPosterDir, job1.ID)
	err = os.RemoveAll(job1TempDir)
	assert.NoError(t, err, "Should be able to cleanup job1 temp posters")

	// Verify job1 temp poster is gone
	_, err = os.Stat(tempPoster1Path)
	assert.True(t, os.IsNotExist(err), "Job1 temp poster should be deleted after cleanup")

	// Verify job2 temp poster still exists (job2 not completed yet)
	_, err = os.Stat(tempPoster2Path)
	assert.NoError(t, err, "Job2 temp poster should still exist")

	t.Log("Test completed successfully - poster regeneration works correctly!")
}

// mockScraperForPosterTest is a mock scraper for testing poster regeneration
type mockScraperForPosterTest struct {
	posterURL string
}

func (m *mockScraperForPosterTest) Name() string {
	return "mock-poster-test"
}

func (m *mockScraperForPosterTest) Search(id string) (*models.ScraperResult, error) {
	// Accept both "TEST-001" and "TEST-1" and "TEST" (for different matching patterns)
	if id != "TEST-001" && id != "TEST-1" && id != "TEST-0001" && id != "TEST" {
		return nil, fmt.Errorf("movie not found: %s", id)
	}

	now := time.Now()
	return &models.ScraperResult{
		Source:           m.Name(),
		SourceURL:        "http://example.com/TEST-001",
		Language:         "en",
		ID:               "TEST-001",
		ContentID:        "test001",
		Title:            "Test Movie",
		Description:      "Test description",
		ReleaseDate:      &now,
		Runtime:          120,
		Maker:            "Test Studio",
		PosterURL:        m.posterURL,
		CoverURL:         m.posterURL,
		ShouldCropPoster: true,
		Genres:           []string{"Drama"},
	}, nil
}

func (m *mockScraperForPosterTest) GetURL(id string) (string, error) {
	return "http://example.com/" + id, nil
}

func (m *mockScraperForPosterTest) IsEnabled() bool {
	return true
}

func (m *mockScraperForPosterTest) Close() error {
	return nil
}

func (m *mockScraperForPosterTest) Config() *config.ScraperConfig {
	return &config.ScraperConfig{Enabled: true}
}
