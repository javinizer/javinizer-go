package downloader

import (
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func createTestMovie() *models.Movie {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	return &models.Movie{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Test Movie",
		ReleaseDate: &releaseDate,
		CoverURL:    "http://example.com/cover.jpg",
		TrailerURL:  "http://example.com/trailer.mp4",
		Screenshots: []string{
			"http://example.com/screenshot1.jpg",
			"http://example.com/screenshot2.jpg",
			"http://example.com/screenshot3.jpg",
		},
		Actresses: []models.Actress{
			{
				FirstName: "Momo",
				LastName:  "Sakura",
				ThumbURL:  "http://example.com/actress1.jpg",
			},
			{
				FirstName: "Test",
				LastName:  "Actress",
				ThumbURL:  "http://example.com/actress2.jpg",
			},
		},
	}
}

func TestDownloader_DownloadCover(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/cover.jpg"

	cfg := &config.OutputConfig{
		DownloadCover: true,
		FanartFormat:  "<ID>-fanart.jpg",
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadCover failed: %v", err)
	}

	if !result.Downloaded {
		t.Error("Expected Downloaded to be true")
	}

	if result.Type != MediaTypeCover {
		t.Errorf("Expected type %s, got %s", MediaTypeCover, result.Type)
	}

	expectedPath := filepath.Join(tmpDir, "IPX-535-fanart.jpg")
	if result.LocalPath != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, result.LocalPath)
	}

	// Verify file exists
	if _, err := os.Stat(result.LocalPath); os.IsNotExist(err) {
		t.Error("Downloaded file does not exist")
	}

	// Verify file content
	content, err := os.ReadFile(result.LocalPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if string(content) != "fake image data" {
		t.Errorf("Content mismatch: got %s", string(content))
	}
}

func TestDownloader_DownloadCover_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()

	cfg := &config.OutputConfig{
		DownloadCover: false,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadCover failed: %v", err)
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false when disabled")
	}
}

func TestDownloader_DownloadCover_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()

	cfg := &config.OutputConfig{
		DownloadCover: true,
		FanartFormat:  "<ID>-fanart.jpg",
	}

	downloader := NewDownloader(cfg, "test-agent")

	// Create existing file
	existingPath := filepath.Join(tmpDir, "IPX-535-fanart.jpg")
	if err := os.WriteFile(existingPath, []byte("existing"), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	result, err := downloader.DownloadCover(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadCover failed: %v", err)
	}

	// Should not download again
	if result.Downloaded {
		t.Error("Expected Downloaded to be false for existing file")
	}

	// Content should be unchanged
	content, _ := os.ReadFile(existingPath)
	if string(content) != "existing" {
		t.Error("Existing file was overwritten")
	}
}

func TestDownloader_DownloadExtrafanart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("screenshot data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.Screenshots = []string{
		server.URL + "/screenshot1.jpg",
		server.URL + "/screenshot2.jpg",
		server.URL + "/screenshot3.jpg",
	}

	cfg := &config.OutputConfig{
		DownloadExtrafanart: true,
		ScreenshotFolder:    "extrafanart",
		ScreenshotFormat:    "fanart<INDEX:2>.jpg",
		ScreenshotPadding:   2,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadExtrafanart failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify all were downloaded
	for i, result := range results {
		if !result.Downloaded {
			t.Errorf("Screenshot %d was not downloaded", i+1)
		}

		if result.Type != MediaTypeExtrafanart {
			t.Errorf("Expected type %s, got %s", MediaTypeExtrafanart, result.Type)
		}

		// Verify file exists in extrafanart subdirectory
		if _, err := os.Stat(result.LocalPath); os.IsNotExist(err) {
			t.Errorf("Screenshot file %d does not exist at %s", i+1, result.LocalPath)
		}
	}
}

func TestDownloader_DownloadTrailer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake video data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.TrailerURL = server.URL + "/trailer.mp4"

	cfg := &config.OutputConfig{
		DownloadTrailer: true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadTrailer(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadTrailer failed: %v", err)
	}

	if !result.Downloaded {
		t.Error("Expected Downloaded to be true")
	}

	if result.Type != MediaTypeTrailer {
		t.Errorf("Expected type %s, got %s", MediaTypeTrailer, result.Type)
	}

	expectedPath := filepath.Join(tmpDir, "IPX-535-trailer.mp4")
	if result.LocalPath != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, result.LocalPath)
	}

	// Verify file exists and has content
	content, err := os.ReadFile(result.LocalPath)
	if err != nil {
		t.Fatalf("Failed to read trailer file: %v", err)
	}
	if string(content) != "fake video data" {
		t.Errorf("Content mismatch: got %s", string(content))
	}
}

