package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestNewProcessFileTask(t *testing.T) {
	t.Run("Creates task with all components", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			true,
			false,
			false,
			tracker,
			false,
			true,
			true,
			true,
			true,
			nil, // no custom scraper priority
		)

		assert.NotNil(t, task)
		assert.Equal(t, "process-IPX-123", task.ID())
		assert.Equal(t, TaskType("process"), task.Type())
		assert.Contains(t, task.Description(), "Processing IPX-123")
		assert.NotContains(t, task.Description(), "DRY RUN")
	})

	t.Run("Creates task with dry run", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-456",
			File: scanner.FileInfo{
				Name: "ipx-456.mp4",
				Path: "/source/ipx-456.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			false,
			false,
			false,
			tracker,
			true, // dry run
			true,
			false,
			false,
			false,
			nil, // no custom scraper priority
		)

		assert.NotNil(t, task)
		assert.Contains(t, task.Description(), "[DRY RUN]")
	})

	t.Run("Creates task with selective operations", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-789",
			File: scanner.FileInfo{
				Name: "ipx-789.mp4",
				Path: "/source/ipx-789.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		// Only enable scrape and organize, disable download and NFO
		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			true,
			false,
			false,
			tracker,
			false,
			true,  // scrapeEnabled
			false, // downloadEnabled
			true,  // organizeEnabled
			false, // nfoEnabled
			nil,   // no custom scraper priority
		)

		assert.NotNil(t, task)
		// Task creation should succeed with selective operations
	})

	t.Run("Applies link and update merge options", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-999",
			File: scanner.FileInfo{
				Name: "ipx-999.mp4",
				Path: "/source/ipx-999.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})
		cfg := &config.Config{}

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			false,
			false,
			false,
			tracker,
			false,
			true,
			true,
			true,
			true,
			nil,
			WithLinkMode(organizer.LinkModeHard),
			WithUpdateMerge(true, "prefer-scraper", "replace", cfg),
		)

		assert.NotNil(t, task)
		assert.Equal(t, organizer.LinkModeHard, task.linkMode)
		assert.True(t, task.updateMode)
		assert.Equal(t, "prefer-scraper", task.scalarStrategy)
		assert.Equal(t, "replace", task.arrayStrategy)
		assert.Equal(t, cfg, task.cfg)
	})
}

func TestProcessFileTask_Execute_DryRun(t *testing.T) {
	// Skip this integration test - ProcessFileTask.Execute requires full database setup
	t.Skip("Skipping ProcessFileTask.Execute integration test - requires database setup")

	if testing.Short() {
		t.Skip("Skipping integration-style test in short mode")
	}

	t.Run("Dry run without scraping skips all operations", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			true,
			false,
			false,
			tracker,
			true,
			false, // scrapeEnabled=false means no metadata
			false,
			false,
			false,
			nil, // no custom scraper priority
		)

		ctx := context.Background()
		err := task.Execute(ctx)

		// Should complete without error (skips due to no metadata)
		assert.NoError(t, err)

		// Check progress updates
		var updates []ProgressUpdate
		close(progressChan)
		for update := range progressChan {
			updates = append(updates, update)
		}

		// Should have at least a starting update
		assert.NotEmpty(t, updates)

		// Should indicate skipped
		found := false
		for _, update := range updates {
			if update.Message == "Skipped (no metadata)" {
				found = true
				assert.Equal(t, 1.0, update.Progress)
			}
		}
		assert.True(t, found, "Expected 'Skipped' message when no metadata")
	})
}

func TestProcessFileTask_Execute_ContextCancellation(t *testing.T) {
	// Skip this integration test - ProcessFileTask.Execute requires full database setup
	t.Skip("Skipping ProcessFileTask.Execute integration test - requires database setup")

	t.Run("Respects context cancellation", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			true,
			false,
			false,
			tracker,
			false,
			true,
			false,
			false,
			false,
			nil, // no custom scraper priority
		)

		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := task.Execute(ctx)

		// Should handle cancellation (may or may not return error depending on when checked)
		if err != nil {
			t.Logf("Task returned error with canceled context: %v", err)
		}
	})
}

func TestProcessFileTask_Execute_ProgressTracking(t *testing.T) {
	// Skip this integration test - ProcessFileTask.Execute requires full database setup
	t.Skip("Skipping ProcessFileTask.Execute integration test - requires database setup")

	if testing.Short() {
		t.Skip("Skipping integration-style test in short mode")
	}

	t.Run("Updates progress throughout execution", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-999",
			File: scanner.FileInfo{
				Name: "ipx-999.mp4",
				Path: "/source/ipx-999.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			false,
			false,
			false,
			tracker,
			true, // dry run to avoid actual operations
			false,
			false,
			false,
			false,
			nil, // no custom scraper priority
		)

		// Start consuming progress updates in background
		progressUpdates := []ProgressUpdate{}
		done := make(chan bool)
		go func() {
			for update := range progressChan {
				progressUpdates = append(progressUpdates, update)
			}
			done <- true
		}()

		ctx := context.Background()
		err := task.Execute(ctx)
		assert.NoError(t, err)

		// Close channel and wait for consumer
		close(progressChan)
		<-done

		// Should have progress updates
		assert.NotEmpty(t, progressUpdates, "Expected progress updates")

		// Should have starting update
		foundStart := false
		for _, update := range progressUpdates {
			if update.Progress == 0.0 && (update.Message == "Starting..." || update.Message == "[DRY RUN] Starting...") {
				foundStart = true
			}
		}
		assert.True(t, foundStart, "Expected starting progress update")
	})
}

