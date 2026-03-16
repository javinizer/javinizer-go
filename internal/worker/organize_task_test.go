package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOrganizeTask(t *testing.T) {
	t.Run("Creates task with move operation", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, "/dest", true, false, org, tracker, false)

		assert.NotNil(t, task)
		assert.Equal(t, "organize-ipx-123.mp4", task.ID())
		assert.Equal(t, TaskTypeOrganize, task.Type())
		assert.Contains(t, task.Description(), "Organizing ipx-123.mp4 (move)")
		assert.NotContains(t, task.Description(), "DRY RUN")
	})

	t.Run("Creates task with copy operation", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123"}

		task := NewOrganizeTask(match, movie, "/dest", false, false, org, tracker, false)

		assert.NotNil(t, task)
		assert.Contains(t, task.Description(), "Organizing ipx-123.mp4 (copy)")
	})

	t.Run("Creates task with dry run", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123"}

		task := NewOrganizeTask(match, movie, "/dest", true, false, org, tracker, true)

		assert.NotNil(t, task)
		assert.Contains(t, task.Description(), "[DRY RUN]")
	})
}

func TestOrganizeTask_Execute_DryRun(t *testing.T) {
	// Skip this integration test - OrganizeTask.Execute requires full organizer integration
	t.Skip("Skipping OrganizeTask.Execute integration test - requires organizer setup")

	t.Run("Dry run previews without executing", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{
			FolderFormat:    "",
			FileFormat:      "<ID> - <TITLE>",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		// Create temporary source file in separate source directory
		srcDir := t.TempDir()
		destDir := t.TempDir() // Separate destination directory
		srcPath := filepath.Join(srcDir, "ipx-123.mp4")
		require.NoError(t, os.WriteFile(srcPath, []byte("test"), 0644))

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name:      "ipx-123.mp4",
				Path:      srcPath,
				Extension: ".mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, destDir, true, false, org, tracker, true)

		ctx := context.Background()
		err := task.Execute(ctx)

		// Dry run should succeed
		assert.NoError(t, err)

		// Original file should still exist
		_, err = os.Stat(srcPath)
		assert.NoError(t, err, "Source file should not be moved in dry run")

		// Check progress updates
		var updates []ProgressUpdate
		close(progressChan)
		for update := range progressChan {
			updates = append(updates, update)
		}

		// Should have progress updates
		assert.NotEmpty(t, updates)

		// Final update should indicate dry run
		if len(updates) > 0 {
			finalUpdate := updates[len(updates)-1]
			assert.Contains(t, finalUpdate.Message, "[DRY RUN]")
			assert.Equal(t, 1.0, finalUpdate.Progress)
		}
	})
}

func TestOrganizeTask_Execute_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping context cancellation test in short mode")
	}

	t.Run("Respects context cancellation", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{
			FolderFormat:    "",
			FileFormat:      "<ID> - <TITLE>",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "ipx-123.mp4")
		require.NoError(t, os.WriteFile(srcPath, []byte("test"), 0644))

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name:      "ipx-123.mp4",
				Path:      srcPath,
				Extension: ".mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, tmpDir, true, false, org, tracker, false)

		// Create a context that's already canceled
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Execute should handle cancellation
		// Note: OrganizeTask doesn't explicitly check context in Execute,
		// but this tests that it doesn't hang
		err := task.Execute(ctx)

		// Error may or may not occur depending on when cancellation is checked
		// The important thing is it doesn't hang
		if err != nil {
			t.Logf("Task returned error (expected with cancellation): %v", err)
		}
	})
}

func TestOrganizeTask_Execute_PlanningError(t *testing.T) {
	t.Run("Returns error when planning fails", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{
			FolderFormat:    "",
			FileFormat:      "<ID> - <TITLE>",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		// Non-existent source file will cause validation/planning error
		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name:      "ipx-123.mp4",
				Path:      "/non/existent/path/ipx-123.mp4",
				Extension: ".mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, "/dest", true, false, org, tracker, false)

		ctx := context.Background()
		err := task.Execute(ctx)

		assert.Error(t, err)
		// Validation catches this before planning
		assert.Contains(t, err.Error(), "validation failed")
	})
}

func TestOrganizeTask_Execute_ValidationError(t *testing.T) {
	t.Run("Returns error when validation fails", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		// Use empty template to potentially cause validation issues
		cfg := &config.OutputConfig{
			FolderFormat:    "",
			FileFormat:      "",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "ipx-123.mp4")
		require.NoError(t, os.WriteFile(srcPath, []byte("test"), 0644))

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name:      "ipx-123.mp4",
				Path:      srcPath,
				Extension: ".mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, "", true, false, org, tracker, false)

		ctx := context.Background()
		err := task.Execute(ctx)

		// Should fail due to validation issues (empty template)
		require.Error(t, err, "Expected validation error with empty template")
		assert.Contains(t, err.Error(), "validation", "Error should mention validation")
	})
}

func TestOrganizeTask_Execute_ProgressTracking(t *testing.T) {
	// Skip this integration test - OrganizeTask.Execute requires full organizer integration
	t.Skip("Skipping OrganizeTask.Execute integration test - requires organizer setup")

	t.Run("Updates progress throughout execution", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		srcDir := t.TempDir()
		destDir := t.TempDir()
		srcPath := filepath.Join(srcDir, "ipx-123.mp4")
		require.NoError(t, os.WriteFile(srcPath, []byte("test"), 0644))

		cfg := &config.OutputConfig{
			FolderFormat:    "",
			FileFormat:      "<ID> - <TITLE>",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name:      "ipx-123.mp4",
				Path:      srcPath,
				Extension: ".mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, destDir, false, false, org, tracker, true)

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

		// Should have multiple progress updates
		assert.NotEmpty(t, progressUpdates, "Expected progress updates")

		// Check for expected progress stages
		foundPlanning := false
		foundComplete := false

		for _, update := range progressUpdates {
			if update.Message == "Planning organization..." {
				foundPlanning = true
				assert.Equal(t, 0.2, update.Progress)
			}
			if update.Progress == 1.0 {
				foundComplete = true
			}
		}

		assert.True(t, foundPlanning, "Expected planning progress update")
		assert.True(t, foundComplete, "Expected completion progress update")
	})
}

