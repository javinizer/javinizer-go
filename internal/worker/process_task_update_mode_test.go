package worker

import (
	"context"
	"path/filepath"
	"strings"
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

func testProcessConfig(dbPath string) *config.Config {
	return &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"},
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
			NFO: config.NFOConfig{
				FilenameTemplate: "<ID>.nfo",
				DisplayName:      "[<ID>] <TITLE>",
				PerFile:          false,
			},
		},
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
			RenameFile:   true,
		},
	}
}

func testScraperResult(id, contentID, title string) *models.ScraperResult {
	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)
	return &models.ScraperResult{
		Source:      "dmm",
		Language:    "ja",
		ID:          id,
		ContentID:   contentID,
		Title:       title,
		Maker:       "Test Maker",
		Description: "Test Description",
		ReleaseDate: &releaseDate,
		Runtime:     120,
	}
}

func writeTestNFO(t *testing.T, path, id, title string) {
	t.Helper()
	gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{})
	err := gen.WriteNFO(&nfo.Movie{
		ID:    id,
		Title: title,
	}, path)
	require.NoError(t, err)
}

func collectProgressMessages(ch <-chan ProgressUpdate) []string {
	msgs := make([]string, 0)
	for update := range ch {
		msgs = append(msgs, update.Message)
	}
	return msgs
}

func hasMessageContaining(messages []string, substr string) bool {
	for _, msg := range messages {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}

func TestProcessFileTask_MergeWithExistingNFO(t *testing.T) {
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "ABC-123.mp4")
	newTask := func(cfg *config.Config, match matcher.MatchResult) *ProcessFileTask {
		return &ProcessFileTask{
			match:          match,
			cfg:            cfg,
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
		}
	}
	baseCfg := testProcessConfig(filepath.Join(tmpDir, "unused.db"))
	baseMatch := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path: videoPath,
			Name: "ABC-123.mp4",
		},
	}

	t.Run("no NFO returns original movie", func(t *testing.T) {
		task := newTask(baseCfg, baseMatch)
		movie := &models.Movie{
			ID:    "ABC-123",
			Title: "Scraped Title",
		}
		merged := task.mergeWithExistingNFO(movie)
		assert.Equal(t, movie, merged)
	})

	t.Run("merges NFO title and regenerates display name", func(t *testing.T) {
		task := newTask(baseCfg, baseMatch)
		movie := &models.Movie{
			ID:    "ABC-123",
			Title: "Scraped Title",
		}
		nfoPath := filepath.Join(tmpDir, "ABC-123.nfo")
		writeTestNFO(t, nfoPath, "ABC-123", "NFO Preferred Title")

		merged := task.mergeWithExistingNFO(movie)
		require.NotNil(t, merged)
		assert.Equal(t, "NFO Preferred Title", merged.Title)
		assert.Equal(t, "[ABC-123] NFO Preferred Title", merged.DisplayName)
	})

	t.Run("templated NFO title is preserved as display name", func(t *testing.T) {
		task := newTask(baseCfg, baseMatch)
		movie := &models.Movie{
			ID:    "ABC-123",
			Title: "Scraped Title",
		}
		nfoPath := filepath.Join(tmpDir, "ABC-123.nfo")
		writeTestNFO(t, nfoPath, "ABC-123", "[ABC-123] Existing Templated Title")

		merged := task.mergeWithExistingNFO(movie)
		require.NotNil(t, merged)
		assert.Equal(t, "[ABC-123] Existing Templated Title", merged.Title)
		assert.Equal(t, merged.Title, merged.DisplayName)
	})

	t.Run("legacy per-file path is used for multipart files", func(t *testing.T) {
		cfg := testProcessConfig(filepath.Join(tmpDir, "unused2.db"))
		cfg.Metadata.NFO.PerFile = true
		cfg.Metadata.NFO.FilenameTemplate = "missing-template-output.nfo"
		_ = afero.NewOsFs().Remove(filepath.Join(tmpDir, "ABC-123.nfo"))
		match := matcher.MatchResult{
			ID:          "ABC-123",
			IsMultiPart: true,
			PartNumber:  2,
			PartSuffix:  "-pt2",
			File: scanner.FileInfo{
				Path: filepath.Join(tmpDir, "ABC-123-pt2.mp4"),
				Name: "ABC-123-pt2.mp4",
			},
		}
		task := newTask(cfg, match)

		legacyVideoNFO := filepath.Join(tmpDir, "ABC-123-pt2.nfo")
		writeTestNFO(t, legacyVideoNFO, "ABC-123", "Legacy Multipart Title")

		movie := &models.Movie{
			ID:    "ABC-123",
			Title: "Scraped Title",
		}
		merged := task.mergeWithExistingNFO(movie)
		require.NotNil(t, merged)
		assert.Equal(t, "Legacy Multipart Title", merged.Title)
	})

	t.Run("invalid filename template falls back to default ID filename", func(t *testing.T) {
		cfg := testProcessConfig(filepath.Join(tmpDir, "unused3.db"))
		cfg.Metadata.NFO.FilenameTemplate = "<INVALID"
		task := newTask(cfg, baseMatch)

		fallbackNFO := filepath.Join(tmpDir, "ABC-123.nfo")
		writeTestNFO(t, fallbackNFO, "ABC-123", "Fallback Template Title")

		movie := &models.Movie{
			ID:    "ABC-123",
			Title: "Scraped Title",
		}
		merged := task.mergeWithExistingNFO(movie)
		require.NotNil(t, merged)
		assert.Equal(t, "Fallback Template Title", merged.Title)
	})

	t.Run("malformed NFO returns original movie unchanged", func(t *testing.T) {
		task := newTask(baseCfg, baseMatch)
		badPath := filepath.Join(tmpDir, "ABC-123.nfo")
		require.NoError(t, afero.WriteFile(afero.NewOsFs(), badPath, []byte("<movie><title>bad"), 0644))

		movie := &models.Movie{
			ID:    "ABC-123",
			Title: "Scraped Title",
		}
		merged := task.mergeWithExistingNFO(movie)
		assert.Equal(t, movie, merged)
	})
}

