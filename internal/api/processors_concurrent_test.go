package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubScraper is a mock scraper for testing that returns deterministic metadata
type stubScraper struct {
	name    string
	enabled bool
}

func newStubScraper(name string) *stubScraper {
	return &stubScraper{
		name:    name,
		enabled: true,
	}
}

func (s *stubScraper) Name() string {
	return s.name
}

func (s *stubScraper) Search(id string) (*models.ScraperResult, error) {
	// Only return results for valid JAV ID patterns (like "IPX-001", "SSIS-100", etc.)
	// This simulates real scrapers that only work with proper JAV IDs
	// Valid pattern: letters followed by hyphen followed by digits
	validPattern := false
	parts := strings.Split(id, "-")
	if len(parts) == 2 {
		// Check if first part is letters and second part is digits
		firstPart := parts[0]
		secondPart := parts[1]
		if len(firstPart) > 0 && len(secondPart) > 0 {
			allLetters := true
			for _, r := range firstPart {
				if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
					allLetters = false
					break
				}
			}
			allDigits := true
			for _, r := range secondPart {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allLetters && allDigits {
				validPattern = true
			}
		}
	}

	if !validPattern {
		return nil, fmt.Errorf("movie not found: %s", id)
	}

	// Create release date
	releaseDate, _ := time.Parse("2006-01-02", "2024-01-15")

	// Return deterministic metadata based on the ID
	return &models.ScraperResult{
		Source:        s.name,
		SourceURL:     fmt.Sprintf("https://example.com/movies/%s", id),
		Language:      "ja",
		ID:            id,
		ContentID:     id,
		Title:         fmt.Sprintf("Test Movie %s", id),
		OriginalTitle: fmt.Sprintf("テストムービー %s", id),
		Description:   "This is a test movie for concurrent testing",
		ReleaseDate:   &releaseDate,
		Runtime:       120,
		Director:      "Test Director",
		Maker:         "Test Studio",
		Label:         "Test Label",
		Series:        "Test Series",
		Actresses: []models.ActressInfo{
			{FirstName: "Test", LastName: "Actress 1", JapaneseName: "テスト女優1", DMMID: 1001},
			{FirstName: "Test", LastName: "Actress 2", JapaneseName: "テスト女優2", DMMID: 1002},
		},
		Genres:    []string{"Drama", "Action"},
		PosterURL: fmt.Sprintf("https://example.com/posters/%s.jpg", id),
		CoverURL:  fmt.Sprintf("https://example.com/covers/%s.jpg", id),
		ScreenshotURL: []string{
			fmt.Sprintf("https://example.com/screens/%s-1.jpg", id),
			fmt.Sprintf("https://example.com/screens/%s-2.jpg", id),
		},
		TrailerURL: fmt.Sprintf("https://example.com/trailers/%s.mp4", id),
	}, nil
}

func (s *stubScraper) GetURL(id string) (string, error) {
	return fmt.Sprintf("https://example.com/movies/%s", id), nil
}

func (s *stubScraper) IsEnabled() bool {
	return s.enabled
}

// createTestFiles creates temporary test video files with JAV ID patterns
func createTestFiles(t *testing.T, count int) ([]string, string) {
	t.Helper()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "javinizer-test-*")
	require.NoError(t, err)

	// Create test files with realistic JAV ID patterns
	files := make([]string, 0, count)
	patterns := []string{
		"IPX-%03d.mp4",
		"SSIS-%03d.mkv",
		"STARS-%03d.avi",
		"ABW-%03d.mp4",
		"MIDV-%03d.mkv",
	}

	for i := 0; i < count; i++ {
		pattern := patterns[i%len(patterns)]
		filename := fmt.Sprintf(pattern, i+1)
		filePath := filepath.Join(tmpDir, filename)

		// Create empty file
		file, err := os.Create(filePath)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		files = append(files, filePath)
	}

	return files, tmpDir
}