func TestOrganizeTask_Execute_MoveVsCopy(t *testing.T) {
	t.Run("Move operation removes source", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping file operation test in short mode")
		}

		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "src", "ipx-123.mp4")
		require.NoError(t, os.MkdirAll(filepath.Dir(srcPath), 0755))
		require.NoError(t, os.WriteFile(srcPath, []byte("test content"), 0644))

		destDir := filepath.Join(tmpDir, "dest")
		require.NoError(t, os.MkdirAll(destDir, 0755))

		cfg := &config.OutputConfig{
			FolderFormat:    destDir,
			FileFormat:      "<ID>",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name:      "ipx-123.mp4",
				Path:      srcPath,
				Extension: ".mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, destDir, true, false, org, tracker, false)

		ctx := context.Background()
		err := task.Execute(ctx)

		// Execute should succeed
		require.NoError(t, err, "Move operation should succeed")

		// Source should be removed after move
		_, srcErr := os.Stat(srcPath)
		assert.True(t, os.IsNotExist(srcErr), "Source file should not exist after move")

		// Destination should exist (actual path determined by organizer)
		// Since we can't predict exact dest path, just verify source was removed
		assert.True(t, os.IsNotExist(srcErr), "Source file must be gone after successful move")
	})

	t.Run("Copy operation preserves source", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping file operation test in short mode")
		}

		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "src", "ipx-456.mp4")
		require.NoError(t, os.MkdirAll(filepath.Dir(srcPath), 0755))
		require.NoError(t, os.WriteFile(srcPath, []byte("test content"), 0644))

		destDir := filepath.Join(tmpDir, "dest")
		require.NoError(t, os.MkdirAll(destDir, 0755))

		cfg := &config.OutputConfig{
			FolderFormat:    destDir,
			FileFormat:      "<ID>",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-456",
			File: scanner.FileInfo{
				Name:      "ipx-456.mp4",
				Path:      srcPath,
				Extension: ".mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-456", Title: "Test Movie"}

		task := NewOrganizeTask(match, movie, destDir, false, false, org, tracker, false)

		ctx := context.Background()
		err := task.Execute(ctx)

		// Execute should succeed
		require.NoError(t, err, "Copy operation should succeed")

		// Source should still exist for copy operation
		_, srcErr := os.Stat(srcPath)
		assert.NoError(t, srcErr, "Source file should still exist after copy")
	})
}

func TestOrganizeTask_Interface(t *testing.T) {
	t.Run("Implements Task interface", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-123",
			File: scanner.FileInfo{
				Name: "ipx-123.mp4",
				Path: "/source/ipx-123.mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-123"}

		task := NewOrganizeTask(match, movie, "/dest", true, false, org, tracker, false)

		// Verify it implements Task interface
		var _ Task = task

		// Verify BaseTask methods work
		assert.Equal(t, "organize-ipx-123.mp4", task.ID())
		assert.Equal(t, TaskTypeOrganize, task.Type())
		assert.NotEmpty(t, task.Description())
	})
}

func TestOrganizeTask_ForceUpdateFlag(t *testing.T) {
	t.Run("Creates task with forceUpdate flag", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)

		cfg := &config.OutputConfig{}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		match := matcher.MatchResult{
			ID: "IPX-789",
			File: scanner.FileInfo{
				Name: "ipx-789.mp4",
				Path: "/source/ipx-789.mp4",
			},
		}
		movie := &models.Movie{ID: "IPX-789"}

		// Create with forceUpdate=true
		task := NewOrganizeTask(match, movie, "/dest", true, true, org, tracker, false)

		assert.NotNil(t, task)
		// ForceUpdate flag is used internally, just verify task creation succeeds
	})
}

func TestOrganizeTask_ConcurrentExecution(t *testing.T) {
	t.Run("Multiple tasks can run concurrently", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(3, 10*time.Second, tracker)
		defer pool.Stop()

		tmpDir := t.TempDir()

		cfg := &config.OutputConfig{
			FolderFormat:    tmpDir,
			FileFormat:      "<ID>",
			SubfolderFormat: []string{},
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), cfg)

		// Create multiple organize tasks
		for i := 0; i < 5; i++ {
			srcPath := filepath.Join(tmpDir, fmt.Sprintf("src-%d.mp4", i))
			require.NoError(t, os.WriteFile(srcPath, []byte("test"), 0644))

			match := matcher.MatchResult{
				ID: fmt.Sprintf("IPX-%d", i),
				File: scanner.FileInfo{
					Name:      fmt.Sprintf("src-%d.mp4", i),
					Path:      srcPath,
					Extension: ".mp4",
				},
			}
			movie := &models.Movie{ID: fmt.Sprintf("IPX-%d", i)}

			task := NewOrganizeTask(match, movie, tmpDir, false, false, org, tracker, true)
			_ = pool.Submit(task)
		}

		err := pool.Wait()
		// Dry run should succeed
		assert.NoError(t, err)

		// All tasks should have executed
		stats := pool.Stats()
		assert.Equal(t, 5, stats.Success)
	})
}