func TestProcessFileTask_Execute_UpdateModeUsesNFOInPreview(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "process-update.db")
	cfg := testProcessConfig(dbPath)

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	movieRepo := database.NewMovieRepository(db)
	agg := aggregator.New(cfg)
	registry := models.NewScraperRegistry()
	registry.Register(&MockScraper{
		name:    "dmm",
		results: testScraperResult("UPD-001", "upd001", "Scraped Title"),
	})

	videoPath := filepath.Join(tmpDir, "UPD-001.mp4")
	match := matcher.MatchResult{
		ID: "UPD-001",
		File: scanner.FileInfo{
			Path: videoPath,
			Name: "UPD-001.mp4",
		},
	}
	writeTestNFO(t, filepath.Join(tmpDir, "UPD-001.nfo"), "UPD-001", "NFO Preferred Title")

	progressChan := make(chan ProgressUpdate, 200)
	tracker := NewProgressTracker(progressChan)
	dl := downloader.NewDownloader(nil, afero.NewOsFs(), &cfg.Output, "test-agent")
	org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output)
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{
		NFOFilenameTemplate: cfg.Metadata.NFO.FilenameTemplate,
	})

	task := NewProcessFileTask(
		match,
		registry,
		agg,
		movieRepo,
		dl,
		org,
		nfoGen,
		tmpDir,
		false,
		false,
		false,
		tracker,
		true,  // dry-run
		true,  // scrape
		false, // download
		false, // organize
		true,  // nfo
		nil,
		WithUpdateMerge(true, "prefer-nfo", "merge", cfg),
	)
	tracker.Start(task.ID(), task.Type(), "start")

	err = task.Execute(context.Background())
	require.NoError(t, err)

	close(progressChan)
	messages := collectProgressMessages(progressChan)
	assert.True(t, hasMessageContaining(messages, "[DRY RUN] Completed preview"))
}