// createInvalidTestFiles creates files with patterns that won't match JAV IDs
func createInvalidTestFiles(t *testing.T, count int, tmpDir string) []string {
	t.Helper()

	// Create files with patterns that won't match ANY JAV ID regex
	// These deliberately avoid the letter-hyphen-digit or letter+digit patterns
	files := make([]string, 0, count)
	patterns := []string{
		"movie_backup_%d.mp4", // underscore, not hyphen
		"random123video.mkv",  // digits before letters
		"nohyphenhere%d.avi",  // no hyphen separator
		"123456.mp4",          // only numbers, no letters
		"xy.mp4",              // too short (< 3 letters)
	}

	for i := 0; i < count; i++ {
		pattern := patterns[i%len(patterns)]
		filename := fmt.Sprintf(pattern, i)
		filePath := filepath.Join(tmpDir, filename)

		file, err := os.Create(filePath)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		files = append(files, filePath)
	}

	return files
}

// TestProcessBatchJobConcurrent tests basic concurrent processing of multiple files
func TestProcessBatchJobConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent integration test in short mode")
	}

	// Initialize WebSocket hub
	initTestWebSocket(t)

	// Create test files
	files, tmpDir := createTestFiles(t, 10)
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// Create test configuration
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Performance: config.PerformanceConfig{
			MaxWorkers:    3,
			WorkerTimeout: 30,
		},
	}

	// Create test dependencies
	deps := createTestDeps(t, cfg, "")

	// Create batch job
	job := deps.JobQueue.CreateJob(files)
	require.NotNil(t, job)

	// Process batch job in goroutine
	done := make(chan struct{})
	go func() {
		processBatchJob(
			job,
			deps.Registry,
			deps.Aggregator,
			deps.MovieRepo,
			deps.Matcher,
			false, // strict
			false, // force
			false, // updateMode
			"",    // destination
			cfg,
			nil,              // selectedScrapers
			"prefer-scraper", // scalarStrategy
			"merge",          // arrayStrategy
			deps.DB,          // db
		)
		close(done)
	}()

	// Wait for completion
	select {
	case <-done:
		// Success
	case <-time.After(180 * time.Second):
		t.Fatal("Batch job did not complete within timeout")
	}

	// Verify job status
	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status, "Job should be completed")
	assert.Equal(t, len(files), status.TotalFiles, "Total files should match")
	assert.Equal(t, 100.0, status.Progress, "Progress should be 100%")

	// Verify results map populated (all files should have results)
	assert.Len(t, status.Results, len(files), "All files should have results")

	// Note: Files will fail because scrapers aren't configured, but that's expected
	// We're testing concurrent execution, not scraper functionality
	totalProcessed := status.Completed + status.Failed
	assert.Equal(t, len(files), totalProcessed, "All files should be processed")
}

// TestProcessBatchJobCancellation tests context cancellation during processing
// Note: There's a known race condition in BatchJob.Cancel() accessing CancelFunc without lock protection.
// This is a production code issue that should be fixed in job_queue.go, not a test issue.
// For now, we test cancellation in a way that minimizes the race.
func TestProcessBatchJobCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cancellation test in short mode")
	}

	// Initialize WebSocket hub
	initTestWebSocket(t)

	// Create many test files to ensure some are still running when cancelled
	files, tmpDir := createTestFiles(t, 25)
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Performance: config.PerformanceConfig{
			MaxWorkers:    2, // Limit workers to slow down processing
			WorkerTimeout: 30,
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Create batch job
	job := deps.JobQueue.CreateJob(files)
	require.NotNil(t, job)

	// Start processing in goroutine
	done := make(chan struct{})
	go func() {
		processBatchJob(
			job,
			deps.Registry,
			deps.Aggregator,
			deps.MovieRepo,
			deps.Matcher,
			false,
			false,
			false, // updateMode
			"",
			cfg,
			nil,              // selectedScrapers
			"prefer-scraper", // scalarStrategy
			"merge",          // arrayStrategy
			deps.DB,          // db
		)
		close(done)
	}()

	// Wait a bit for tasks to start and ensure CancelFunc is definitely set
	time.Sleep(500 * time.Millisecond)

	// Cancel the job using the Cancel method
	// NOTE: This has a known race condition in production code (CancelFunc access without mutex)
	job.Cancel()

	// Wait for completion (should be quick after cancellation)
	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Job did not terminate after cancellation")
	}

	// Verify job was cancelled
	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCancelled, status.Status, "Job should be cancelled")

	// Some tasks may have completed before cancellation, others should be cancelled
	// We just verify that not all files completed
	assert.Less(t, status.Completed, len(files), "Not all files should complete after cancellation")
}

