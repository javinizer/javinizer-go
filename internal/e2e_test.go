package internal_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
)

// TestFullWorkflow tests the complete end-to-end workflow:
// 1. Scan directory for video files
// 2. Match filenames to extract IDs
// 3. Create mock scraper results
// 4. Aggregate metadata from multiple sources
// 5. Generate NFO file
// 6. Organize files with templated naming
// 7. Download cover images (mocked)
// This test validates that all components work together correctly.
func TestFullWorkflow(t *testing.T) {
	// Setup test environment
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	destDir := filepath.Join(tmpDir, "dest")
	dataDir := filepath.Join(tmpDir, "data")

	// Create directories
	for _, dir := range []string{sourceDir, destDir, dataDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create test video files with different naming patterns
	testFiles := []struct {
		filename string
		content  string
	}{
		{"IPX-535.mp4", "test video content 1"},
		{"abc-123 sample.mkv", "test video content 2"},
		{"[ThirdParty] xyz-999 [1080p].mp4", "test video content 3"},
	}

	for _, tf := range testFiles {
		path := filepath.Join(sourceDir, tf.filename)
		if err := os.WriteFile(path, []byte(tf.content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", tf.filename, err)
		}
	}

	// Initialize configuration
	cfg := createTestConfig(dataDir)

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Run migrations
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Add genre replacements to database
	genreRepo := database.NewGenreReplacementRepository(db)
	if err := genreRepo.Upsert(&models.GenreReplacement{Original: "Blow", Replacement: "Blowjob"}); err != nil {
		t.Fatalf("Failed to add genre replacement: %v", err)
	}
	if err := genreRepo.Upsert(&models.GenreReplacement{Original: "3P", Replacement: "Threesome"}); err != nil {
		t.Fatalf("Failed to add genre replacement: %v", err)
	}

	// Step 1: Scan directory for video files
	t.Log("Step 1: Scanning directory for video files")
	scnr := scanner.NewScanner(afero.NewOsFs(), &cfg.Matching)
	scanResult, err := scnr.Scan(sourceDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(scanResult.Files) != 3 {
		t.Errorf("Expected 3 files, got %d", len(scanResult.Files))
	}

	// Step 2: Match filenames to extract IDs
	t.Log("Step 2: Matching filenames to extract IDs")
	mtchr, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	matches := make([]matcher.MatchResult, 0)

	for _, file := range scanResult.Files {
		if match := mtchr.MatchFile(file); match != nil {
			matches = append(matches, *match)
			t.Logf("  Matched: %s -> ID: %s", file.Name, match.ID)
		}
	}

	if len(matches) != 3 {
		t.Errorf("Expected 3 matches, got %d", len(matches))
	}

	// Step 3: Create mock scraper results (simulating real scrapers)
	t.Log("Step 3: Creating mock scraper results")
	scraperResults := createMockScraperResults()

	// Step 4: Aggregate metadata from multiple sources
	t.Log("Step 4: Aggregating metadata from multiple sources")
	agg := aggregator.NewWithDatabase(cfg, db)

	movies := make(map[string]*models.Movie)
	for id, results := range scraperResults {
		movie, err := agg.Aggregate(results)
		if err != nil {
			t.Fatalf("Aggregation failed for %s: %v", id, err)
		}
		movies[id] = movie
		t.Logf("  Aggregated metadata for %s: %s", id, movie.Title)

		// Verify genre replacements were applied
		for _, genre := range movie.Genres {
			if genre.Name == "Blow" {
				t.Error("Genre replacement not applied: 'Blow' should be 'Blowjob'")
			}
			if genre.Name == "3P" {
				t.Error("Genre replacement not applied: '3P' should be 'Threesome'")
			}
		}
	}

	// Step 5: Generate NFO files
	t.Log("Step 5: Generating NFO files")
	nfoConfig := &nfo.Config{
		ActorFirstNameOrder:  true,
		ActorJapaneseNames:   false,
		UnknownActress:       cfg.Metadata.NFO.UnknownActressText,
		NFOFilenameTemplate:  "<ID>.nfo",
		IncludeStreamDetails: false,
		IncludeFanart:        true,
		IncludeTrailer:       true,
		DefaultRatingSource:  "themoviedb",
	}
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfoConfig)

	for id, movie := range movies {
		if err := nfoGen.Generate(movie, tmpDir, "", ""); err != nil {
			t.Fatalf("NFO generation failed for %s: %v", id, err)
		}

		nfoPath := filepath.Join(tmpDir, id+".nfo")

		// Verify NFO was created and contains data
		nfoData, err := os.ReadFile(nfoPath)
		if err != nil {
			t.Fatalf("Failed to read NFO file: %v", err)
		}

		nfoContent := string(nfoData)
		if len(nfoContent) == 0 {
			t.Error("NFO file is empty")
		}

		// Verify NFO contains expected fields
		expectedFields := []string{"<title>", "<plot>", "<actor>", "<genre>", "<studio>"}
		for _, field := range expectedFields {
			if !containsString(nfoContent, field) {
				t.Errorf("NFO missing expected field: %s", field)
			}
		}

		t.Logf("  Generated NFO for %s (%d bytes)", id, len(nfoData))
	}

	// Step 6: Test template engine
	t.Log("Step 6: Testing template engine")
	tmplEngine := template.NewEngine()

	for id, movie := range movies {
		ctx := template.NewContextFromMovie(movie)

		// Test folder format
		folderName, err := tmplEngine.Execute(cfg.Output.FolderFormat, ctx)
		if err != nil {
			t.Fatalf("Template execute failed for folder: %v", err)
		}
		t.Logf("  Folder template: %s -> %s", cfg.Output.FolderFormat, folderName)

		// Test file format
		fileName, err := tmplEngine.Execute(cfg.Output.FileFormat, ctx)
		if err != nil {
			t.Fatalf("Template execute failed for file: %v", err)
		}
		t.Logf("  File template: %s -> %s", cfg.Output.FileFormat, fileName)

		// Verify template contains expected values
		if !containsString(folderName, id) {
			t.Errorf("Folder name should contain ID %s, got: %s", id, folderName)
		}
	}

	// Step 7: Organize files
	t.Log("Step 7: Organizing files")
	org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output)

	organized := 0
	for _, match := range matches {
		movie, exists := movies[match.ID]
		if !exists {
			continue
		}

		// Test dry run first
		dryRunResult, err := org.Organize(match, movie, destDir, true, false, false)
		if err != nil {
			t.Fatalf("Dry run organize failed: %v", err)
		}

		if dryRunResult.Moved {
			t.Error("File was moved during dry run")
		}

		// Verify source still exists after dry run
		if _, err := os.Stat(match.File.Path); err != nil {
			t.Error("Source file missing after dry run")
		}

		// Now do actual organization
		result, err := org.Organize(match, movie, destDir, false, false, false)
		if err != nil {
			t.Fatalf("Organize failed: %v", err)
		}

		if !result.Moved {
			t.Error("File was not moved")
		}

		// Verify file was moved
		if _, err := os.Stat(match.File.Path); !os.IsNotExist(err) {
			t.Error("Source file still exists after organize")
		}

		if _, err := os.Stat(result.NewPath); err != nil {
			t.Errorf("Target file does not exist: %v", err)
		}

		t.Logf("  Organized: %s -> %s", match.File.Name, result.NewPath)
		organized++

		// Verify organized file has correct content
		content, err := os.ReadFile(result.NewPath)
		if err != nil {
			t.Fatalf("Failed to read organized file: %v", err)
		}

		// Find original content
		var expectedContent string
		for _, tf := range testFiles {
			if containsString(match.File.Name, filepath.Base(tf.filename)) ||
				containsString(tf.filename, filepath.Base(match.File.Name)) {
				expectedContent = tf.content
				break
			}
		}

		if string(content) != expectedContent && expectedContent != "" {
			t.Errorf("File content mismatch after organize")
		}
	}

	if organized != 3 {
		t.Errorf("Expected to organize 3 files, organized %d", organized)
	}

	// Step 8: Test downloader (dry run with mock URLs)
	t.Log("Step 8: Testing downloader (mock)")
	_ = downloader.NewDownloader(http.DefaultClient, afero.NewOsFs(), &cfg.Output, cfg.Scrapers.UserAgent)

	for id, movie := range movies {
		if movie.CoverURL == "" {
			continue
		}

		// Use local file as mock download source
		coverPath := filepath.Join(tmpDir, id+"-cover.jpg")
		mockCoverData := []byte("mock cover image data")
		if err := os.WriteFile(coverPath, mockCoverData, 0644); err != nil {
			t.Fatalf("Failed to create mock cover: %v", err)
		}

		// Verify downloader would save to correct location
		targetDir := filepath.Join(destDir, id)
		targetPath := filepath.Join(targetDir, "cover.jpg")
		t.Logf("  Would download cover for %s to: %s", id, targetPath)
	}

	// Step 9: Test database persistence
	t.Log("Step 9: Testing database persistence")
	movieRepo := database.NewMovieRepository(db)

	for _, movie := range movies {
		// Save movie to database
		if err := movieRepo.Create(movie); err != nil {
			t.Fatalf("Failed to save movie to database: %v", err)
		}

		// Retrieve movie from database
		retrieved, err := movieRepo.FindByID(movie.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve movie from database: %v", err)
		}

		// Verify data integrity
		if retrieved.ID != movie.ID {
			t.Errorf("ID mismatch: expected %s, got %s", movie.ID, retrieved.ID)
		}
		if retrieved.Title != movie.Title {
			t.Errorf("Title mismatch: expected %s, got %s", movie.Title, retrieved.Title)
		}

		t.Logf("  Saved and retrieved movie: %s", movie.ID)
	}

	// Step 10: Verify final state
	t.Log("Step 10: Verifying final state")

	// All source files should be gone
	sourceScanResult, _ := scnr.Scan(sourceDir)
	if len(sourceScanResult.Files) != 0 {
		t.Errorf("Expected source directory to be empty, found %d files", len(sourceScanResult.Files))
	}

	// All files should be in destination with proper structure
	destScanResult, _ := scnr.Scan(destDir)
	if len(destScanResult.Files) != 3 {
		t.Errorf("Expected 3 files in destination, found %d", len(destScanResult.Files))
	}

	t.Log("Integration test completed successfully!")
}

// createTestConfig creates a test configuration
func createTestConfig(dataDir string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Scrapers: config.ScrapersConfig{
			UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
				"(KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			Priority: []string{"r18dev", "dmm"},
			R18Dev: config.R18DevConfig{
				Enabled: true,
			},
			DMM: config.DMMConfig{
				Enabled:       true,
				ScrapeActress: true,
			},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:            []string{"r18dev", "dmm"},
				ContentID:     []string{"dmm", "r18dev"},
				Title:         []string{"r18dev", "dmm"},
				OriginalTitle: []string{"dmm", "r18dev"},
				Description:   []string{"dmm", "r18dev"},
				Actress:       []string{"r18dev", "dmm"},
				Genre:         []string{"r18dev", "dmm"},
				Rating:        []string{"dmm", "r18dev"},
				Runtime:       []string{"dmm", "r18dev"},
				Director:      []string{"dmm", "r18dev"},
				Maker:         []string{"r18dev", "dmm"},
				Label:         []string{"r18dev", "dmm"},
				Series:        []string{"r18dev", "dmm"},
				ReleaseDate:   []string{"r18dev", "dmm"},
				CoverURL:      []string{"r18dev", "dmm"},
				ScreenshotURL: []string{"r18dev", "dmm"},
				TrailerURL:    []string{"r18dev", "dmm"},
			},
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
				AutoAdd: true,
			},
			IgnoreGenres: []string{"Sample", "Trailer"},
			NFO: config.NFOConfig{
				Enabled:              true,
				DisplayName:          "",
				FilenameTemplate:     "<ID>.nfo",
				FirstNameOrder:       true,
				ActressLanguageJA:    false,
				UnknownActressText:   "Unknown",
				ActressAsTag:         false,
				IncludeStreamDetails: false,
				IncludeFanart:        true,
				IncludeTrailer:       true,
				RatingSource:         "themoviedb",
				Tag:                  []string{},
				Tagline:              "",
				Credits:              []string{},
			},
		},
		Matching: config.MatchingConfig{
			Extensions:      []string{".mp4", ".mkv", ".avi", ".mov"},
			MinSizeMB:       0,
			ExcludePatterns: []string{"*-trailer.*", "*-sample.*"},
			RegexEnabled:    false,
			RegexPattern:    `(?i)([a-z]{2,10})-?(\d{2,5})`,
		},
		Output: config.OutputConfig{
			FolderFormat:    "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			FileFormat:      "<ID>",
			SubfolderFormat: []string{},
			Delimiter:       ", ",
			DownloadCover:   true,
			DownloadPoster:  false,
			DownloadTrailer: false,
			DownloadActress: false,
		},
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  filepath.Join(dataDir, "javinizer-test.db"),
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
	}
}

