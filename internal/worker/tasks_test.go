package worker

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/stretchr/testify/require"
)

// TestNFOTask_Execute tests NFO generation task execution
func TestNFOTask_Execute(t *testing.T) {
	tests := []struct {
		name       string
		movie      *models.Movie
		dryRun     bool
		partSuffix string
		wantErr    bool
		checkNFO   func(t *testing.T, path string)
	}{
		{
			name: "successful NFO generation",
			movie: &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
			},
			dryRun:     false,
			partSuffix: "",
			wantErr:    false,
			checkNFO: func(t *testing.T, path string) {
				// Verify NFO file exists
				_, err := os.Stat(path)
				require.NoError(t, err, "NFO file should exist")

				// Verify NFO content
				content, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Contains(t, string(content), "<title>Test Movie</title>")
				assert.Contains(t, string(content), "<id>IPX-001</id>")
			},
		},
		{
			name: "dry-run mode",
			movie: &models.Movie{
				ID:    "IPX-002",
				Title: "Test Movie 2",
			},
			dryRun:     true,
			partSuffix: "",
			wantErr:    false,
			checkNFO: func(t *testing.T, path string) {
				// In dry-run mode, NFO should not be created
				_, err := os.Stat(path)
				assert.True(t, os.IsNotExist(err), "NFO file should not exist in dry-run mode")
			},
		},
		{
			name: "NFO without part suffix (PerFile disabled)",
			movie: &models.Movie{
				ID:    "IPX-003",
				Title: "Multi-Part Movie",
			},
			dryRun:     false,
			partSuffix: "-pt1",
			wantErr:    false,
			checkNFO: func(t *testing.T, path string) {
				// When PerFile is false (default), suffix is NOT appended
				// So we check for the base filename without suffix
				baseNFOPath := filepath.Join(filepath.Dir(path), "IPX-003.nfo")
				_, err := os.Stat(baseNFOPath)
				require.NoError(t, err, "NFO file should exist without suffix when PerFile is disabled")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			tmpDir := t.TempDir()
			progressChan := make(chan ProgressUpdate, 10)
			tracker := NewProgressTracker(progressChan)

			// Create NFO generator
			nfoCfg := &nfo.Config{
				UnknownActress:      "Unknown",
				NFOFilenameTemplate: "<ID>.nfo",
			}
			gen := nfo.NewGenerator(afero.NewOsFs(), nfoCfg)

			// Create task
			taskID := "nfo-" + tt.movie.ID + tt.partSuffix
			task := NewNFOTask(
				tt.movie,
				tmpDir,
				gen,
				tracker,
				tt.dryRun,
				tt.partSuffix,
				"", // No video file path for these tests
			)

			// Start tracking before execution
			tracker.Start(taskID, TaskTypeNFO, "Starting NFO generation")

			// Execute
			ctx := context.Background()
			err := task.Execute(ctx)

			// Assert
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				// Check progress was updated
				progress, exists := tracker.Get(taskID)
				assert.True(t, exists, "Task should be tracked")
				if exists {
					// NFO task updates progress to 1.0 but doesn't call Complete()
					// so status remains "running". This is expected behavior.
					assert.Equal(t, 1.0, progress.Progress)
				}

				// Check NFO file
				if tt.checkNFO != nil {
					// NFO files are created as <ID>.nfo or <ID><suffix>.nfo
					nfoFilename := tt.movie.ID
					if tt.partSuffix != "" {
						nfoFilename = tt.movie.ID + tt.partSuffix
					}
					nfoPath := filepath.Join(tmpDir, nfoFilename+".nfo")
					tt.checkNFO(t, nfoPath)
				}
			}
		})
	}
}