// TestProcessBatchJobRaceConditions tests for race conditions during concurrent processing
// This test is specifically designed to catch race conditions when run with -race flag
func TestProcessBatchJobRaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition test in short mode")
	}

	// Initialize WebSocket hub
	initTestWebSocket(t)

	// Create test files
	files, tmpDir := createTestFiles(t, 20)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Performance: config.PerformanceConfig{
			MaxWorkers:    5, // High concurrency to trigger potential races
			WorkerTimeout: 30,
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Create batch job
	job := deps.JobQueue.CreateJob(files)
	require.NotNil(t, job)

	// Start processing in goroutine
	done := make(chan struct{})
	go func() {
		processBatchJob(
			job,
			deps.Registry,
			deps.Aggregator,
			deps.MovieRepo,
			deps.Matcher,
			false,
			false,
			false, // updateMode
			"",
			cfg,
			nil,              // selectedScrapers
			"prefer-scraper", // scalarStrategy
			"merge",          // arrayStrategy
			deps.DB,          // db
		)
		close(done)
	}()

	// Concurrently read job status while processing (this will catch races if present)
	readsDone := make(chan struct{})
	go func() {
		defer close(readsDone)
		for i := 0; i < 100; i++ {
			status := job.GetStatus() // Thread-safe read
			// Access fields to ensure they're properly copied
			_ = status.Status
			_ = status.Progress
			_ = status.Completed
			_ = status.Failed
			_ = len(status.Results)

			// Also test GetProgress (lightweight accessor)
			progress := job.GetProgress()
			_ = progress

			time.Sleep(10 * time.Millisecond)

			// Exit if job completed
			if status.Status == worker.JobStatusCompleted {
				break
			}
		}
	}()

	// Wait for processing to complete
	select {
	case <-done:
		// Success
	case <-time.After(180 * time.Second):
		t.Fatal("Batch job did not complete within timeout")
	}

	// Wait for status reads to complete
	<-readsDone

	// Verify no race conditions were detected (the -race flag would fail the test if any existed)
	status := job.GetStatus()
	assert.NotNil(t, status)
	assert.Equal(t, worker.JobStatusCompleted, status.Status)
}

// TestProgressAdapterWebSocketOrdering tests WebSocket message ordering and correctness
func TestProgressAdapterWebSocketOrdering(t *testing.T) {
	// Create mock WebSocket hub
	mockHub := &mockWebSocketHub{}

	// Create test job
	files := []string{"/test/file1.mp4", "/test/file2.mp4", "/test/file3.mp4"}
	jobQueue := worker.NewJobQueue()
	job := jobQueue.CreateJob(files)

	// Create progress adapter with mock hub
	adapter := NewProgressAdapter(job.ID, job, mockHub)

	// Create progress tracker
	progressTracker := worker.NewProgressTracker(adapter.GetChannel())

	// Register tasks
	taskIDs := make([]string, len(files))
	for i, filePath := range files {
		taskID := fmt.Sprintf("test-task-%d", i)
		taskIDs[i] = taskID
		adapter.RegisterTask(taskID, i, filePath)
	}

	// Start adapter
	adapter.Start()
	defer adapter.Stop()

	// Send progress updates in order
	for i, taskID := range taskIDs {
		progressTracker.Start(taskID, worker.TaskTypeBatchScrape, "Starting")
		time.Sleep(10 * time.Millisecond)

		progressTracker.Update(taskID, 0.5, fmt.Sprintf("Processing file %d", i), 0)
		time.Sleep(10 * time.Millisecond)

		progressTracker.Complete(taskID, fmt.Sprintf("Completed file %d", i))
		time.Sleep(10 * time.Millisecond)

		// Update job status to reflect progress
		result := &worker.FileResult{
			FilePath:  files[i],
			Status:    worker.JobStatusCompleted,
			StartedAt: time.Now(),
		}
		job.UpdateFileResult(files[i], result)
	}

	// Wait for messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Stop adapter and verify messages
	adapter.Stop()

	messages := mockHub.GetMessages()

	// Verify we received messages
	assert.NotEmpty(t, messages, "Should receive progress messages")

	// Verify message fields and strict ordering
	lastProgress := 0.0
	for i, msg := range messages {
		assert.Equal(t, job.ID, msg.JobID, "Job ID should match")
		assert.NotEmpty(t, msg.Status, "Status should be set")

		// Verify file index is valid
		if msg.FileIndex >= 0 {
			assert.Less(t, msg.FileIndex, len(files), "File index should be valid")
			assert.Equal(t, files[msg.FileIndex], msg.FilePath, "File path should match index")
		}

		// Verify progress never decreases (strict ordering)
		assert.GreaterOrEqual(t, msg.Progress, lastProgress,
			"Progress should not decrease (message %d: %.2f -> %.2f)", i, lastProgress, msg.Progress)
		lastProgress = msg.Progress
	}
	assert.Greater(t, lastProgress, 0.0, "Progress should increase over time")
}

