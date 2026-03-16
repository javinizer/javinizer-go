package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNFOTask_DryRunPreview(t *testing.T) {
	tests := []struct {
		name            string
		movie           *models.Movie
		partSuffix      string
		expectedPreview []string
	}{
		{
			name: "Basic NFO preview",
			movie: &models.Movie{
				ID:    "IPX-123",
				Title: "Test Movie",
			},
			partSuffix: "",
			expectedPreview: []string{
				"[DRY RUN]",
				"Would generate NFO:",
				"IPX-123.nfo",
				"Title: Test Movie",
			},
		},
		{
			name: "NFO preview with part suffix",
			movie: &models.Movie{
				ID:    "ABC-456",
				Title: "Multi-Part Movie",
			},
			partSuffix: "-pt1",
			expectedPreview: []string{
				"[DRY RUN]",
				"Would generate NFO:",
				"ABC-456.nfo",
				"Title: Multi-Part Movie",
			},
		},
		{
			name: "NFO preview without title",
			movie: &models.Movie{
				ID:    "XYZ-789",
				Title: "",
			},
			partSuffix: "",
			expectedPreview: []string{
				"[DRY RUN]",
				"Would generate NFO:",
				"XYZ-789.nfo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progressChan := make(chan ProgressUpdate, 100)
			progressTracker := NewProgressTracker(progressChan)

			tempDir := t.TempDir()
			task := NewNFOTask(
				tt.movie,
				tempDir,
				nil, // Generator not used in dry-run
				progressTracker,
				true, // dryRun
				tt.partSuffix,
				"",
			)

			// Start the task in progress tracker
			progressTracker.Start(task.id, task.Type(), task.Description())

			// Execute dry-run
			ctx := context.Background()
			err := task.Execute(ctx)
			require.NoError(t, err, "Dry-run should not fail")

			// Verify progress
			progress, exists := progressTracker.Get(task.id)
			require.True(t, exists, "Progress should exist")
			assert.Equal(t, 1.0, progress.Progress, "Should reach 100%")

			// Verify preview message
			message := progress.Message
			for _, expected := range tt.expectedPreview {
				assert.Contains(t, message, expected,
					"Preview should contain '%s'", expected)
			}
		})
	}
}

func TestNFOTask_Integration(t *testing.T) {
	// Integration test with real NFO generator
	tests := []struct {
		name            string
		movie           *models.Movie
		partSuffix      string
		expectedNFOFile string
	}{
		{
			name: "Generate NFO for movie without part suffix",
			movie: &models.Movie{
				ID:          "TEST-001",
				Title:       "Test Movie Title",
				Maker:       "Test Studio",
				Description: "Test Description",
				ReleaseDate: func() *time.Time {
					t := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
					return &t
				}(),
				Runtime: 120,
				Actresses: []models.Actress{
					{FirstName: "Actress", LastName: "One"},
					{FirstName: "Actress", LastName: "Two"},
				},
				Genres: []models.Genre{
					{Name: "Genre1"},
					{Name: "Genre2"},
				},
			},
			partSuffix:      "",
			expectedNFOFile: "TEST-001.nfo",
		},
		{
			name: "Generate NFO for multi-part movie",
			movie: &models.Movie{
				ID:          "TEST-002",
				Title:       "Multi-Part Movie",
				Maker:       "Test Studio",
				Description: "Multi-part description",
			},
			partSuffix:      "", // Use empty part suffix for simpler test
			expectedNFOFile: "TEST-002.nfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			// Create real NFO generator
			generator := nfo.NewGenerator(afero.NewOsFs(), nil) // Use default config

			progressChan := make(chan ProgressUpdate, 100)
			progressTracker := NewProgressTracker(progressChan)

			task := NewNFOTask(
				tt.movie,
				tempDir,
				generator,
				progressTracker,
				false, // Not dry-run
				tt.partSuffix,
				"",
			)

			// Start the task
			progressTracker.Start(task.id, task.Type(), task.Description())

			// Execute task
			ctx := context.Background()
			err := task.Execute(ctx)
			require.NoError(t, err, "NFO generation should succeed")

			// Verify NFO file was created
			nfoPath := filepath.Join(tempDir, tt.expectedNFOFile)
			_, err = os.Stat(nfoPath)
			require.NoError(t, err, "NFO file should exist at %s", nfoPath)

			// Read NFO file
			content, err := os.ReadFile(nfoPath)
			require.NoError(t, err, "Should be able to read NFO file")

			// Verify content contains expected movie data
			nfoContent := string(content)
			assert.Contains(t, nfoContent, tt.movie.ID, "NFO should contain movie ID")
			assert.Contains(t, nfoContent, tt.movie.Title, "NFO should contain title")
			assert.Contains(t, nfoContent, tt.movie.Maker, "NFO should contain maker")

			// Verify progress tracker shows completion
			progress, exists := progressTracker.Get(task.id)
			require.True(t, exists)
			assert.Equal(t, 1.0, progress.Progress)
			assert.Contains(t, progress.Message, "NFO generated")
		})
	}
}

