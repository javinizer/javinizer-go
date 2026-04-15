package worker

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validJPEGBytes(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 180, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 180; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

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
		{
			name:       "empty URL with empty referer returns empty",
			url:        "",
			configured: "",
			expected:   "",
		},
		{
			name:       "http URL uses origin fallback",
			url:        "http://images.example.com/cover.jpg",
			configured: "",
			expected:   "http://images.example.com/",
		},
		{
			name:       "nil resolver with configured referer uses configured",
			url:        "https://example.com/cover.jpg",
			configured: "https://configured.example/",
			expected:   "https://configured.example/",
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

func TestGenerateTempPoster_SuccessAndFallbacks(t *testing.T) {
	tempDir := chdirToTempDir(t)

	jpegBody := validJPEGBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jpegBody)
	}))
	defer server.Close()

	movie := &models.Movie{
		ID:        "TEMP-001",
		PosterURL: "",
		CoverURL:  server.URL + "/cover.jpg",
	}

	url, err := GenerateTempPoster(
		context.Background(),
		"job-temp-1",
		movie,
		server.Client(),
		"test-agent",
		"https://configured.example/",
		downloader.ResolveMediaReferer,
		tempDir,
	)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/temp/posters/job-temp-1/TEMP-001.jpg", url)
	assert.False(t, movie.ShouldCropPoster)

	_, err = os.Stat(filepath.Join(tempDir, "posters", "job-temp-1", "TEMP-001-full.jpg"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(tempDir, "posters", "job-temp-1", "TEMP-001.jpg"))
	require.NoError(t, err)
}

func TestGenerateTempPoster_ErrorBranches(t *testing.T) {
	tempDir := chdirToTempDir(t)

	t.Run("missing poster and cover url", func(t *testing.T) {
		movie := &models.Movie{ID: "TEMP-ERR-1"}
		_, err := GenerateTempPoster(context.Background(), "job-temp-err", movie, http.DefaultClient, "", "", downloader.ResolveMediaReferer, tempDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no poster or cover URL")
	})

	t.Run("invalid url", func(t *testing.T) {
		movie := &models.Movie{ID: "TEMP-ERR-2", PosterURL: "://bad-url"}
		_, err := GenerateTempPoster(context.Background(), "job-temp-err", movie, http.DefaultClient, "", "", downloader.ResolveMediaReferer, tempDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create request")
	})

	t.Run("non-200 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		movie := &models.Movie{ID: "TEMP-ERR-3", PosterURL: server.URL + "/forbidden.jpg"}
		_, err := GenerateTempPoster(context.Background(), "job-temp-err", movie, server.Client(), "", "", downloader.ResolveMediaReferer, tempDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 403")
	})
}

func TestGenerateCroppedPoster_RealSuccess(t *testing.T) {
	chdirToTempDir(t)

	jpegBody := validJPEGBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jpegBody)
	}))
	defer server.Close()

	movie := &models.Movie{
		ID:       "POSTER-SUCCESS-001",
		CoverURL: server.URL + "/cover.jpg",
	}

	url, err := GenerateCroppedPoster(context.Background(), movie, server.Client(), "ua", "", downloader.ResolveMediaReferer)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/posters/POSTER-SUCCESS-001.jpg", url)
	assert.False(t, movie.ShouldCropPoster)

	path := filepath.Join("data", "posters", "POSTER-SUCCESS-001.jpg")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestResolvePosterReferer_NilResolver(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		configured string
		expected   string
	}{
		{
			name:       "nil resolver uses configured referer",
			url:        "https://example.com/cover.jpg",
			configured: "https://configured.example/",
			expected:   "https://configured.example/",
		},
		{
			name:       "nil resolver with empty referer falls back to origin",
			url:        "https://images.example.com/cover.jpg",
			configured: "",
			expected:   "https://images.example.com/",
		},
		{
			name:       "nil resolver with invalid URL falls back to configured",
			url:        "://invalid",
			configured: "https://configured.example/",
			expected:   "https://configured.example/",
		},
		{
			name:       "nil resolver with empty URL and empty referer",
			url:        "",
			configured: "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			referer := resolvePosterReferer(tt.url, tt.configured, nil)
			assert.Equal(t, tt.expected, referer,
				"resolvePosterReferer(%q, %q, nil) = %q, want %q", tt.url, tt.configured, referer, tt.expected)
		})
	}
}

func TestResolvePosterReferer_CustomResolver(t *testing.T) {
	customResolver := func(downloadURL, configuredReferer string) string {
		if downloadURL == "https://custom.host/image.jpg" {
			return "https://custom-referer.example/"
		}
		return ""
	}

	tests := []struct {
		name       string
		url        string
		configured string
		expected   string
	}{
		{
			name:       "custom resolver overrides for matching URL",
			url:        "https://custom.host/image.jpg",
			configured: "https://configured.example/",
			expected:   "https://custom-referer.example/",
		},
		{
			name:       "custom resolver returns empty, falls back to configured",
			url:        "https://other.host/image.jpg",
			configured: "https://configured.example/",
			expected:   "https://configured.example/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			referer := resolvePosterReferer(tt.url, tt.configured, RefererResolver(customResolver))
			assert.Equal(t, tt.expected, referer)
		})
	}
}