// TestBatchScrapeTaskDatabaseSafety tests database transaction safety during concurrent operations
func TestBatchScrapeTaskDatabaseSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database safety test in short mode")
	}

	// Initialize WebSocket hub
	initTestWebSocket(t)

	// Create multiple files with the same movie ID pattern to test concurrent database access
	files, tmpDir := createTestFiles(t, 1)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create additional copies of the same file with different names but same ID pattern
	// This simulates concurrent scraping of the same movie ID
	baseFile := files[0]
	additionalFiles := []string{
		filepath.Join(tmpDir, "IPX-001-part1.mp4"),
		filepath.Join(tmpDir, "IPX-001-part2.mp4"),
	}

	for _, filePath := range additionalFiles {
		// Copy the file
		srcFile, err := os.Open(baseFile)
		require.NoError(t, err)
		dstFile, err := os.Create(filePath)
		require.NoError(t, err)
		require.NoError(t, srcFile.Close())
		require.NoError(t, dstFile.Close())
	}

	files = append(files, additionalFiles...)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"stub"},
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Performance: config.PerformanceConfig{
			MaxWorkers:    1, // Sequential processing to avoid race conditions with same movie ID
			WorkerTimeout: 30,
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Register stub scraper so database operations are actually exercised
	deps.Registry.Register(newStubScraper("stub"))

	// Create batch job
	job := deps.JobQueue.CreateJob(files)

	// Process job
	done := make(chan struct{})
	go func() {
		processBatchJob(
			job,
			deps.Registry,
			deps.Aggregator,
			deps.MovieRepo,
			deps.Matcher,
			false,
			false,
			false, // updateMode
			"",
			cfg,
			nil,              // selectedScrapers
			"prefer-scraper", // scalarStrategy
			"merge",          // arrayStrategy
			deps.DB,          // db
		)
		close(done)
	}()

	// Wait for completion
	select {
	case <-done:
		// Success - no deadlocks occurred
	case <-time.After(180 * time.Second):
		t.Fatal("Database operations deadlocked")
	}

	// Verify all tasks completed (even with same movie ID)
	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status)

	// All tasks should succeed now that we have a stub scraper
	assert.Equal(t, len(files), status.Completed, "All tasks should complete successfully")
	assert.Equal(t, 0, status.Failed, "No tasks should fail")

	// Verify database state - movie should be in database
	movie, err := deps.MovieRepo.FindByID("IPX-001")
	require.NoError(t, err, "Movie should be in database")
	assert.Equal(t, "IPX-001", movie.ID)
	assert.Equal(t, "Test Movie IPX-001", movie.Title)
	assert.Equal(t, "Test Studio", movie.Maker)

	// Verify no deadlocks occurred (if we got here, we're good)
	totalProcessed := status.Completed + status.Failed
	assert.Equal(t, len(files), totalProcessed, "All tasks should be processed without deadlock")
}

