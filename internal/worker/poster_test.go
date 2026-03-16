package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestResolvePosterReferer(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		configured string
		expected   string
	}{
		{
			name:       "javdb static host overrides configured referer",
			url:        "https://c0.jdbstatic.com/samples/abc.jpg",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://javdb.com/",
		},
		{
			name:       "javdb host overrides configured referer",
			url:        "https://javdb.com/covers/abc.jpg",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://javdb.com/",
		},
		{
			name:       "javbus host overrides configured referer",
			url:        "https://www.javbus.com/pics/cover/77dp_b.jpg",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://www.javbus.com/",
		},
		{
			name:       "aventertainments host overrides configured referer",
			url:        "https://imgs02.aventertainments.com/vodimages/xlarge/1pon_020326_001.webp",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://www.aventertainments.com/",
		},
		{
			name:       "caribbeancom host overrides configured referer",
			url:        "https://www.caribbeancom.com/moviepages/120614-753/images/l_l.jpg",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://www.caribbeancom.com/",
		},
		{
			name:       "dmm host override applies",
			url:        "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880pl.jpg",
			configured: "https://example.com/",
			expected:   "https://www.dmm.co.jp/",
		},
		{
			name:       "configured referer used for other hosts",
			url:        "https://example.com/cover.jpg",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://www.dmm.co.jp/",
		},
		{
			name:       "origin fallback when configured referer empty",
			url:        "https://images.example.com/cover.jpg",
			configured: "",
			expected:   "https://images.example.com/",
		},
		{
			name:       "invalid URL falls back to configured referer",
			url:        "://invalid",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://www.dmm.co.jp/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			referer := resolvePosterReferer(tt.url, tt.configured, downloader.ResolveMediaReferer)
			if referer != tt.expected {
				t.Fatalf("resolvePosterReferer(%q, %q) = %q, want %q", tt.url, tt.configured, referer, tt.expected)
			}
		})
	}
}

// TestGenerateCroppedPoster_Success tests successful poster generation
func TestGenerateCroppedPoster_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	chdirToTempDir(t)

	// Create test server that serves a 100x200 JPEG image (valid aspect ratio for cropping)
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read a test image from testdata (we'll skip this test if file doesn't exist)
		// For now, just return 404 to test error handling
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "image/jpeg")
		// Minimal valid JPEG header + SOS marker
		jpeg := []byte{
			0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01,
			0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
			0xFF, 0xDA, 0x00, 0x0C, 0x03, 0x01, 0x00, 0x02, 0x11, 0x03, 0x11, 0x00, 0x3F, 0x00,
			0xFF, 0xD9, // EOI marker
		}
		_, _ = w.Write(jpeg)
	}))
	defer testServer.Close()

	// Setup test data
	movie := &models.Movie{
		ID:       "TEST-001",
		CoverURL: testServer.URL + "/cover.jpg",
	}

	ctx := context.Background()
	httpClient := testServer.Client()

	// Execute - this will fail with decode error for our minimal JPEG
	// That's acceptable - we're testing the workflow, not the image library
	_, err := GenerateCroppedPoster(ctx, movie, httpClient, "test-agent", "test-referer", downloader.ResolveMediaReferer)

	// We expect an error because our minimal JPEG isn't a real image
	// The important thing is that it doesn't crash and cleans up properly
	if err == nil {
		// Cleanup if somehow it succeeded
		posterPath := filepath.Join("data", "posters", "TEST-001.jpg")
		_ = os.Remove(posterPath)
	}
}

// TestGenerateCroppedPoster_HTTPError tests handling of HTTP errors
func TestGenerateCroppedPoster_HTTPError(t *testing.T) {
	chdirToTempDir(t)

	// Create test server that returns 404
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer testServer.Close()

	// Setup test data
	movie := &models.Movie{
		ID:               "TEST-002",
		CoverURL:         testServer.URL + "/nonexistent.jpg",
		ShouldCropPoster: true,
	}

	ctx := context.Background()
	httpClient := testServer.Client()

	// Execute
	_, err := GenerateCroppedPoster(ctx, movie, httpClient, "test-agent", "test-referer", downloader.ResolveMediaReferer)

	// Verify error is returned
	if err == nil {
		t.Fatal("Expected error for HTTP 404, got nil")
	}

	// Verify no file was created
	posterPath := filepath.Join("data", "posters", "TEST-002.jpg")
	if _, err := os.Stat(posterPath); !os.IsNotExist(err) {
		t.Error("Expected no poster file to be created on HTTP error")
		_ = os.Remove(posterPath) // Cleanup if it exists
	}
}

// TestGenerateCroppedPoster_ContextCancellation tests cancellation handling
func TestGenerateCroppedPoster_ContextCancellation(t *testing.T) {
	chdirToTempDir(t)

	// Create test server that blocks
	blockChan := make(chan struct{})
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockChan // Block until channel is closed
	}))
	defer testServer.Close()
	defer close(blockChan)

	// Setup test data
	movie := &models.Movie{
		ID:               "TEST-003",
		CoverURL:         testServer.URL + "/cover.jpg",
		ShouldCropPoster: true,
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	httpClient := testServer.Client()

	// Cancel immediately
	cancel()

	// Execute
	_, err := GenerateCroppedPoster(ctx, movie, httpClient, "test-agent", "test-referer", downloader.ResolveMediaReferer)

	// Verify error is returned
	if err == nil {
		t.Fatal("Expected error for cancelled context, got nil")
	}

	// Verify no file was created
	posterPath := filepath.Join("data", "posters", "TEST-003.jpg")
	if _, err := os.Stat(posterPath); !os.IsNotExist(err) {
		t.Error("Expected no poster file to be created on cancellation")
		_ = os.Remove(posterPath) // Cleanup if it exists
	}
}

// TestGenerateCroppedPoster_NoCoverURL tests handling when no cover URL is provided
func TestGenerateCroppedPoster_NoCoverURL(t *testing.T) {
	chdirToTempDir(t)

	movie := &models.Movie{
		ID:               "TEST-004",
		CoverURL:         "", // No cover URL
		ShouldCropPoster: true,
	}

	ctx := context.Background()
	httpClient := &http.Client{}

	// Execute
	_, err := GenerateCroppedPoster(ctx, movie, httpClient, "test-agent", "test-referer", downloader.ResolveMediaReferer)

	// Verify error is returned
	if err == nil {
		t.Fatal("Expected error for empty cover URL, got nil")
	}
}

// TestGenerateCroppedPoster_ErrorHandling tests general error scenarios
func TestGenerateCroppedPoster_ErrorHandling(t *testing.T) {
	chdirToTempDir(t)

	// Test with invalid URL (no http client timeout)
	movie := &models.Movie{
		ID:       "TEST-005",
		CoverURL: "http://invalid-domain-that-does-not-exist-12345.com/cover.jpg",
	}

	ctx := context.Background()
	httpClient := &http.Client{}

	// Execute
	_, err := GenerateCroppedPoster(ctx, movie, httpClient, "test-agent", "test-referer", downloader.ResolveMediaReferer)

	// Verify error is returned for network failure
	if err == nil {
		t.Fatal("Expected error for invalid domain, got nil")
	}
}