func TestProcessFileTask_Execute_NonDryRunScrapeOnlyCompletes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "process-normal.db")
	cfg := testProcessConfig(dbPath)

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	movieRepo := database.NewMovieRepository(db)
	agg := aggregator.New(cfg)
	registry := models.NewScraperRegistry()
	registry.Register(&MockScraper{
		name:    "dmm",
		results: testScraperResult("RUN-001", "run001", "Runtime Title"),
	})

	match := matcher.MatchResult{
		ID: "RUN-001",
		File: scanner.FileInfo{
			Path: filepath.Join(tmpDir, "RUN-001.mp4"),
			Name: "RUN-001.mp4",
		},
	}

	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)
	dl := downloader.NewDownloader(nil, afero.NewOsFs(), &cfg.Output, "test-agent")
	org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output)
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{
		NFOFilenameTemplate: cfg.Metadata.NFO.FilenameTemplate,
	})

	task := NewProcessFileTask(
		match,
		registry,
		agg,
		movieRepo,
		dl,
		org,
		nfoGen,
		tmpDir,
		false,
		false,
		false,
		tracker,
		false, // non-dry-run
		true,  // scrape
		false, // download
		false, // organize
		false, // nfo
		nil,
	)
	tracker.Start(task.ID(), task.Type(), "start")

	err = task.Execute(context.Background())
	require.NoError(t, err)

	cachedMovie, err := movieRepo.FindByID("RUN-001")
	require.NoError(t, err)
	assert.Equal(t, "Runtime Title", cachedMovie.Title)

	close(progressChan)
	messages := collectProgressMessages(progressChan)
	assert.True(t, hasMessageContaining(messages, "Completed"))
}

func TestProcessFileTask_Execute_DryRunAllStepsEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "process-dryrun-all.db")
	cfg := testProcessConfig(dbPath)

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	movieRepo := database.NewMovieRepository(db)
	agg := aggregator.New(cfg)
	registry := models.NewScraperRegistry()
	registry.Register(&MockScraper{
		name:    "dmm",
		results: testScraperResult("DRY-001", "dry001", "Dry Run Title"),
	})

	videoPath := filepath.Join(tmpDir, "DRY-001.mp4")
	require.NoError(t, afero.WriteFile(afero.NewOsFs(), videoPath, []byte("video"), 0644))
	match := matcher.MatchResult{
		ID: "DRY-001",
		File: scanner.FileInfo{
			Path: videoPath,
			Name: "DRY-001.mp4",
		},
	}

	progressChan := make(chan ProgressUpdate, 200)
	tracker := NewProgressTracker(progressChan)
	dl := downloader.NewDownloader(nil, afero.NewOsFs(), &cfg.Output, "test-agent")
	org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output)
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{
		NFOFilenameTemplate: cfg.Metadata.NFO.FilenameTemplate,
	})

	task := NewProcessFileTask(
		match,
		registry,
		agg,
		movieRepo,
		dl,
		org,
		nfoGen,
		tmpDir,
		false,
		false,
		false,
		tracker,
		true, // dry-run
		true, // scrape
		true, // download
		true, // organize
		true, // nfo
		nil,
	)
	tracker.Start(task.ID(), task.Type(), "start")

	err = task.Execute(context.Background())
	require.NoError(t, err)

	close(progressChan)
	messages := collectProgressMessages(progressChan)
	assert.True(t, hasMessageContaining(messages, "[DRY RUN] Completed preview"))
}

func TestProcessFileTask_Execute_OrganizeFailureReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "process-organize-fail.db")
	cfg := testProcessConfig(dbPath)

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	movieRepo := database.NewMovieRepository(db)
	agg := aggregator.New(cfg)
	registry := models.NewScraperRegistry()
	registry.Register(&MockScraper{
		name:    "dmm",
		results: testScraperResult("ORG-001", "org001", "Organize Fail Title"),
	})

	// Intentionally point to a missing source file so organize validation fails.
	match := matcher.MatchResult{
		ID: "ORG-001",
		File: scanner.FileInfo{
			Path:      filepath.Join(tmpDir, "missing-source.mp4"),
			Name:      "missing-source.mp4",
			Extension: ".mp4",
		},
	}

	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)
	dl := downloader.NewDownloader(nil, afero.NewOsFs(), &cfg.Output, "test-agent")
	org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output)
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{
		NFOFilenameTemplate: cfg.Metadata.NFO.FilenameTemplate,
	})

	task := NewProcessFileTask(
		match,
		registry,
		agg,
		movieRepo,
		dl,
		org,
		nfoGen,
		tmpDir,
		false,
		false,
		false,
		tracker,
		false, // non-dry-run
		true,  // scrape
		false, // download
		true,  // organize
		false, // nfo
		nil,
	)
	tracker.Start(task.ID(), task.Type(), "start")

	err = task.Execute(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "organize failed")
}