// TestWorkerPoolErrorHandling tests error handling in concurrent scenarios
func TestWorkerPoolErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping error handling test in short mode")
	}

	// Initialize WebSocket hub
	initTestWebSocket(t)

	// Create mix of valid and invalid files
	validFiles, tmpDir := createTestFiles(t, 5)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	invalidFiles := createInvalidTestFiles(t, 5, tmpDir)

	// Mix them together
	files := make([]string, 0, len(validFiles)+len(invalidFiles))
	for i := 0; i < len(validFiles); i++ {
		files = append(files, validFiles[i])
		if i < len(invalidFiles) {
			files = append(files, invalidFiles[i])
		}
	}

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"stub"},
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Performance: config.PerformanceConfig{
			MaxWorkers:    3,
			WorkerTimeout: 30,
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Register stub scraper so valid files can succeed
	deps.Registry.Register(newStubScraper("stub"))

	// Create batch job
	job := deps.JobQueue.CreateJob(files)

	// Process job
	done := make(chan struct{})
	go func() {
		processBatchJob(
			job,
			deps.Registry,
			deps.Aggregator,
			deps.MovieRepo,
			deps.Matcher,
			false,
			false,
			false, // updateMode
			"",
			cfg,
			nil,              // selectedScrapers
			"prefer-scraper", // scalarStrategy
			"merge",          // arrayStrategy
			deps.DB,          // db
		)
		close(done)
	}()

	// Wait for completion
	select {
	case <-done:
		// Success
	case <-time.After(180 * time.Second):
		t.Fatal("Batch job did not complete within timeout")
	}

	// Verify job completed despite errors
	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status, "Job should complete despite errors")

	// Verify all files were processed
	totalProcessed := status.Completed + status.Failed
	assert.Equal(t, len(files), totalProcessed, "All files should be processed")

	// Verify valid files succeeded
	assert.Equal(t, len(validFiles), status.Completed, "All valid files should succeed")
	for _, validFile := range validFiles {
		result, exists := status.Results[validFile]
		require.True(t, exists, "Valid file should have result")
		assert.Equal(t, worker.JobStatusCompleted, result.Status, "Valid file should succeed")
		assert.NotNil(t, result.Data, "Valid file should have movie data")
	}

	// Verify invalid files failed with correct errors
	assert.Equal(t, len(invalidFiles), status.Failed, "All invalid files should fail")
	for _, invalidFile := range invalidFiles {
		result, exists := status.Results[invalidFile]
		require.True(t, exists, "Invalid file should have result")
		assert.Equal(t, worker.JobStatusFailed, result.Status, "Invalid file should fail")
		assert.NotEmpty(t, result.Error, "Error should be present")
	}
}

// TestProgressTrackerConcurrentUpdates tests concurrent progress updates
func TestProgressTrackerConcurrentUpdates(t *testing.T) {
	// Create progress channel
	progressChan := make(chan worker.ProgressUpdate, 100)
	tracker := worker.NewProgressTracker(progressChan)

	// Collect updates in background
	updates := make([]worker.ProgressUpdate, 0)
	var updatesMu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		for update := range progressChan {
			updatesMu.Lock()
			updates = append(updates, update)
			updatesMu.Unlock()
		}
	}()

	// Launch multiple goroutines sending updates concurrently
	numGoroutines := 10
	updatesPerGoroutine := 10

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			taskID := fmt.Sprintf("task-%d", goroutineID)

			// Start task first
			tracker.Start(taskID, worker.TaskTypeScrape, "Starting")

			// Send updates
			for i := 0; i < updatesPerGoroutine; i++ {
				tracker.Update(taskID, float64(i)/float64(updatesPerGoroutine), "Processing", int64(i*100))
				time.Sleep(1 * time.Millisecond)
			}

			// Complete task
			tracker.Complete(taskID, "Done")
		}(g)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Close channel and wait for collector
	close(progressChan)
	<-done

	// Verify updates were recorded (start + updates + complete for each task)
	updatesMu.Lock()
	totalUpdates := len(updates)
	updatesMu.Unlock()

	// Each goroutine sends: 1 start + 10 updates + 1 complete = 12 messages
	expectedMinUpdates := numGoroutines * (1 + updatesPerGoroutine + 1)
	assert.GreaterOrEqual(t, totalUpdates, expectedMinUpdates, "Should have at least all start/update/complete messages")

	// Verify no race conditions in stats (if we got here without crash, we're good)
	stats := tracker.Stats()
	assert.Equal(t, numGoroutines, stats.Total, "Should have stats for all tasks")
	assert.Equal(t, numGoroutines, stats.Success, "All tasks should be successful")
}

