package downloader

import (
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type testDownloadProxyResolver struct {
	match            func(host string) bool
	downloadOverride *config.ProxyConfig
	scraperProxy     *config.ProxyConfig
}

func (r testDownloadProxyResolver) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	if r.match == nil {
		return nil, nil, false
	}
	if !r.match(strings.ToLower(strings.TrimSpace(host))) {
		return nil, nil, false
	}
	return r.downloadOverride, r.scraperProxy, true
}

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
		_, _ = w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/cover.jpg"

	cfg := &config.OutputConfig{
		DownloadCover: true,
		FanartFormat:  "<ID>-fanart.jpg",
	}

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	// Create existing file
	existingPath := filepath.Join(tmpDir, "IPX-535-fanart.jpg")
	if err := os.WriteFile(existingPath, []byte("existing"), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	result, err := downloader.DownloadCover(movie, tmpDir, nil)
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
		_, _ = w.Write([]byte("screenshot data"))
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir, nil)
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
		_, _ = w.Write([]byte("fake video data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.TrailerURL = server.URL + "/trailer.mp4"

	cfg := &config.OutputConfig{
		DownloadTrailer: true,
	}

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadTrailer(movie, tmpDir, nil)
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
		_, _ = w.Write([]byte("actress image"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.Actresses[0].ThumbURL = server.URL + "/actress1.jpg"
	movie.Actresses[1].ThumbURL = server.URL + "/actress2.jpg"

	cfg := &config.OutputConfig{
		DownloadActress: true,
	}

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir, nil)
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

func TestDownloader_DownloadAll_MultiPartFilenames(t *testing.T) {
	// Test that multipart templates produce correct filenames
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	// Use multipart conditional templates
	cfg := &config.OutputConfig{
		DownloadCover:       true,
		DownloadPoster:      false, // Skip poster to simplify test
		DownloadExtrafanart: false,
		DownloadTrailer:     false,
		DownloadActress:     false,
		FanartFormat:        "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
	}

	movie := &models.Movie{
		ID:       "IPX-535",
		Title:    "Test Movie",
		CoverURL: server.URL + "/cover.jpg",
	}

	dl := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	// Test single file (no multipart)
	tmpDir1 := t.TempDir()
	results1, err := dl.DownloadAll(movie, tmpDir1, nil)
	if err != nil {
		t.Fatalf("DownloadAll (single) failed: %v", err)
	}
	if len(results1) == 0 {
		t.Fatal("Expected at least one download")
	}
	// Should be IPX-535-fanart.jpg (no -pt suffix)
	expectedSingle := filepath.Join(tmpDir1, "IPX-535-fanart.jpg")
	if results1[0].LocalPath != expectedSingle {
		t.Errorf("Single file: expected %s, got %s", expectedSingle, results1[0].LocalPath)
	}

	// Test multipart file
	tmpDir2 := t.TempDir()
	multipart := &MultipartInfo{IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"}
	results2, err := dl.DownloadAll(movie, tmpDir2, multipart)
	if err != nil {
		t.Fatalf("DownloadAll (multipart) failed: %v", err)
	}
	if len(results2) == 0 {
		t.Fatal("Expected at least one download")
	}
	// Should be IPX-535-pt1-fanart.jpg (with -pt1 suffix)
	expectedMulti := filepath.Join(tmpDir2, "IPX-535-pt1-fanart.jpg")
	if results2[0].LocalPath != expectedMulti {
		t.Errorf("Multipart file: expected %s, got %s", expectedMulti, results2[0].LocalPath)
	}
}

func TestDownloader_DownloadAll_MultiPartDeduplication(t *testing.T) {
	// Set up mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()

	// Test with templates WITHOUT multipart placeholders (shared filenames)
	// This tests that file-exists deduplication works correctly
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	// Part 1 should download everything
	multipartPart1 := &MultipartInfo{IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"}
	resultsPart1, err := downloader.DownloadAll(movie, tmpDir, multipartPart1)
	if err != nil {
		t.Fatalf("DownloadAll part 1 failed: %v", err)
	}

	if len(resultsPart1) == 0 {
		t.Error("Expected downloads for part 1, got 0")
	}

	// Count how many were actually downloaded
	downloadedPart1 := 0
	for _, r := range resultsPart1 {
		if r.Downloaded {
			downloadedPart1++
		}
	}

	// Part 2 returns results but with Downloaded=false (files already exist with same names)
	// Note: Actress images are not included for part 2+ (only first part downloads them)
	multipartPart2 := &MultipartInfo{IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"}
	resultsPart2, err := downloader.DownloadAll(movie, tmpDir, multipartPart2)
	if err != nil {
		t.Fatalf("DownloadAll part 2 failed: %v", err)
	}

	// Part 2 should return results (cover, poster, screenshots, trailer - no actresses)
	// but none should be newly downloaded (files already exist with same names)
	downloadedPart2 := 0
	for _, r := range resultsPart2 {
		if r.Downloaded {
			downloadedPart2++
		}
	}

	if downloadedPart2 != 0 {
		t.Errorf("Expected 0 new downloads for part 2 (files already exist), got %d", downloadedPart2)
	}

	// nil (single file) should download everything
	tmpDir2 := t.TempDir()
	resultsSingle, err := downloader.DownloadAll(movie, tmpDir2, nil)
	if err != nil {
		t.Fatalf("DownloadAll single file failed: %v", err)
	}

	if len(resultsSingle) == 0 {
		t.Error("Expected downloads for single file, got 0")
	}
}

func TestDownloader_DownloadAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test data"))
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	results, err := downloader.DownloadAll(movie, tmpDir, nil) // nil = single file
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

func TestResolveDownloadReferer(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "javbus host uses javbus referer",
			url:      "https://www.javbus.com/pics/cover/77dp_b.jpg",
			expected: "https://www.javbus.com/",
		},
		{
			name:     "javdb static host uses javdb referer",
			url:      "https://c0.jdbstatic.com/samples/abc.jpg",
			expected: "https://javdb.com/",
		},
		{
			name:     "dmm host uses dmm referer",
			url:      "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880jp-1.jpg",
			expected: "https://www.dmm.co.jp/",
		},
		{
			name:     "libredmm host uses libredmm referer",
			url:      "https://imageproxy.libredmm.com/_refabc/https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg",
			expected: "https://www.libredmm.com/",
		},
		{
			name:     "unknown host falls back to origin",
			url:      "https://images.example.com/a/b.jpg",
			expected: "https://images.example.com/",
		},
		{
			name:     "invalid url returns empty",
			url:      "://invalid",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDownloadReferer(tt.url)
			if got != tt.expected {
				t.Errorf("resolveDownloadReferer(%q) = %q, want %q", tt.url, got, tt.expected)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	movie := createTestMovie()

	tests := []struct {
		name        string
		template    string
		index       int
		multipart   *MultipartInfo
		expected    string
		description string
	}{
		{
			name:        "Poster template",
			template:    "<ID>-poster.jpg",
			index:       0,
			multipart:   nil,
			expected:    "IPX-535-poster.jpg",
			description: "Simple poster template with ID",
		},
		{
			name:        "Fanart template with title",
			template:    "<ID>-<TITLE>-fanart.jpg",
			index:       0,
			multipart:   nil,
			expected:    "IPX-535-Test Movie-fanart.jpg",
			description: "Template with title",
		},
		{
			name:        "Screenshot with index",
			template:    "fanart<INDEX:2>.jpg",
			index:       5,
			multipart:   nil,
			expected:    "fanart05.jpg",
			description: "Screenshot template with padded index",
		},
		{
			name:        "Complex template",
			template:    "<ID>-<TITLE:10>-<YEAR>.jpg",
			index:       0,
			multipart:   nil,
			expected:    "IPX-535-Test Movie-2020.jpg",
			description: "Complex template with title truncation",
		},
		{
			name:        "Empty template",
			template:    "",
			index:       0,
			multipart:   nil,
			expected:    "",
			description: "Empty template returns empty string",
		},
		// Multipart conditional templating tests
		{
			name:        "Multipart conditional - single file (nil multipart)",
			template:    "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
			index:       0,
			multipart:   nil,
			expected:    "IPX-535-poster.jpg",
			description: "Single file should not include multipart section",
		},
		{
			name:        "Multipart conditional - multi-part file part 1",
			template:    "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
			index:       0,
			multipart:   &MultipartInfo{IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
			expected:    "IPX-535-pt1-poster.jpg",
			description: "Multi-part file part 1 should include part number",
		},
		{
			name:        "Multipart conditional - multi-part file part 2",
			template:    "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
			index:       0,
			multipart:   &MultipartInfo{IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
			expected:    "IPX-535-pt2-poster.jpg",
			description: "Multi-part file part 2 should include part number",
		},
		{
			name:        "Multipart PARTSUFFIX placeholder",
			template:    "<ID><PARTSUFFIX>-fanart.jpg",
			index:       0,
			multipart:   &MultipartInfo{IsMultiPart: true, PartNumber: 1, PartSuffix: "-A"},
			expected:    "IPX-535-A-fanart.jpg",
			description: "PARTSUFFIX should use original suffix from filename",
		},
		{
			name:        "Multipart PARTSUFFIX - single file (empty)",
			template:    "<ID><PARTSUFFIX>-fanart.jpg",
			index:       0,
			multipart:   nil,
			expected:    "IPX-535-fanart.jpg",
			description: "PARTSUFFIX should be empty for single files",
		},
		{
			name:        "Multipart PART:2 zero-padded",
			template:    "<ID>-pt<PART:2>-poster.jpg",
			index:       0,
			multipart:   &MultipartInfo{IsMultiPart: true, PartNumber: 3, PartSuffix: "-pt3"},
			expected:    "IPX-535-pt03-poster.jpg",
			description: "PART:2 should zero-pad to 2 digits",
		},
		{
			name:        "Multipart conditional with fanart",
			template:    "<ID><IF:MULTIPART><PARTSUFFIX></IF>-fanart.jpg",
			index:       0,
			multipart:   &MultipartInfo{IsMultiPart: true, PartNumber: 2, PartSuffix: "-cd2"},
			expected:    "IPX-535-cd2-fanart.jpg",
			description: "Fanart template with multipart conditional and PARTSUFFIX",
		},
		{
			name:        "Multipart conditional with trailer",
			template:    "<ID><IF:MULTIPART>-part<PART></IF>-trailer.mp4",
			index:       0,
			multipart:   &MultipartInfo{IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
			expected:    "IPX-535-part1-trailer.mp4",
			description: "Trailer template with multipart conditional",
		},
		{
			name:        "Multipart IsMultiPart false should not render conditional",
			template:    "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
			index:       0,
			multipart:   &MultipartInfo{IsMultiPart: false, PartNumber: 0, PartSuffix: ""},
			expected:    "IPX-535-poster.jpg",
			description: "IsMultiPart=false should not render conditional content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downloader.generateFilename(movie, tt.template, tt.index, tt.multipart)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

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
			result := downloader.generateFilename(actressMovie, tt.template, 0, nil)
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
	if err := CleanupPartialDownloads(afero.NewOsFs(), tmpDir); err != nil {
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

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
		require.NoError(t, jpeg.Encode(w, img, &jpeg.Options{Quality: 85}))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.PosterURL = server.URL + "/poster.jpg"

	cfg := &config.OutputConfig{
		DownloadPoster: true,
		PosterFormat:   "<ID>-poster.jpg",
	}

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadPoster(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadPoster(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir, nil)
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
		_, _ = w.Write([]byte("screenshot data"))
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	results, err := downloader.DownloadExtrafanart(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadTrailer(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadTrailer(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

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
		_, _ = w.Write([]byte("actress image"))
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir, nil)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir, nil)
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
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/slow.jpg"

	cfg := &config.OutputConfig{
		DownloadCover:   true,
		DownloadTimeout: 1, // 1 second timeout
	}

	// Create HTTP client with timeout for this test
	httpClient, err := NewHTTPClientForDownloader(&config.Config{
		Output: *cfg,
	})
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	downloader := NewDownloader(httpClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadCover(movie, tmpDir, nil)
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
		_, _ = w.Write([]byte("data"))
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
	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, expectedUserAgent)

	_, err := downloader.DownloadCover(movie, tmpDir, nil)
	if err != nil {
		t.Fatalf("DownloadCover failed: %v", err)
	}

	if userAgent != expectedUserAgent {
		t.Errorf("Expected User-Agent %q, got %q", expectedUserAgent, userAgent)
	}
}

func TestDownloader_NewHTTPClientForDownloader_DefaultTimeout(t *testing.T) {
	cfg := &config.Config{
		Output: config.OutputConfig{
			DownloadTimeout: 0, // Should default to 60
		},
	}

	client, err := NewHTTPClientForDownloader(cfg)
	if err != nil {
		t.Fatalf("NewHTTPClientForDownloader failed: %v", err)
	}

	adaptiveClient, ok := client.(*adaptiveDownloaderHTTPClient)
	if !ok {
		t.Fatalf("Expected *adaptiveDownloaderHTTPClient, got %T", client)
	}

	if adaptiveClient.timeout != 60*time.Second {
		t.Errorf("Expected default timeout 60s, got %v", adaptiveClient.timeout)
	}
}

func TestDownloader_NewHTTPClientForDownloader_CustomTimeout(t *testing.T) {
	cfg := &config.Config{
		Output: config.OutputConfig{
			DownloadTimeout: 30,
		},
	}

	client, err := NewHTTPClientForDownloader(cfg)
	if err != nil {
		t.Fatalf("NewHTTPClientForDownloader failed: %v", err)
	}

	adaptiveClient, ok := client.(*adaptiveDownloaderHTTPClient)
	if !ok {
		t.Fatalf("Expected *adaptiveDownloaderHTTPClient, got %T", client)
	}

	if adaptiveClient.timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", adaptiveClient.timeout)
	}
}

func TestDownloader_NewHTTPClientForDownloader_UseMainProxyForAllDownloads(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Output.DownloadProxy = config.ProxyConfig{
		Enabled:      true,
		UseMainProxy: true,
	}

	client, err := NewHTTPClientForDownloader(cfg)
	if err != nil {
		t.Fatalf("NewHTTPClientForDownloader failed: %v", err)
	}

	adaptiveClient, ok := client.(*adaptiveDownloaderHTTPClient)
	if !ok {
		t.Fatalf("Expected *adaptiveDownloaderHTTPClient, got %T", client)
	}
	if adaptiveClient.forceClient == nil {
		t.Fatal("Expected forceClient to be configured when using main proxy for downloads")
	}
}

func TestDownloader_NewHTTPClientForDownloader_UseMainProxyWithoutGlobalProxyFallsBack(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: false,
		URL:     "",
	}
	cfg.Output.DownloadProxy = config.ProxyConfig{
		Enabled:      true,
		UseMainProxy: true,
	}

	client, err := NewHTTPClientForDownloader(cfg)
	if err != nil {
		t.Fatalf("NewHTTPClientForDownloader failed: %v", err)
	}

	adaptiveClient, ok := client.(*adaptiveDownloaderHTTPClient)
	if !ok {
		t.Fatalf("Expected *adaptiveDownloaderHTTPClient, got %T", client)
	}
	if adaptiveClient.forceClient != nil {
		t.Fatal("Expected forceClient to be nil when main proxy reuse is enabled but scrapers.proxy is not configured")
	}
}

func TestDownloader_NewHTTPClientForDownloader_GlobalProxyDoesNotForceAllDownloads(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}

	client, err := NewHTTPClientForDownloader(cfg)
	if err != nil {
		t.Fatalf("NewHTTPClientForDownloader failed: %v", err)
	}

	adaptiveClient, ok := client.(*adaptiveDownloaderHTTPClient)
	if !ok {
		t.Fatalf("Expected *adaptiveDownloaderHTTPClient, got %T", client)
	}
	if adaptiveClient.forceClient != nil {
		t.Fatal("Expected forceClient to remain nil when only global scraper proxy is enabled")
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_UsesScraperDownloadOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Scrapers.JavDB.Proxy = &config.ProxyConfig{
		Enabled: true,
		URL:     "http://javdb-main-proxy.example.com:8080",
	}
	cfg.Scrapers.JavDB.DownloadProxy = &config.ProxyConfig{
		Enabled: true,
		URL:     "http://javdb-download-proxy.example.com:8080",
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match: func(host string) bool {
					return strings.HasSuffix(host, "jdbstatic.com") || strings.HasSuffix(host, "javdb.com")
				},
				downloadOverride: cfg.Scrapers.JavDB.DownloadProxy,
				scraperProxy:     cfg.Scrapers.JavDB.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://c0.jdbstatic.com/samples/x.jpg", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved == nil {
		t.Fatal("Expected proxy to be selected")
	}
	if resolved.URL != "http://javdb-download-proxy.example.com:8080" {
		t.Fatalf("Expected scraper download proxy URL, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_AVEntertainmentNoScraperProxyUsesDirect(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Scrapers.AVEntertainment.Proxy = &config.ProxyConfig{
		Enabled: false,
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match:            func(host string) bool { return strings.HasSuffix(host, "aventertainments.com") },
				downloadOverride: cfg.Scrapers.AVEntertainment.DownloadProxy,
				scraperProxy:     cfg.Scrapers.AVEntertainment.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://imgs02.aventertainments.com/vodimages/screenshot/large/1pon_020326_001/001.webp", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved != nil {
		t.Fatalf("Expected direct download for AVEntertainment host when scraper proxy is disabled, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_CaribbeancomUsesScraperProxy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Scrapers.Caribbeancom.Proxy = &config.ProxyConfig{
		Enabled: true,
		URL:     "http://caribbeancom-proxy.example.com:8080",
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match:            func(host string) bool { return strings.HasSuffix(host, "caribbeancom.com") },
				downloadOverride: cfg.Scrapers.Caribbeancom.DownloadProxy,
				scraperProxy:     cfg.Scrapers.Caribbeancom.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://www.caribbeancom.com/moviepages/120614-753/images/l_l.jpg", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved == nil {
		t.Fatal("Expected scraper proxy for caribbeancom host")
	}
	if resolved.URL != "http://caribbeancom-proxy.example.com:8080" {
		t.Fatalf("Expected caribbeancom scraper proxy URL, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_FC2NoScraperProxyUsesDirect(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Scrapers.FC2.Proxy = &config.ProxyConfig{
		Enabled: false,
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match:            func(host string) bool { return strings.HasSuffix(host, "fc2.com") },
				downloadOverride: cfg.Scrapers.FC2.DownloadProxy,
				scraperProxy:     cfg.Scrapers.FC2.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://contents-thumbnail2.fc2.com/w1280/storage201000.contents.fc2.com/file/x.jpg", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved != nil {
		t.Fatalf("Expected direct download for FC2 host when scraper proxy is disabled, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_FC2DownloadOverrideWins(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Scrapers.FC2.Proxy = &config.ProxyConfig{
		Enabled: true,
		URL:     "http://fc2-main-proxy.example.com:8080",
	}
	cfg.Scrapers.FC2.DownloadProxy = &config.ProxyConfig{
		Enabled: true,
		URL:     "http://fc2-download-proxy.example.com:8080",
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match:            func(host string) bool { return strings.HasSuffix(host, "fc2.com") },
				downloadOverride: cfg.Scrapers.FC2.DownloadProxy,
				scraperProxy:     cfg.Scrapers.FC2.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://storage201000.contents.fc2.com/file/x.jpg", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved == nil {
		t.Fatal("Expected FC2 download proxy to be selected")
	}
	if resolved.URL != "http://fc2-download-proxy.example.com:8080" {
		t.Fatalf("Expected FC2 download proxy URL, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_UnknownHostFallsBackToGlobal(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match: func(host string) bool {
					return strings.HasSuffix(host, "jdbstatic.com") || strings.HasSuffix(host, "javdb.com")
				},
				downloadOverride: cfg.Scrapers.JavDB.DownloadProxy,
				scraperProxy:     cfg.Scrapers.JavDB.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example-cdn.invalid/path/image.jpg", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved == nil {
		t.Fatal("Expected global proxy fallback for unknown host")
	}
	if resolved.URL != "http://global-proxy.example.com:8080" {
		t.Fatalf("Expected global proxy URL, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_UsesDownloadProxyProfileWithoutEnabledToggle(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled:        true,
		DefaultProfile: "main",
		Profiles: map[string]config.ProxyProfile{
			"main": {
				URL: "http://global-proxy.example.com:8080",
			},
			"download": {
				URL: "http://javdb-download-proxy.example.com:8080",
			},
		},
	}
	cfg.Scrapers.JavDB.Proxy = &config.ProxyConfig{
		Enabled: true,
		Profile: "main",
	}
	cfg.Scrapers.JavDB.DownloadProxy = &config.ProxyConfig{
		Profile: "download",
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match: func(host string) bool {
					return strings.HasSuffix(host, "jdbstatic.com") || strings.HasSuffix(host, "javdb.com")
				},
				downloadOverride: cfg.Scrapers.JavDB.DownloadProxy,
				scraperProxy:     cfg.Scrapers.JavDB.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://c0.jdbstatic.com/samples/x.jpg", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved == nil {
		t.Fatal("Expected download proxy profile to be selected")
	}
	if resolved.URL != "http://javdb-download-proxy.example.com:8080" {
		t.Fatalf("Expected download profile URL, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_ReusesMainProxyWhenRequested(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Scrapers.JavDB.Proxy = &config.ProxyConfig{
		Enabled: true,
		URL:     "http://javdb-main-proxy.example.com:8080",
	}
	cfg.Scrapers.JavDB.DownloadProxy = &config.ProxyConfig{
		Enabled:      true,
		UseMainProxy: true,
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match: func(host string) bool {
					return strings.HasSuffix(host, "jdbstatic.com") || strings.HasSuffix(host, "javdb.com")
				},
				downloadOverride: cfg.Scrapers.JavDB.DownloadProxy,
				scraperProxy:     cfg.Scrapers.JavDB.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://javdb.com/v/mgnl032", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved == nil {
		t.Fatal("Expected proxy to be selected")
	}
	if resolved.URL != "http://javdb-main-proxy.example.com:8080" {
		t.Fatalf("Expected scraper main proxy URL, got %q", resolved.URL)
	}
}

func TestAdaptiveDownloader_SelectProxyForRequest_EmptyDownloadOverrideInheritsScraperProxy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy = config.ProxyConfig{
		Enabled: true,
		URL:     "http://global-proxy.example.com:8080",
	}
	cfg.Scrapers.JavDB.Proxy = &config.ProxyConfig{
		Enabled: true,
		URL:     "http://javdb-main-proxy.example.com:8080",
	}
	cfg.Scrapers.JavDB.DownloadProxy = &config.ProxyConfig{
		// Empty override should be treated as inherit scraper/global proxy.
	}

	client := &adaptiveDownloaderHTTPClient{
		cfg: cfg,
		proxyResolvers: []models.ScraperDownloadProxyResolver{
			testDownloadProxyResolver{
				match: func(host string) bool {
					return strings.HasSuffix(host, "jdbstatic.com") || strings.HasSuffix(host, "javdb.com")
				},
				downloadOverride: cfg.Scrapers.JavDB.DownloadProxy,
				scraperProxy:     cfg.Scrapers.JavDB.Proxy,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "https://c0.jdbstatic.com/samples/x.jpg", nil)

	resolved := client.selectProxyForRequest(req)
	if resolved == nil {
		t.Fatal("Expected inherited scraper proxy")
	}
	if resolved.URL != "http://javdb-main-proxy.example.com:8080" {
		t.Fatalf("Expected inherited scraper proxy URL, got %q", resolved.URL)
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	results, err := downloader.DownloadAll(movie, tmpDir, nil)
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
	err := CleanupPartialDownloads(afero.NewOsFs(), "/nonexistent/directory")
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
	if err := CleanupPartialDownloads(afero.NewOsFs(), tmpDir); err != nil {
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

			downloader := NewDownloaderWithNFOConfig(http.DefaultClient, afero.NewMemMapFs(), cfg, "test-agent", tt.actorJapaneseNames, tt.actorFirstNameOrder)

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

			// HTTP client timeout checking moved to TestDownloader_NewHTTPClientForDownloader_CustomTimeout
			if downloader.httpClient == nil {
				t.Error("Expected httpClient to be set, got nil")
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
			downloader := NewDownloaderWithNFOConfig(http.DefaultClient, afero.NewMemMapFs(), cfg, "test", tt.useJapanese, tt.firstNameOrder)

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
		require.NoError(t, jpeg.Encode(w, img, &jpeg.Options{Quality: 85}))
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

	downloader := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, "test-agent")

	result, err := downloader.DownloadPoster(movie, tmpDir, nil)
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
		_, _ = w.Write([]byte("actress image"))
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

			downloader := NewDownloaderWithNFOConfig(http.DefaultClient, afero.NewMemMapFs(), cfg, "test", tt.useJapanese, tt.firstNameOrder)

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

// BenchmarkDownload measures the performance of downloading a 1MB file with mocked HTTP client
// This benchmark is for observation only - not a pass/fail gate
// Expected baseline: ~100ms per operation for mocked I/O
func BenchmarkDownload(b *testing.B) {
	// Setup: Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate 1MB response body
		data := make([]byte, 1024*1024) // 1MB
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	// Setup: Configure downloader with in-memory filesystem
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		DownloadCover: true,
		FanartFormat:  "<ID>-fanart.jpg",
	}
	downloader := NewDownloader(http.DefaultClient, fs, cfg, "benchmark-agent")

	// Setup: Create test movie
	movie := createTestMovie()
	movie.CoverURL = server.URL + "/cover.jpg"
	destDir := "/tmp/benchmark"

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Benchmark loop
	for i := 0; i < b.N; i++ {
		_, err := downloader.DownloadCover(movie, destDir, nil)
		if err != nil {
			b.Fatalf("DownloadCover failed: %v", err)
		}
	}
}