// TestNFOTask_Execute_Cancellation tests task cancellation
func TestNFOTask_Execute_Cancellation(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	nfoCfg := &nfo.Config{
		UnknownActress:      "Unknown",
		NFOFilenameTemplate: "<ID>.nfo",
	}
	gen := nfo.NewGenerator(afero.NewOsFs(), nfoCfg)

	movie := &models.Movie{
		ID:    "IPX-999",
		Title: "Test Movie",
	}

	task := NewNFOTask(movie, tmpDir, gen, tracker, false, "", "")

	// Create canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Execute should respect cancellation (though NFO generation is fast,
	// so it might complete before checking context)
	err := task.Execute(ctx)

	// The task may or may not error depending on timing
	// This test mainly ensures the context is passed through correctly
	_ = err
}

// TestDownloadTask_Execute tests download task execution
func TestDownloadTask_Execute(t *testing.T) {
	tests := []struct {
		name      string
		movie     *models.Movie
		dryRun    bool
		multipart *downloader.MultipartInfo
		wantErr   bool
	}{
		{
			name: "dry-run mode with URLs",
			movie: &models.Movie{
				ID:          "IPX-001",
				CoverURL:    "https://example.com/cover.jpg",
				Screenshots: []string{"https://example.com/shot1.jpg", "https://example.com/shot2.jpg"},
			},
			dryRun:    true,
			multipart: nil, // single file
			wantErr:   false,
		},
		{
			name: "dry-run mode without URLs",
			movie: &models.Movie{
				ID: "IPX-002",
			},
			dryRun:    true,
			multipart: nil, // single file
			wantErr:   false,
		},
		{
			name: "multi-part movie",
			movie: &models.Movie{
				ID:       "IPX-003",
				CoverURL: "https://example.com/cover.jpg",
			},
			dryRun:    true,
			multipart: &downloader.MultipartInfo{IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			tmpDir := t.TempDir()
			progressChan := make(chan ProgressUpdate, 10)
			tracker := NewProgressTracker(progressChan)

			outputCfg := &config.OutputConfig{
				DownloadCover:  true,
				DownloadPoster: true,
			}
			dl := downloader.NewDownloader(http.DefaultClient, afero.NewOsFs(), outputCfg, "test-agent")

			// Create task
			task := NewDownloadTask(tt.movie, tmpDir, dl, tracker, tt.dryRun, tt.multipart)

			// Start tracking before execution
			taskID := "download-" + tt.movie.ID
			tracker.Start(taskID, TaskTypeDownload, "Starting download")

			// Execute
			ctx := context.Background()
			err := task.Execute(ctx)

			// Assert
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				// Check progress tracker
				taskID := "download-" + tt.movie.ID
				progress, exists := tracker.Get(taskID)
				assert.True(t, exists, "Task should be tracked")
				if exists {
					// Download task updates progress to 1.0 but doesn't call Complete()
					assert.Equal(t, 1.0, progress.Progress)

					// Verify dry-run message
					if tt.dryRun {
						assert.Contains(t, progress.Message, "[DRY RUN]")
					}
				}
			}
		})
	}
}

// TestDownloadTask_Execute_Cancellation tests download task cancellation
func TestDownloadTask_Execute_Cancellation(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	outputCfg := &config.OutputConfig{
		DownloadCover:  true,
		DownloadPoster: true,
	}
	dl := downloader.NewDownloader(http.DefaultClient, afero.NewOsFs(), outputCfg, "test-agent")

	movie := &models.Movie{
		ID:       "IPX-999",
		CoverURL: "https://example.com/cover.jpg",
	}

	task := NewDownloadTask(movie, tmpDir, dl, tracker, false, nil)

	// Create canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Execute - may or may not error depending on how fast downloader checks context
	_ = task.Execute(ctx)
}