func TestDownloader_DownloadActressImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("actress image"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.Actresses[0].ThumbURL = server.URL + "/actress1.jpg"
	movie.Actresses[1].ThumbURL = server.URL + "/actress2.jpg"

	cfg := &config.OutputConfig{
		DownloadActress: true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadActressImages(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadActressImages failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Verify actress images
	for _, result := range results {
		if !result.Downloaded {
			t.Error("Expected Downloaded to be true")
		}

		if result.Type != MediaTypeActress {
			t.Errorf("Expected type %s, got %s", MediaTypeActress, result.Type)
		}

		// Verify file exists
		if _, err := os.Stat(result.LocalPath); os.IsNotExist(err) {
			t.Errorf("Actress image does not exist: %s", result.LocalPath)
		}
	}
}

func TestDownloader_Download_BadStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/notfound.jpg"

	cfg := &config.OutputConfig{
		DownloadCover: true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir)
	if err == nil {
		t.Error("Expected error for 404 status")
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false on error")
	}

	if result.Error == nil {
		t.Error("Expected result.Error to be set")
	}
}

func TestDownloader_DownloadAll_MultiPartDeduplication(t *testing.T) {
	// Set up mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		DownloadCover:       true,
		DownloadPoster:      true,
		DownloadExtrafanart: true,
		DownloadTrailer:     true,
		DownloadActress:     true,
		PosterFormat:        "<ID>-poster",
		FanartFormat:        "<ID>-fanart",
		TrailerFormat:       "<ID>-trailer",
	}

	movie := &models.Movie{
		ID:        "IPX-535",
		Title:     "Test Movie",
		CoverURL:  server.URL + "/cover.jpg",
		PosterURL: server.URL + "/poster.jpg",
		Screenshots: []string{
			server.URL + "/screen1.jpg",
			server.URL + "/screen2.jpg",
		},
		TrailerURL: server.URL + "/trailer.mp4",
		Actresses: []models.Actress{
			{ThumbURL: server.URL + "/actress1.jpg"},
		},
	}

	downloader := NewDownloader(cfg, "test-agent")

	// Part 1 should download everything
	resultsPart1, err := downloader.DownloadAll(movie, tmpDir, 1)
	if err != nil {
		t.Fatalf("DownloadAll part 1 failed: %v", err)
	}

	if len(resultsPart1) == 0 {
		t.Error("Expected downloads for part 1, got 0")
	}

	// Part 2 should NOT download anything (deduplication)
	resultsPart2, err := downloader.DownloadAll(movie, tmpDir, 2)
	if err != nil {
		t.Fatalf("DownloadAll part 2 failed: %v", err)
	}

	if len(resultsPart2) != 0 {
		t.Errorf("Expected 0 downloads for part 2 (deduplication), got %d", len(resultsPart2))
	}

	// Part 0 (single file) should download everything
	tmpDir2 := t.TempDir()
	resultsPart0, err := downloader.DownloadAll(movie, tmpDir2, 0)
	if err != nil {
		t.Fatalf("DownloadAll part 0 failed: %v", err)
	}

	if len(resultsPart0) == 0 {
		t.Error("Expected downloads for part 0 (single file), got 0")
	}
}

func TestDownloader_DownloadAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/cover.jpg"
	movie.TrailerURL = server.URL + "/trailer.mp4"
	movie.Screenshots = []string{
		server.URL + "/screenshot1.jpg",
		server.URL + "/screenshot2.jpg",
	}
	movie.Actresses[0].ThumbURL = server.URL + "/actress1.jpg"

	cfg := &config.OutputConfig{
		DownloadCover:       true,
		DownloadPoster:      true,
		DownloadExtrafanart: true,
		DownloadTrailer:     true,
		DownloadActress:     true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadAll(movie, tmpDir, 0) // Part 0 = single file
	if err != nil {
		t.Fatalf("DownloadAll failed: %v", err)
	}

	// Should have: cover, poster, 2 screenshots, trailer, 1 actress = 7 total
	// (But actress 2 has no URL, so it won't be included)
	expectedMin := 5 // At minimum: cover, poster, 2 screenshots, trailer
	if len(results) < expectedMin {
		t.Errorf("Expected at least %d results, got %d", expectedMin, len(results))
	}

	// Count by type
	typeCounts := make(map[MediaType]int)
	for _, result := range results {
		typeCounts[result.Type]++
	}

	if typeCounts[MediaTypeCover] != 1 {
		t.Errorf("Expected 1 cover, got %d", typeCounts[MediaTypeCover])
	}
	if typeCounts[MediaTypeExtrafanart] != 2 {
		t.Errorf("Expected 2 screenshots, got %d", typeCounts[MediaTypeExtrafanart])
	}
}

