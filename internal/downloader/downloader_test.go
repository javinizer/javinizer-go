package downloader

import (
	"fmt"
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

	expectedPath := filepath.Join(tmpDir, "IPX-535-poster.jpg")
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
	}

	downloader := NewDownloader(cfg, "test-agent")

	// Create existing file
	existingPath := filepath.Join(tmpDir, "IPX-535-poster.jpg")
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

func TestDownloader_DownloadScreenshots(t *testing.T) {
	// Create test server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("screenshot %d", callCount)))
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
		DownloadScreenshots: true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadScreenshots(movie, tmpDir)
	if err != nil {
		t.Fatalf("DownloadScreenshots failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify all were downloaded
	for i, result := range results {
		if !result.Downloaded {
			t.Errorf("Screenshot %d was not downloaded", i+1)
		}

		if result.Type != MediaTypeScreenshot {
			t.Errorf("Expected type %s, got %s", MediaTypeScreenshot, result.Type)
		}

		expectedPath := filepath.Join(tmpDir, fmt.Sprintf("IPX-535-screenshot%02d.jpg", i+1))
		if result.LocalPath != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, result.LocalPath)
		}

		// Verify file exists
		if _, err := os.Stat(result.LocalPath); os.IsNotExist(err) {
			t.Errorf("Screenshot file %d does not exist", i+1)
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
		DownloadScreenshots: true,
		DownloadTrailer:     true,
		DownloadActress:     true,
	}

	downloader := NewDownloader(cfg, "test-agent")

	results, err := downloader.DownloadAll(movie, tmpDir)
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
	if typeCounts[MediaTypeScreenshot] != 2 {
		t.Errorf("Expected 2 screenshots, got %d", typeCounts[MediaTypeScreenshot])
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