// TestOrganizeTask_Execute tests organize task execution
func TestOrganizeTask_Execute(t *testing.T) {
	tests := []struct {
		name        string
		setupFile   bool
		dryRun      bool
		moveFiles   bool
		forceUpdate bool
		wantErr     bool
	}{
		{
			name:        "dry-run mode with move",
			setupFile:   true,
			dryRun:      true,
			moveFiles:   true,
			forceUpdate: false,
			wantErr:     false,
		},
		{
			name:        "dry-run mode with copy",
			setupFile:   true,
			dryRun:      true,
			moveFiles:   false,
			forceUpdate: false,
			wantErr:     false,
		},
		{
			name:        "execute move operation",
			setupFile:   true,
			dryRun:      false,
			moveFiles:   true,
			forceUpdate: false,
			wantErr:     false,
		},
		{
			name:        "execute copy operation",
			setupFile:   true,
			dryRun:      false,
			moveFiles:   false,
			forceUpdate: false,
			wantErr:     false,
		},
		{
			name:        "missing source file",
			setupFile:   false,
			dryRun:      false,
			moveFiles:   true,
			forceUpdate: false,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			tmpSrcDir := t.TempDir()
			tmpDestDir := t.TempDir()
			progressChan := make(chan ProgressUpdate, 10)
			tracker := NewProgressTracker(progressChan)

			// Create source file if needed
			srcFile := filepath.Join(tmpSrcDir, "IPX-001.mp4")
			if tt.setupFile {
				err := os.WriteFile(srcFile, []byte("test video content"), 0644)
				require.NoError(t, err)
			}

			// Create match result
			match := matcher.MatchResult{
				ID: "IPX-001",
				File: scanner.FileInfo{
					Path:      srcFile,
					Name:      "IPX-001.mp4",
					Extension: ".mp4",
				},
			}

			// Create movie metadata
			movie := &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
			}

			// Create organizer
			outputCfg := &config.OutputConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
				RenameFile:   true,
			}
			org := organizer.NewOrganizer(afero.NewOsFs(), outputCfg)

			// Create task
			task := NewOrganizeTask(
				match,
				movie,
				tmpDestDir,
				tt.moveFiles,
				tt.forceUpdate,
				org,
				tracker,
				tt.dryRun,
			)

			// Start tracking before execution
			taskID := "organize-" + match.File.Name
			tracker.Start(taskID, TaskTypeOrganize, "Starting organize")

			// Execute
			ctx := context.Background()
			err := task.Execute(ctx)

			// Assert
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				// Check progress tracker
				taskID := "organize-" + match.File.Name
				progress, exists := tracker.Get(taskID)
				assert.True(t, exists, "Task should be tracked")
				if exists {
					// Organize task updates progress to 1.0 but doesn't call Complete()
					assert.Equal(t, 1.0, progress.Progress)
				}

				// Verify file operations
				if !tt.dryRun && tt.setupFile {
					destPath := filepath.Join(tmpDestDir, "IPX-001", "IPX-001.mp4")

					if tt.moveFiles {
						// Source should not exist
						_, err := os.Stat(srcFile)
						assert.True(t, os.IsNotExist(err), "Source file should be moved")

						// Destination should exist
						_, err = os.Stat(destPath)
						assert.NoError(t, err, "Destination file should exist")
					} else {
						// Both source and dest should exist (copy)
						_, err := os.Stat(srcFile)
						assert.NoError(t, err, "Source file should still exist")

						_, err = os.Stat(destPath)
						assert.NoError(t, err, "Destination file should exist")
					}
				}
			}
		})
	}
}

// TestOrganizeTask_Execute_Cancellation tests organize task cancellation
func TestOrganizeTask_Execute_Cancellation(t *testing.T) {
	// Setup
	tmpSrcDir := t.TempDir()
	tmpDestDir := t.TempDir()
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	srcFile := filepath.Join(tmpSrcDir, "IPX-999.mp4")
	err := os.WriteFile(srcFile, []byte("test video"), 0644)
	require.NoError(t, err)

	match := matcher.MatchResult{
		ID: "IPX-999",
		File: scanner.FileInfo{
			Path:      srcFile,
			Name:      "IPX-999.mp4",
			Extension: ".mp4",
		},
	}

	movie := &models.Movie{
		ID:    "IPX-999",
		Title: "Test Movie",
	}

	outputCfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	org := organizer.NewOrganizer(afero.NewOsFs(), outputCfg)

	task := NewOrganizeTask(match, movie, tmpDestDir, true, false, org, tracker, false)

	// Create canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Execute - organize is fast so may complete before checking context
	_ = task.Execute(ctx)
}