func TestGetImageExtension(t *testing.T) {
	testCases := []struct {
		url      string
		expected string
	}{
		{"http://example.com/image.jpg", ".jpg"},
		{"http://example.com/image.jpeg", ".jpeg"},
		{"http://example.com/image.png", ".png"},
		{"http://example.com/image.gif", ".gif"},
		{"http://example.com/image.webp", ".webp"},
		{"http://example.com/image", ".jpg"},     // Default
		{"http://example.com/image.JPG", ".jpg"}, // Case insensitive
	}

	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			result := GetImageExtension(tc.url)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestDownloader_generateFilename(t *testing.T) {
	cfg := &config.OutputConfig{
		PosterFormat:     "<ID>-poster.jpg",
		FanartFormat:     "<ID>-fanart.jpg",
		TrailerFormat:    "<ID>-trailer.mp4",
		ScreenshotFormat: "fanart",
		ActressFolder:    ".actors",
	}

	downloader := NewDownloader(cfg, "test-agent")

	movie := createTestMovie()

	tests := []struct {
		name        string
		template    string
		index       int
		expected    string
		description string
	}{
		{
			name:        "Poster template",
			template:    "<ID>-poster.jpg",
			index:       0,
			expected:    "IPX-535-poster.jpg",
			description: "Simple poster template with ID",
		},
		{
			name:        "Fanart template with title",
			template:    "<ID>-<TITLE>-fanart.jpg",
			index:       0,
			expected:    "IPX-535-Test Movie-fanart.jpg",
			description: "Template with title",
		},
		{
			name:        "Screenshot with index",
			template:    "fanart<INDEX:2>.jpg",
			index:       5,
			expected:    "fanart05.jpg",
			description: "Screenshot template with padded index",
		},
		{
			name:        "Complex template",
			template:    "<ID>-<TITLE:10>-<YEAR>.jpg",
			index:       0,
			expected:    "IPX-535-Test Movie-2020.jpg",
			description: "Complex template with title truncation",
		},
		{
			name:        "Empty template",
			template:    "",
			index:       0,
			expected:    "",
			description: "Empty template returns empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downloader.generateFilename(movie, tt.template, tt.index)
			if result != tt.expected {
				t.Errorf("generateFilename() = %q, want %q (%s)", result, tt.expected, tt.description)
			}
		})
	}
}

func TestDownloader_generateFilenameActress(t *testing.T) {
	cfg := &config.OutputConfig{
		PosterFormat:     "<ID>-poster.jpg",
		FanartFormat:     "<ID>-fanart.jpg",
		TrailerFormat:    "<ID>-trailer.mp4",
		ScreenshotFormat: "fanart",
		ActressFolder:    ".actors",
	}

	downloader := NewDownloader(cfg, "test-agent")

	actressMovie := &models.Movie{
		ID:    "IPX-535",
		Title: "Momo Sakura",
	}

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "Actress template",
			template: "actress-<ACTORNAME>.jpg",
			expected: "actress-Momo Sakura.jpg",
		},
		{
			name:     "Actress with ID",
			template: "<ID>-<ACTORNAME>.jpg",
			expected: "IPX-535-Momo Sakura.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downloader.generateFilename(actressMovie, tt.template, 0)
			if result != tt.expected {
				t.Errorf("generateFilename() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCleanupPartialDownloads(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some .tmp files
	tmpFiles := []string{
		"file1.jpg.tmp",
		"file2.jpg.tmp",
		"file3.jpg.tmp",
	}

	for _, name := range tmpFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("temp"), 0644); err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
	}

	// Create a normal file
	normalFile := filepath.Join(tmpDir, "normal.jpg")
	if err := os.WriteFile(normalFile, []byte("normal"), 0644); err != nil {
		t.Fatalf("Failed to create normal file: %v", err)
	}

	// Cleanup
	if err := CleanupPartialDownloads(tmpDir); err != nil {
		t.Fatalf("CleanupPartialDownloads failed: %v", err)
	}

	// Verify .tmp files are gone
	for _, name := range tmpFiles {
		path := filepath.Join(tmpDir, name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Temp file %s was not removed", name)
		}
	}

	// Verify normal file still exists
	if _, err := os.Stat(normalFile); os.IsNotExist(err) {
		t.Error("Normal file was removed")
	}
}