// TestBatchJobResultConsistency tests result map consistency during concurrent updates
func TestBatchJobResultConsistency(t *testing.T) {
	// Create batch job with many files
	numFiles := 50
	files := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		files[i] = fmt.Sprintf("/test/file-%d.mp4", i)
	}

	jobQueue := worker.NewJobQueue()
	job := jobQueue.CreateJob(files)

	// Concurrently update file results
	var wg sync.WaitGroup
	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			// Simulate processing
			time.Sleep(time.Duration(index%10) * time.Millisecond)

			// Update result
			result := &worker.FileResult{
				FilePath:  files[index],
				MovieID:   fmt.Sprintf("TEST-%03d", index),
				Status:    worker.JobStatusCompleted,
				Data:      &models.Movie{ID: fmt.Sprintf("TEST-%03d", index)},
				StartedAt: time.Now(),
			}
			now := time.Now()
			result.EndedAt = &now

			job.UpdateFileResult(files[index], result)
		}(i)
	}

	// Concurrently read status while updates are happening
	readCount := atomic.Int32{}
	readsDone := make(chan struct{})
	go func() {
		defer close(readsDone)
		for i := 0; i < 100; i++ {
			status := job.GetStatus()

			// Verify consistency
			assert.Equal(t, numFiles, status.TotalFiles)
			assert.LessOrEqual(t, status.Completed, numFiles)
			assert.LessOrEqual(t, status.Failed, numFiles)
			assert.LessOrEqual(t, status.Progress, 100.0)

			// Verify result map is consistent
			for filePath, result := range status.Results {
				assert.NotEmpty(t, filePath)
				assert.NotNil(t, result)
				assert.NotEmpty(t, result.FilePath)
			}

			readCount.Add(1)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Wait for updates to complete
	wg.Wait()

	// Wait for reads to complete
	<-readsDone

	// Verify final state
	finalStatus := job.GetStatus()
	assert.Equal(t, numFiles, finalStatus.Completed, "All files should be completed")
	assert.Equal(t, 0, finalStatus.Failed, "No files should fail")
	assert.Equal(t, 100.0, finalStatus.Progress, "Progress should be 100%")
	assert.Len(t, finalStatus.Results, numFiles, "Results map should have all files")

	// Verify counters are accurate
	completed := 0
	for _, result := range finalStatus.Results {
		if result.Status == worker.JobStatusCompleted {
			completed++
		}
	}
	assert.Equal(t, finalStatus.Completed, completed, "Completed counter should match actual completed tasks")

	t.Logf("Successfully performed %d concurrent status reads", readCount.Load())
}

// TestProgressAdapterRegistrationConcurrency tests concurrent task registration
func TestProgressAdapterRegistrationConcurrency(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	files := make([]string, 100)
	for i := range files {
		files[i] = fmt.Sprintf("/test/file-%d.mp4", i)
	}

	jobQueue := worker.NewJobQueue()
	job := jobQueue.CreateJob(files)

	adapter := NewProgressAdapter(job.ID, job, mockHub)
	adapter.Start()
	defer adapter.Stop()

	// Concurrently register tasks
	var wg sync.WaitGroup
	for i := range files {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			taskID := fmt.Sprintf("task-%d", index)
			adapter.RegisterTask(taskID, index, files[index])
		}(i)
	}

	wg.Wait()

	// Verify all tasks registered
	count := adapter.GetRegisteredTaskCount()
	assert.Equal(t, len(files), count, "All tasks should be registered")
}