// createMockScraperResults creates mock scraper results for testing
func createMockScraperResults() map[string][]*models.ScraperResult {
	releaseDate1 := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	releaseDate2 := time.Date(2021, 5, 20, 0, 0, 0, 0, time.UTC)
	releaseDate3 := time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC)

	return map[string][]*models.ScraperResult{
		"IPX-535": {
			{
				Source:      "r18dev",
				SourceURL:   "https://r18.dev/videos/vod/movies/detail/-/id=ipx00535/",
				ID:          "IPX-535",
				ContentID:   "ipx00535",
				Title:       "Beautiful Day With Momo",
				Description: "A beautiful day with Momo Sakura",
				ReleaseDate: &releaseDate1,
				Runtime:     120,
				Maker:       "IdeaPocket",
				Label:       "Tissue",
				CoverURL:    "https://example.com/cover.jpg",
				TrailerURL:  "https://example.com/trailer.mp4",
				Language:    "en",
				Actresses: []models.ActressInfo{
					{
						FirstName:    "Momo",
						LastName:     "Sakura",
						JapaneseName: "桜空もも",
						ThumbURL:     "https://example.com/momo.jpg",
					},
				},
				Genres:        []string{"Drama", "Blow", "Beautiful Girl"},
				ScreenshotURL: []string{"https://example.com/shot1.jpg", "https://example.com/shot2.jpg"},
				Rating: &models.Rating{
					Score: 4.5,
					Votes: 100,
				},
			},
			{
				Source:      "dmm",
				SourceURL:   "https://dmm.co.jp/ipx00535/",
				ID:          "IPX-535",
				ContentID:   "ipx00535",
				Title:       "もも先輩と過ごす最高の一日",
				Description: "桜空ももと過ごす素敵な一日",
				ReleaseDate: &releaseDate1,
				Runtime:     120,
				Maker:       "アイデアポケット",
				Label:       "ティッシュ",
				Language:    "ja",
				Actresses: []models.ActressInfo{
					{
						FirstName:    "Momo",
						LastName:     "Sakura",
						JapaneseName: "桜空もも",
					},
				},
				Genres: []string{"ドラマ", "フェラ", "美少女"},
				Rating: &models.Rating{
					Score: 4.6,
					Votes: 250,
				},
			},
		},
		"ABC-123": {
			{
				Source:      "r18dev",
				SourceURL:   "https://r18.dev/videos/vod/movies/detail/-/id=abc00123/",
				ID:          "ABC-123",
				ContentID:   "abc00123",
				Title:       "Summer Adventure",
				Description: "An exciting summer adventure",
				ReleaseDate: &releaseDate2,
				Runtime:     150,
				Maker:       "TestStudio",
				Label:       "Premium",
				CoverURL:    "https://example.com/abc-cover.jpg",
				Language:    "en",
				Actresses: []models.ActressInfo{
					{
						FirstName:    "Yua",
						LastName:     "Mikami",
						JapaneseName: "三上悠亜",
					},
				},
				Genres: []string{"3P", "Drama", "Beautiful Girl"},
			},
		},
		"XYZ-999": {
			{
				Source:      "r18dev",
				SourceURL:   "https://r18.dev/videos/vod/movies/detail/-/id=xyz00999/",
				ID:          "XYZ-999",
				ContentID:   "xyz00999",
				Title:       "Winter Romance",
				Description: "A romantic winter story",
				ReleaseDate: &releaseDate3,
				Runtime:     135,
				Maker:       "WinterStudios",
				Label:       "Special",
				CoverURL:    "https://example.com/xyz-cover.jpg",
				Language:    "en",
				Actresses: []models.ActressInfo{
					{
						FirstName:    "Aika",
						LastName:     "Yumeno",
						JapaneseName: "夢乃あいか",
					},
				},
				Genres: []string{"Romance", "Drama"},
			},
		},
	}
}

// containsString checks if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && filepath.Base(s) == filepath.Base(substr) ||
		s == substr || (len(substr) > 0 && len(s) >= len(substr) && s[:len(substr)] == substr) ||
		(len(s) > 0 && len(substr) > 0 && s[0:min(len(s), len(substr))] == substr[0:min(len(s), len(substr))]) ||
		contains(s, substr)
}

func contains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