func TestDownloader_SetDownloadExtrafanart(t *testing.T) {
	cfg := &config.OutputConfig{
		DownloadExtrafanart: false,
	}

	downloader := NewDownloader(cfg, "test-agent")

	// Verify initial state
	if downloader.config.DownloadExtrafanart {
		t.Error("Expected DownloadExtrafanart to be false initially")
	}

	// Enable it
	downloader.SetDownloadExtrafanart(true)
	if !downloader.config.DownloadExtrafanart {
		t.Error("Expected DownloadExtrafanart to be true after SetDownloadExtrafanart(true)")
	}

	// Disable it
	downloader.SetDownloadExtrafanart(false)
	if downloader.config.DownloadExtrafanart {
		t.Error("Expected DownloadExtrafanart to be false after SetDownloadExtrafanart(false)")
	}
}

func TestDownloader_DownloadPoster_WithPosterURL(t *testing.T) {
	// Create a real JPEG image for testing
	img := image.NewRGBA(image.Rect(0, 0, 800, 538))
	// Fill with a simple pattern
	for y := 0; y < 538; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		// Encode as JPEG
		jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.PosterURL = server.URL + "/poster.jpg"

	cfg := &config.OutputConfig{
		DownloadPoster: true,
		PosterFormat:   "<ID>-poster.jpg",
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadPoster(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadPoster failed: %v", err)
	}

	if result.Type != MediaTypePoster {
		t.Errorf("Expected type %s, got %s", MediaTypePoster, result.Type)
	}

	if !result.Downloaded {
		t.Error("Expected Downloaded to be true")
	}

	expectedPath := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	if result.LocalPath != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, result.LocalPath)
	}

	// Verify file exists and has content
	info, err := os.Stat(result.LocalPath)
	if err != nil {
		t.Fatalf("Poster file does not exist: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Poster file has zero size")
	}
}

func TestDownloader_DownloadPoster_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()

	cfg := &config.OutputConfig{
		DownloadPoster: false,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadPoster(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadPoster failed: %v", err)
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false when poster download disabled")
	}

	if result.Type != MediaTypePoster {
		t.Errorf("Expected type %s, got %s", MediaTypePoster, result.Type)
	}
}

func TestDownloader_DownloadExtrafanart_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()

	cfg := &config.OutputConfig{
		DownloadExtrafanart: false,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadExtrafanart failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results when disabled, got %d", len(results))
	}
}

func TestDownloader_DownloadExtrafanart_EmptyScreenshots(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.Screenshots = []string{}

	cfg := &config.OutputConfig{
		DownloadExtrafanart: true,
		ScreenshotFolder:    "extrafanart",
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadExtrafanart failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty screenshots, got %d", len(results))
	}
}

func TestDownloader_DownloadExtrafanart_PartialFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 2 {
			// Fail the second request
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("screenshot data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.Screenshots = []string{
		server.URL + "/screenshot1.jpg",
		server.URL + "/screenshot2.jpg",
		server.URL + "/screenshot3.jpg",
	}

	cfg := &config.OutputConfig{
		DownloadExtrafanart: true,
		ScreenshotFolder:    "extrafanart",
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadExtrafanart failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results (including failures), got %d", len(results))
	}

	// Count successful vs failed downloads
	successCount := 0
	failureCount := 0
	for _, result := range results {
		if result.Downloaded {
			successCount++
		} else if result.Error != nil {
			failureCount++
		}
	}

	if successCount != 2 {
		t.Errorf("Expected 2 successful downloads, got %d", successCount)
	}

	if failureCount != 1 {
		t.Errorf("Expected 1 failed download, got %d", failureCount)
	}
}