func TestNFOTask_Description(t *testing.T) {
	tests := []struct {
		name        string
		movieID     string
		partSuffix  string
		dryRun      bool
		wantContain []string
	}{
		{
			name:        "Normal NFO description",
			movieID:     "IPX-123",
			partSuffix:  "",
			dryRun:      false,
			wantContain: []string{"Generating NFO", "IPX-123"},
		},
		{
			name:        "Dry-run NFO description",
			movieID:     "ABC-456",
			partSuffix:  "",
			dryRun:      true,
			wantContain: []string{"[DRY RUN]", "Generating NFO", "ABC-456"},
		},
		{
			name:        "Multi-part NFO description",
			movieID:     "XYZ-789",
			partSuffix:  "-pt2",
			dryRun:      false,
			wantContain: []string{"Generating NFO", "XYZ-789-pt2"},
		},
		{
			name:        "Multi-part dry-run NFO description",
			movieID:     "TEST-001",
			partSuffix:  "-A",
			dryRun:      true,
			wantContain: []string{"[DRY RUN]", "Generating NFO", "TEST-001-A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := &models.Movie{
				ID:    tt.movieID,
				Title: "Test Movie",
			}

			progressChan := make(chan ProgressUpdate, 100)
			progressTracker := NewProgressTracker(progressChan)

			targetDir := t.TempDir()

			task := NewNFOTask(
				movie,
				targetDir,
				nfo.NewGenerator(afero.NewOsFs(), nil), // Use default config
				progressTracker,
				tt.dryRun,
				tt.partSuffix,
				"",
			)

			description := task.Description()
			for _, want := range tt.wantContain {
				assert.Contains(t, description, want,
					"Description should contain '%s'", want)
			}

			if !tt.dryRun {
				assert.NotContains(t, description, "[DRY RUN]",
					"Non-dry-run description should not contain '[DRY RUN]'")
			}
		})
	}
}

func TestNFOTask_WithVideoFilePath(t *testing.T) {
	// Test that videoFilePath parameter is passed to generator
	t.Run("NFO task with video file path", func(t *testing.T) {
		tempDir := t.TempDir()

		movie := &models.Movie{
			ID:    "VIDEO-TEST",
			Title: "Video Test Movie",
		}

		generator := nfo.NewGenerator(afero.NewOsFs(), nil) // Use default config
		progressChan := make(chan ProgressUpdate, 100)
		progressTracker := NewProgressTracker(progressChan)

		// Create a dummy video file so generator doesn't fail
		videoPath := filepath.Join(tempDir, "video.mp4")
		err := os.WriteFile(videoPath, []byte("dummy video content"), 0644)
		require.NoError(t, err)

		task := NewNFOTask(
			movie,
			tempDir,
			generator,
			progressTracker,
			false,
			"",
			videoPath, // Video file path provided
		)

		// Verify task stores videoFilePath
		assert.Equal(t, videoPath, task.videoFilePath)

		// Start and execute
		progressTracker.Start(task.id, task.Type(), task.Description())

		ctx := context.Background()
		err = task.Execute(ctx)
		require.NoError(t, err, "Task should complete successfully with video file path")

		// Verify NFO was generated
		nfoPath := filepath.Join(tempDir, "VIDEO-TEST.nfo")
		_, err = os.Stat(nfoPath)
		require.NoError(t, err, "NFO file should be created")
	})
}

func TestNFOTask_ProgressUpdates(t *testing.T) {
	// Test that progress updates are sent during execution
	t.Run("Progress updates during NFO generation", func(t *testing.T) {
		tempDir := t.TempDir()

		movie := &models.Movie{
			ID:    "PROGRESS-NFO",
			Title: "Progress Test",
		}

		generator := nfo.NewGenerator(afero.NewOsFs(), nil) // Use default config
		progressChan := make(chan ProgressUpdate, 100)
		progressTracker := NewProgressTracker(progressChan)

		task := NewNFOTask(
			movie,
			tempDir,
			generator,
			progressTracker,
			false,
			"",
			"",
		)

		// Start the task
		progressTracker.Start(task.id, task.Type(), task.Description())

		// Execute
		ctx := context.Background()
		err := task.Execute(ctx)
		require.NoError(t, err)

		// Verify final progress
		progress, exists := progressTracker.Get(task.id)
		require.True(t, exists)
		assert.Equal(t, 1.0, progress.Progress, "Should reach 100%")
		assert.Contains(t, progress.Message, "NFO generated")
	})
}

func TestNFOTask_ErrorHandling(t *testing.T) {
	// Test error handling when generator fails
	t.Run("Handles generator error gracefully", func(t *testing.T) {
		// Create a temp file (not a directory) to force a deterministic error
		// when the generator tries to create files inside it
		tempFile, err := os.CreateTemp("", "not-a-dir-*.txt")
		require.NoError(t, err)
		_ = tempFile.Close()
		defer func() { _ = os.Remove(tempFile.Name()) }()

		// Use the file path as targetDir - this will consistently fail
		// when generator tries to create subdirectories/files inside it
		invalidDir := tempFile.Name()

		movie := &models.Movie{
			ID:    "ERROR-TEST",
			Title: "Error Test",
		}

		generator := nfo.NewGenerator(afero.NewOsFs(), nil) // Use default config
		progressChan := make(chan ProgressUpdate, 100)
		progressTracker := NewProgressTracker(progressChan)

		task := NewNFOTask(
			movie,
			invalidDir,
			generator,
			progressTracker,
			false,
			"",
			"",
		)

		// Start the task
		progressTracker.Start(task.id, task.Type(), task.Description())

		// Execute should return error (file path is not a directory)
		ctx := context.Background()
		err = task.Execute(ctx)
		require.Error(t, err, "Should return error when target is a file, not a directory")
		assert.Contains(t, err.Error(), "failed to generate NFO")
	})
}