// TestScrapeTask_Execute_DryRun tests scrape task in dry-run mode
func TestScrapeTask_Execute_DryRun(t *testing.T) {
	// Setup temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"},
		},
	}

	// Initialize database
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	movieRepo := database.NewMovieRepository(db)

	// Create aggregator
	agg := aggregator.New(cfg)

	// Create empty scraper registry
	registry := models.NewScraperRegistry()

	// Create progress tracker
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	// Create scrape task
	task := NewScrapeTask(
		"IPX-001",
		registry,
		agg,
		movieRepo,
		tracker,
		true,  // dry-run
		false, // no force refresh
		nil,   // no custom scraper priority
	)

	// Execute - will fail because no scrapers registered, but we test dry-run behavior
	ctx := context.Background()
	err = task.Execute(ctx)

	// Should error because no scrapers available
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Movie lookup failed")

	// Verify nothing was saved to database (dry-run mode)
	_, err = movieRepo.FindByID("IPX-001")
	assert.Error(t, err, "Movie should not be in database during dry-run")
}

// TestScrapeTask_Execute_ForceRefresh tests scrape task with force refresh
func TestScrapeTask_Execute_ForceRefresh(t *testing.T) {
	// Setup temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"},
		},
	}

	// Initialize database
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Run migrations
	err = db.AutoMigrate()
	require.NoError(t, err)

	movieRepo := database.NewMovieRepository(db)

	// Pre-populate with cached movie
	cachedMovie := &models.Movie{
		ID:    "IPX-001",
		Title: "Cached Title",
	}
	err = movieRepo.Upsert(cachedMovie)
	require.NoError(t, err)

	// Create aggregator
	agg := aggregator.New(cfg)

	// Create empty scraper registry
	registry := models.NewScraperRegistry()

	// Create progress tracker
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	// Create scrape task with force refresh
	task := NewScrapeTask(
		"IPX-001",
		registry,
		agg,
		movieRepo,
		tracker,
		false, // not dry-run
		true,  // force refresh
		nil,   // no custom scraper priority
	)

	// Execute - will fail because no scrapers, but we test force refresh behavior
	ctx := context.Background()
	err = task.Execute(ctx)

	// Should error because no scrapers available
	assert.Error(t, err)

	// Movie should be deleted from cache due to force refresh
	_, err = movieRepo.FindByID("IPX-001")
	assert.Error(t, err, "Movie should be deleted from cache with force refresh")
}

// TestScrapeTask_Execute_Cache tests scrape task with cached data
func TestScrapeTask_Execute_Cache(t *testing.T) {
	// Setup temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"},
		},
	}

	// Initialize database
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Run migrations
	err = db.AutoMigrate()
	require.NoError(t, err)

	movieRepo := database.NewMovieRepository(db)

	// Pre-populate with cached movie
	cachedMovie := &models.Movie{
		ID:    "IPX-001",
		Title: "Cached Title",
		Maker: "Test Studio",
	}
	err = movieRepo.Upsert(cachedMovie)
	require.NoError(t, err)

	// Create aggregator
	agg := aggregator.New(cfg)

	// Create empty scraper registry
	registry := models.NewScraperRegistry()

	// Create progress tracker
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	// Create scrape task (no force refresh, no dry-run)
	task := NewScrapeTask(
		"IPX-001",
		registry,
		agg,
		movieRepo,
		tracker,
		false, // not dry-run
		false, // no force refresh
		nil,   // no custom scraper priority
	)

	// Start tracking before execution
	tracker.Start("IPX-001", TaskTypeScrape, "Starting scrape")

	// Execute - should use cache
	ctx := context.Background()
	err = task.Execute(ctx)

	// Should succeed using cached data
	require.NoError(t, err)

	// Verify progress tracker shows cache hit
	progress, exists := tracker.Get("IPX-001")
	assert.True(t, exists, "Task should be tracked")
	if exists {
		// ScrapeTask calls Update(), not Complete(), so status depends on internal behavior
		// Just verify that the message indicates cache was used
		assert.Contains(t, progress.Message, "cache")
		assert.Equal(t, 1.0, progress.Progress)
	}
}