func TestDownloader_DownloadTrailer_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()

	cfg := &config.OutputConfig{
		DownloadTrailer: false,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadTrailer(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadTrailer failed: %v", err)
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false when disabled")
	}
}

func TestDownloader_DownloadTrailer_EmptyURL(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.TrailerURL = ""

	cfg := &config.OutputConfig{
		DownloadTrailer: true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadTrailer(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadTrailer failed: %v", err)
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false for empty URL")
	}
}

func TestDownloader_DownloadActressImages_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()

	cfg := &config.OutputConfig{
		DownloadActress: false,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadActressImages(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadActressImages failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results when disabled, got %d", len(results))
	}
}

func TestDownloader_DownloadActressImages_EmptyActresses(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.Actresses = []models.Actress{}

	cfg := &config.OutputConfig{
		DownloadActress: true,
		ActressFolder:   ".actors",
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadActressImages(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadActressImages failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty actresses, got %d", len(results))
	}
}

func TestDownloader_DownloadActressImages_SkipEmptyThumbURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("actress image"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	// First actress has URL, second doesn't
	movie.Actresses[0].ThumbURL = server.URL + "/actress1.jpg"
	movie.Actresses[1].ThumbURL = ""

	cfg := &config.OutputConfig{
		DownloadActress: true,
		ActressFolder:   ".actors",
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadActressImages(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadActressImages failed: %v", err)
	}

	// Should only download 1 (skip the actress with empty URL)
	if len(results) != 1 {
		t.Errorf("Expected 1 result (skipping empty URL), got %d", len(results))
	}

	if !results[0].Downloaded {
		t.Error("Expected first actress image to be downloaded")
	}
}

func TestDownloader_Download_InvalidURL(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = "not-a-valid-url://invalid"

	cfg := &config.OutputConfig{
		DownloadCover: true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}

	if result == nil || result.Error == nil {
		t.Error("Expected result with Error set")
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false on error")
	}
}

func TestDownloader_Download_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/error.jpg"

	cfg := &config.OutputConfig{
		DownloadCover: true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir)
	if err == nil {
		t.Error("Expected error for 500 status")
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false on server error")
	}
}

func TestDownloader_Download_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/slow.jpg"

	cfg := &config.OutputConfig{
		DownloadCover:   true,
		DownloadTimeout: 1, // 1 second timeout
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir)
	if err == nil {
		t.Error("Expected timeout error")
	}

	if result.Downloaded {
		t.Error("Expected Downloaded to be false on timeout")
	}
}

func TestDownloader_Download_WithUserAgent(t *testing.T) {
	userAgent := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/cover.jpg"

	cfg := &config.OutputConfig{
		DownloadCover: true,
		FanartFormat:  "<ID>-fanart.jpg",
	}

	expectedUserAgent := "test-custom-agent/1.0"
	downloader := NewDownloader(cfg, expectedUserAgent)

	_, err := downloader.DownloadCover(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadCover failed: %v", err)
	}

	if userAgent != expectedUserAgent {
		t.Errorf("Expected User-Agent %q, got %q", expectedUserAgent, userAgent)
	}
}

func TestDownloader_NewDownloader_DefaultTimeout(t *testing.T) {
	cfg := &config.OutputConfig{
		DownloadTimeout: 0, // Should default to 60
	}

	downloader := NewDownloader(cfg, "test-agent")

	if downloader.httpClient.Timeout != 60*time.Second {
		t.Errorf("Expected default timeout 60s, got %v", downloader.httpClient.Timeout)
	}
}

func TestDownloader_NewDownloader_CustomTimeout(t *testing.T) {
	cfg := &config.OutputConfig{
		DownloadTimeout: 30,
	}

	downloader := NewDownloader(cfg, "test-agent")

	if downloader.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", downloader.httpClient.Timeout)
	}
}

func TestDownloader_DownloadAll_AllDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()

	cfg := &config.OutputConfig{
		DownloadCover:       false,
		DownloadPoster:      false,
		DownloadExtrafanart: false,
		DownloadTrailer:     false,
		DownloadActress:     false,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadAll(movie, tmpDir, 0)
	if err != nil {
		t.Fatalf("DownloadAll failed: %v", err)
	}

	// Even when disabled, the methods return DownloadResult with Downloaded=false
	// Verify that none were actually downloaded
	for _, result := range results {
		if result.Downloaded {
			t.Errorf("Expected no downloads when all disabled, but %s was downloaded", result.Type)
		}
	}
}