func TestProcessFileTask_Execute_NoScraperEnabled(t *testing.T) {
	// Skip this integration test - ProcessFileTask.Execute requires full database setup
	t.Skip("Skipping ProcessFileTask.Execute integration test - requires database setup")

	t.Run("Skips processing when scraping disabled", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-000",
			File: scanner.FileInfo{
				Name: "ipx-000.mp4",
				Path: "/source/ipx-000.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			false,
			false,
			false,
			tracker,
			false,
			false, // scrapeEnabled=false
			false,
			false,
			false,
			nil, // no custom scraper priority
		)

		ctx := context.Background()
		err := task.Execute(ctx)

		// Should skip without error
		assert.NoError(t, err)

		// Collect progress updates
		updates := []ProgressUpdate{}
		close(progressChan)
		for update := range progressChan {
			updates = append(updates, update)
		}

		// Should have completion update indicating skip
		found := false
		for _, update := range updates {
			if update.Message == "Skipped (no metadata)" && update.Progress == 1.0 {
				found = true
			}
		}
		assert.True(t, found, "Expected skip message when scraping disabled")
	})
}

func TestProcessFileTask_Interface(t *testing.T) {
	t.Run("Implements Task interface", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			true,
			false,
			false,
			tracker,
			false,
			true,
			true,
			true,
			true,
			nil, // no custom scraper priority
		)

		// Verify it implements Task interface
		var _ Task = task

		// Verify BaseTask methods work
		assert.Equal(t, "process-IPX-123", task.ID())
		assert.Equal(t, TaskType("process"), task.Type())
		assert.NotEmpty(t, task.Description())
	})
}

func TestProcessFileTask_ConcurrentExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	t.Run("Multiple process tasks can run concurrently", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 1000)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(3, 10*time.Second, tracker)
		defer pool.Stop()

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		// Submit multiple process tasks
		numTasks := 5
		for i := 0; i < numTasks; i++ {
			match := matcher.MatchResult{
				ID: string(rune('A' + i)),
				File: scanner.FileInfo{
					Name: string(rune('a'+i)) + ".mp4",
					Path: "/source/" + string(rune('a'+i)) + ".mp4",
				},
			}

			task := NewProcessFileTask(
				match,
				registry,
				agg,
				movieRepo,
				dl,
				org,
				nfoGen,
				"/dest",
				false,
				false,
				false,
				tracker,
				true, // dry run
				false,
				false,
				false,
				false,
				nil, // no custom scraper priority
			)

			pool.Submit(task)
		}

		err := pool.Wait()
		// All tasks should complete (they skip due to no scraping enabled)
		assert.NoError(t, err)

		stats := pool.Stats()
		assert.Equal(t, numTasks, stats.Success)
	})
}

func TestProcessFileTask_ForceRefreshFlag(t *testing.T) {
	t.Run("Creates task with forceRefresh flag", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-REFRESH",
			File: scanner.FileInfo{
				Name: "ipx-refresh.mp4",
				Path: "/source/ipx-refresh.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		// Create with forceRefresh=true
		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			true,
			false,
			true, // forceRefresh
			tracker,
			false,
			true,
			true,
			true,
			true,
			nil, // no custom scraper priority
		)

		assert.NotNil(t, task)
		// ForceRefresh flag is used internally in Execute, just verify task creation succeeds
	})
}

func TestProcessFileTask_AllOperationsDisabled(t *testing.T) {
	// Skip this integration test - ProcessFileTask.Execute requires full database setup
	t.Skip("Skipping ProcessFileTask.Execute integration test - requires database setup")

	t.Run("Task with all operations disabled", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-NONE",
			File: scanner.FileInfo{
				Name: "ipx-none.mp4",
				Path: "/source/ipx-none.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		// All operations disabled
		task := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			false,
			false,
			false,
			tracker,
			false,
			false, // no scrape
			false, // no download
			false, // no organize
			false, // no nfo
			nil,   // no custom scraper priority
		)

		ctx := context.Background()
		err := task.Execute(ctx)

		// Should complete without error (skips everything)
		assert.NoError(t, err)

		// Should indicate skipped
		updates := []ProgressUpdate{}
		close(progressChan)
		for update := range progressChan {
			updates = append(updates, update)
		}

		found := false
		for _, update := range updates {
			if update.Message == "Skipped (no metadata)" {
				found = true
			}
		}
		assert.True(t, found)
	})
}

func TestProcessFileTask_MoveVsCopyFlag(t *testing.T) {
	t.Run("Task respects moveFiles flag", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		match := matcher.MatchResult{
			ID: "IPX-MOVE",
			File: scanner.FileInfo{
				Name: "ipx-move.mp4",
				Path: "/source/ipx-move.mp4",
			},
		}

		registry := models.NewScraperRegistry()
		agg := &aggregator.Aggregator{}
		movieRepo := &database.MovieRepository{}
		dl := &downloader.Downloader{}
		org := organizer.NewOrganizer(afero.NewMemMapFs(), &config.OutputConfig{})
		nfoGen := nfo.NewGenerator(afero.NewMemMapFs(), &nfo.Config{})

		// Test with moveFiles=true
		taskMove := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			true, // moveFiles
			false,
			false,
			tracker,
			false,
			false,
			false,
			false,
			false,
			nil, // no custom scraper priority
		)
		assert.NotNil(t, taskMove)

		// Test with moveFiles=false
		taskCopy := NewProcessFileTask(
			match,
			registry,
			agg,
			movieRepo,
			dl,
			org,
			nfoGen,
			"/dest",
			false, // moveFiles=false (copy)
			false,
			false,
			tracker,
			false,
			false,
			false,
			false,
			false,
			nil, // no custom scraper priority
		)
		assert.NotNil(t, taskCopy)
	})
}