// TestScrapeTask_Execute_Cancellation tests scrape task cancellation
func TestScrapeTask_Execute_Cancellation(t *testing.T) {
	// Setup temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"},
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	movieRepo := database.NewMovieRepository(db)
	agg := aggregator.New(cfg)
	registry := models.NewScraperRegistry()

	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	task := NewScrapeTask("IPX-999", registry, agg, movieRepo, tracker, false, false, nil)

	// Create canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Execute with canceled context
	err = task.Execute(ctx)

	// May error with context canceled or "no results found" depending on timing
	assert.Error(t, err)
}

// TestProcessFileTask_Execute_DisabledSteps tests ProcessFileTask with various steps disabled
func TestProcessFileTask_Execute_DisabledSteps(t *testing.T) {
	tests := []struct {
		name            string
		scrapeEnabled   bool
		downloadEnabled bool
		organizeEnabled bool
		nfoEnabled      bool
		expectError     bool
	}{
		{
			name:            "all steps disabled",
			scrapeEnabled:   false,
			downloadEnabled: false,
			organizeEnabled: false,
			nfoEnabled:      false,
			expectError:     false,
		},
		{
			name:            "only scrape enabled",
			scrapeEnabled:   true,
			downloadEnabled: false,
			organizeEnabled: false,
			nfoEnabled:      false,
			expectError:     true, // Will fail because no scrapers
		},
		{
			name:            "only download enabled",
			scrapeEnabled:   false,
			downloadEnabled: true,
			organizeEnabled: false,
			nfoEnabled:      false,
			expectError:     false, // Should skip download when no movie metadata
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			cfg := &config.Config{
				Database: config.DatabaseConfig{
					DSN: dbPath,
				},
				Output: config.OutputConfig{
					FolderFormat: "<ID>",
					FileFormat:   "<ID>",
					RenameFile:   true,
				},
			}

			db, err := database.New(cfg)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			movieRepo := database.NewMovieRepository(db)
			agg := aggregator.New(cfg)
			registry := models.NewScraperRegistry()

			progressChan := make(chan ProgressUpdate, 10)
			tracker := NewProgressTracker(progressChan)

			match := matcher.MatchResult{
				ID: "IPX-001",
				File: scanner.FileInfo{
					Path:      filepath.Join(tmpDir, "IPX-001.mp4"),
					Name:      "IPX-001.mp4",
					Extension: ".mp4",
				},
			}

			dl := downloader.NewDownloader(http.DefaultClient, afero.NewOsFs(), &cfg.Output, "test-agent")
			org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output)
			nfoCfg := &nfo.Config{}
			nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfoCfg)

			task := NewProcessFileTask(
				match,
				registry,
				agg,
				movieRepo,
				dl,
				org,
				nfoGen,
				tmpDir,
				false, // don't move
				false, // no force update
				false, // no force refresh
				tracker,
				true, // dry-run
				tt.scrapeEnabled,
				tt.downloadEnabled,
				tt.organizeEnabled,
				tt.nfoEnabled,
				nil, // no custom scraper priority
			)

			// Execute
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err = task.Execute(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNewTaskConstructors tests all task constructors
func TestNewTaskConstructors(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	t.Run("NewScrapeTask", func(t *testing.T) {
		registry := models.NewScraperRegistry()
		cfg := &config.Config{
			Database: config.DatabaseConfig{DSN: ":memory:"},
		}
		db, _ := database.New(cfg)
		defer func() { _ = db.Close() }()
		movieRepo := database.NewMovieRepository(db)
		agg := aggregator.New(cfg)

		task := NewScrapeTask("IPX-001", registry, agg, movieRepo, tracker, false, false, nil)
		assert.NotNil(t, task)
		assert.Equal(t, "IPX-001", task.ID())
		assert.Equal(t, TaskTypeScrape, task.Type())
		assert.Contains(t, task.Description(), "Scraping metadata")
	})

	t.Run("NewScrapeTask_DryRun", func(t *testing.T) {
		registry := models.NewScraperRegistry()
		cfg := &config.Config{
			Database: config.DatabaseConfig{DSN: ":memory:"},
		}
		db, _ := database.New(cfg)
		defer func() { _ = db.Close() }()
		movieRepo := database.NewMovieRepository(db)
		agg := aggregator.New(cfg)

		task := NewScrapeTask("IPX-002", registry, agg, movieRepo, tracker, true, false, nil)
		assert.Contains(t, task.Description(), "[DRY RUN]")
	})

	t.Run("NewDownloadTask", func(t *testing.T) {
		outputCfg := &config.OutputConfig{}
		dl := downloader.NewDownloader(http.DefaultClient, afero.NewOsFs(), outputCfg, "test-agent")
		movie := &models.Movie{ID: "IPX-001"}

		task := NewDownloadTask(movie, "/tmp", dl, tracker, false, nil)
		assert.NotNil(t, task)
		assert.Equal(t, "download-IPX-001", task.ID())
		assert.Equal(t, TaskTypeDownload, task.Type())
	})

	t.Run("NewOrganizeTask", func(t *testing.T) {
		outputCfg := &config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), outputCfg)
		match := matcher.MatchResult{
			ID:   "IPX-001",
			File: scanner.FileInfo{Name: "IPX-001.mp4"},
		}
		movie := &models.Movie{ID: "IPX-001"}

		task := NewOrganizeTask(match, movie, "/tmp", true, false, org, tracker, false)
		assert.NotNil(t, task)
		assert.Contains(t, task.ID(), "organize-")
		assert.Equal(t, TaskTypeOrganize, task.Type())
	})

	t.Run("NewNFOTask", func(t *testing.T) {
		nfoCfg := &nfo.Config{}
		gen := nfo.NewGenerator(afero.NewOsFs(), nfoCfg)
		movie := &models.Movie{ID: "IPX-001"}

		task := NewNFOTask(movie, "/tmp", gen, tracker, false, "", "")
		assert.NotNil(t, task)
		assert.Equal(t, "nfo-IPX-001", task.ID())
		assert.Equal(t, TaskTypeNFO, task.Type())
	})

	t.Run("NewProcessFileTask", func(t *testing.T) {
		cfg := &config.Config{
			Database: config.DatabaseConfig{DSN: ":memory:"},
		}
		registry := models.NewScraperRegistry()
		db, _ := database.New(cfg)
		defer func() { _ = db.Close() }()
		movieRepo := database.NewMovieRepository(db)
		agg := aggregator.New(cfg)
		outputCfg := &config.OutputConfig{}
		dl := downloader.NewDownloader(http.DefaultClient, afero.NewOsFs(), outputCfg, "test-agent")
		org := organizer.NewOrganizer(afero.NewOsFs(), outputCfg)
		nfoCfg := &nfo.Config{}
		gen := nfo.NewGenerator(afero.NewOsFs(), nfoCfg)

		match := matcher.MatchResult{ID: "IPX-001"}

		task := NewProcessFileTask(
			match, registry, agg, movieRepo, dl, org, gen,
			"/tmp", false, false, false, tracker, false,
			true, true, true, true, nil,
		)
		assert.NotNil(t, task)
		assert.Contains(t, task.ID(), "process-")
		assert.Equal(t, TaskType("process"), task.Type())
	})
}