func TestCleanupPartialDownloads_NonExistentDir(t *testing.T) {
	err := CleanupPartialDownloads("/nonexistent/directory")
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
}

func TestCleanupPartialDownloads_WithSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory with a .tmp file
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	// Create .tmp file in root
	tmpFile := filepath.Join(tmpDir, "file.tmp")
	if err := os.WriteFile(tmpFile, []byte("temp"), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Cleanup should ignore subdirectories
	if err := CleanupPartialDownloads(tmpDir); err != nil {
		t.Fatalf("CleanupPartialDownloads failed: %v", err)
	}

	// Verify .tmp file is gone
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("Temp file was not removed")
	}

	// Verify subdirectory still exists
	if _, err := os.Stat(subDir); os.IsNotExist(err) {
		t.Error("Subdirectory was removed")
	}
}

func TestNewDownloaderWithNFOConfig(t *testing.T) {
	tests := []struct {
		name                   string
		actorJapaneseNames     bool
		actorFirstNameOrder    bool
		expectedJapanese       bool
		expectedFirstNameOrder bool
	}{
		{
			name:                   "Japanese names, FirstName LastName order",
			actorJapaneseNames:     true,
			actorFirstNameOrder:    true,
			expectedJapanese:       true,
			expectedFirstNameOrder: true,
		},
		{
			name:                   "English names, LastName FirstName order",
			actorJapaneseNames:     false,
			actorFirstNameOrder:    false,
			expectedJapanese:       false,
			expectedFirstNameOrder: false,
		},
		{
			name:                   "Japanese names, LastName FirstName order",
			actorJapaneseNames:     true,
			actorFirstNameOrder:    false,
			expectedJapanese:       true,
			expectedFirstNameOrder: false,
		},
		{
			name:                   "English names, FirstName LastName order",
			actorJapaneseNames:     false,
			actorFirstNameOrder:    true,
			expectedJapanese:       false,
			expectedFirstNameOrder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.OutputConfig{
				DownloadTimeout: 30,
			}

			downloader := NewDownloaderWithNFOConfig(cfg, "test-agent", tt.actorJapaneseNames, tt.actorFirstNameOrder)

			if downloader == nil {
				t.Fatal("NewDownloaderWithNFOConfig returned nil")
			}

			if downloader.actorJapaneseNames != tt.expectedJapanese {
				t.Errorf("Expected actorJapaneseNames=%v, got %v", tt.expectedJapanese, downloader.actorJapaneseNames)
			}

			if downloader.actorFirstNameOrder != tt.expectedFirstNameOrder {
				t.Errorf("Expected actorFirstNameOrder=%v, got %v", tt.expectedFirstNameOrder, downloader.actorFirstNameOrder)
			}

			if downloader.userAgent != "test-agent" {
				t.Errorf("Expected userAgent=%q, got %q", "test-agent", downloader.userAgent)
			}

			if downloader.httpClient.Timeout != 30*time.Second {
				t.Errorf("Expected timeout=30s, got %v", downloader.httpClient.Timeout)
			}
		})
	}
}

func TestFormatActressName(t *testing.T) {
	tests := []struct {
		name           string
		actress        models.Actress
		useJapanese    bool
		firstNameOrder bool
		expectedName   string
		description    string
	}{
		{
			name: "Both names - FirstName LastName order",
			actress: models.Actress{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			useJapanese:    false,
			firstNameOrder: true,
			expectedName:   "Yui Hatano",
			description:    "English names in FirstName LastName order",
		},
		{
			name: "Both names - LastName FirstName order",
			actress: models.Actress{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			useJapanese:    false,
			firstNameOrder: false,
			expectedName:   "Hatano Yui",
			description:    "English names in LastName FirstName order",
		},
		{
			name: "Japanese name preferred",
			actress: models.Actress{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			useJapanese:    true,
			firstNameOrder: true,
			expectedName:   "波多野結衣",
			description:    "Japanese name when configured",
		},
		{
			name: "Only first name",
			actress: models.Actress{
				FirstName:    "Yui",
				JapaneseName: "波多野結衣",
			},
			useJapanese:    false,
			firstNameOrder: true,
			expectedName:   "Yui",
			description:    "Single first name only",
		},
		{
			name: "Only last name",
			actress: models.Actress{
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			useJapanese:    false,
			firstNameOrder: true,
			expectedName:   "Hatano",
			description:    "Single last name only",
		},
		{
			name: "Only Japanese name",
			actress: models.Actress{
				JapaneseName: "波多野結衣",
			},
			useJapanese:    false,
			firstNameOrder: true,
			expectedName:   "波多野結衣",
			description:    "Fallback to Japanese when English names empty",
		},
		{
			name: "Japanese name not available - fallback to FullName",
			actress: models.Actress{
				FirstName: "",
				LastName:  "",
			},
			useJapanese:    true,
			firstNameOrder: true,
			expectedName:   "",
			description:    "Empty name when all fields empty",
		},
		{
			name: "First name with Japanese preference but no Japanese name",
			actress: models.Actress{
				FirstName: "Test",
				LastName:  "Actress",
			},
			useJapanese:    true,
			firstNameOrder: true,
			expectedName:   "Test Actress",
			description:    "Fallback to English when Japanese name unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.OutputConfig{}
			downloader := NewDownloaderWithNFOConfig(cfg, "test", tt.useJapanese, tt.firstNameOrder)

			result := downloader.formatActressName(tt.actress)

			if result != tt.expectedName {
				t.Errorf("%s: Expected %q, got %q", tt.description, tt.expectedName, result)
			}
		})
	}
}

func TestDownloader_DownloadPoster_WithCropping(t *testing.T) {
	// Create a real JPEG image for cropping test
	img := image.NewRGBA(image.Rect(0, 0, 1500, 1000))
	// Fill with a pattern
	for y := 0; y < 1000; y++ {
		for x := 0; x < 1500; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 100, 255})
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.PosterURL = server.URL + "/cover.jpg"
	movie.ShouldCropPoster = true // Enable cropping

	cfg := &config.OutputConfig{
		DownloadPoster: true,
		PosterFormat:   "<ID>-poster.jpg",
	}

	downloader := NewDownloader(cfg, "test-agent")

	result, err := downloader.DownloadPoster(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadPoster with cropping failed: %v", err)
	}

	if !result.Downloaded {
		t.Error("Expected Downloaded to be true")
	}

	expectedPath := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	if result.LocalPath != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, result.LocalPath)
	}

	// Verify file exists
	info, err := os.Stat(result.LocalPath)
	if err != nil {
		t.Fatalf("Cropped poster file does not exist: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Cropped poster file has zero size")
	}

	// Verify temp file was cleaned up
	tempPath := expectedPath + ".full.tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("Temp file was not cleaned up after cropping")
	}
}

func TestDownloader_DownloadActressImages_WithNFOConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("actress image"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()

	tests := []struct {
		name             string
		useJapanese      bool
		firstNameOrder   bool
		expectedFilename string
	}{
		{
			name:             "English FirstName LastName",
			useJapanese:      false,
			firstNameOrder:   true,
			expectedFilename: "Momo Sakura.jpg",
		},
		{
			name:             "English LastName FirstName",
			useJapanese:      false,
			firstNameOrder:   false,
			expectedFilename: "Sakura Momo.jpg",
		},
		{
			name:             "Japanese name",
			useJapanese:      true,
			firstNameOrder:   true,
			expectedFilename: "さくらもも.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := filepath.Join(tmpDir, tt.name)

			movie := &models.Movie{
				ID: "IPX-535",
				Actresses: []models.Actress{
					{
						FirstName:    "Momo",
						LastName:     "Sakura",
						JapaneseName: "さくらもも",
						ThumbURL:     server.URL + "/actress.jpg",
					},
				},
			}

			cfg := &config.OutputConfig{
				DownloadActress: true,
				ActressFolder:   ".actors",
			}

			downloader := NewDownloaderWithNFOConfig(cfg, "test", tt.useJapanese, tt.firstNameOrder)

			results, err := downloader.DownloadActressImages(movie, testDir)
			if err != nil {
				t.Fatalf("DownloadActressImages failed: %v", err)
			}

			if len(results) != 1 {
				t.Fatalf("Expected 1 result, got %d", len(results))
			}

			if !results[0].Downloaded {
				t.Error("Expected actress image to be downloaded")
			}

			// Verify filename contains expected actress name format
			filename := filepath.Base(results[0].LocalPath)
			if filename != tt.expectedFilename {
				t.Errorf("Expected filename %q, got %q", tt.expectedFilename, filename)
			}
		})
	}
}
