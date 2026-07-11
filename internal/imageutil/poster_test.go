package imageutil

import (
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstructAwsimgsrcPosterURL(t *testing.T) {
	tests := []struct {
		name        string
		coverURL    string
		expectedURL string
	}{
		{
			name:        "digital video format",
			coverURL:    "https://pics.dmm.co.jp/digital/video/sone00860/sone00860pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/video/sone00860/sone00860ps.jpg",
		},
		{
			name:        "mono movie format",
			coverURL:    "https://pics.dmm.co.jp/mono/movie/adult/118abw001/118abw001pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/mono/movie/118abw001/118abw001ps.jpg",
		},
		{
			name:        "awsimgsrc already - pl.jpg",
			coverURL:    "https://awsimgsrc.dmm.com/dig/digital/video/ipx00535/ipx00535pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/video/ipx00535/ipx00535ps.jpg",
		},
		{
			name:        "awsimgsrc mono format - pl.jpg",
			coverURL:    "https://awsimgsrc.dmm.com/dig/mono/movie/mdb087/mdb087pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/mono/movie/mdb087/mdb087ps.jpg",
		},
		{
			name:        "empty URL",
			coverURL:    "",
			expectedURL: "",
		},
		{
			name:        "invalid URL format",
			coverURL:    "https://example.com/image.jpg",
			expectedURL: "",
		},
		{
			name:        "digital amateur format",
			coverURL:    "https://pics.dmm.co.jp/digital/amateur/oreco183/oreco183pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/amateur/oreco183/oreco183ps.jpg",
		},
		{
			name:        "awsimgsrc.dmm.co.jp domain",
			coverURL:    "https://awsimgsrc.dmm.co.jp/pics_dig/video/sone00860/sone00860pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.co.jp/pics_dig/video/sone00860/sone00860ps.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructAwsimgsrcPosterURL(tt.coverURL)
			if result != tt.expectedURL {
				t.Errorf("constructAwsimgsrcPosterURL() = %v, want %v", result, tt.expectedURL)
			}
		})
	}
}

func TestGetOptimalPosterURL(t *testing.T) {
	tests := []struct {
		name            string
		coverURL        string
		expectedCrop    bool
		expectedContain string // Check if result contains this substring
	}{
		{
			name:            "empty cover URL",
			coverURL:        "",
			expectedCrop:    false, // nothing to crop when there's no URL at all
			expectedContain: "",
		},
		{
			name:            "invalid cover URL format",
			coverURL:        "https://example.com/image.jpg",
			expectedCrop:    true, // no portrait poster found: caller must crop the cover
			expectedContain: "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			posterURL, shouldCrop := GetOptimalPosterURL(tt.coverURL, nil)

			if shouldCrop != tt.expectedCrop {
				t.Errorf("GetOptimalPosterURL() shouldCrop = %v, want %v", shouldCrop, tt.expectedCrop)
			}

			if tt.expectedContain != "" && posterURL != tt.coverURL {
				t.Errorf("GetOptimalPosterURL() posterURL = %v, want %v", posterURL, tt.coverURL)
			}
		})
	}
}

func createTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a simple color
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{100, 150, 200, 255})
		}
	}

	// Encode to JPEG in memory
	buf := &testBuffer{}
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("Failed to encode test JPEG: %v", err)
	}
	return buf.Bytes()
}

type testBuffer struct {
	data []byte
}

func (b *testBuffer) Write(p []byte) (n int, err error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *testBuffer) Bytes() []byte {
	return b.data
}

// TestGetOptimalPosterURL_UpgradeCoverResolution was removed: the original
// test asserted GetOptimalPosterURL upgrades ps.jpg -> pl.jpg on the success
// path, but that behavior is the bug behind issue #31 (pl.jpg is the
// landscape jacket, not a higher-res portrait poster). The success path now
// returns the awsimgsrc ps.jpg directly. The network-dependent awsimgsrc
// fetch can't be reliably exercised here, so the regression is covered by
// TestScraperResultNormalizeMediaURLs (which deterministically asserts
// PosterURL is preserved as ps.jpg) plus end-to-end CLI verification.

func TestGetOptimalPosterURL_WithHTTPServer(t *testing.T) {
	// Create test images with different dimensions
	highQualityImage := createTestJPEG(t, 1000, 1500) // Meets requirements
	mihdImage := createTestJPEG(t, 714, 972)          // Real MIHD-001 poster size
	lowQualityImage := createTestJPEG(t, 500, 700)    // Too small

	tests := []struct {
		name            string
		posterImageData []byte
		posterStatus    int
		expectedCrop    bool
	}{
		{
			name:            "high quality poster - use awsimgsrc",
			posterImageData: highQualityImage,
			posterStatus:    http.StatusOK,
			expectedCrop:    false, // meets MinPoster dimensions: return ps.jpg directly
		},
		{
			name:            "real DMM poster below old threshold (MIHD-001 714x972)",
			posterImageData: mihdImage,
			posterStatus:    http.StatusOK,
			expectedCrop:    false, // real poster art preferred over cropping the landscape cover
		},
		{
			name:            "low quality poster - fallback to cover",
			posterImageData: lowQualityImage,
			posterStatus:    http.StatusOK,
			expectedCrop:    true, // too small: caller must crop the cover
		},
		{
			name:            "poster not found - fallback to cover",
			posterImageData: nil,
			posterStatus:    http.StatusNotFound,
			expectedCrop:    true, // no high-quality portrait: caller must crop the cover
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.posterStatus != http.StatusOK {
					w.WriteHeader(tt.posterStatus)
					return
				}
				w.Header().Set("Content-Type", "image/jpeg")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(tt.posterImageData)
			}))
			defer server.Close()

			// Use a real awsimgsrc.dmm.com cover URL (so constructAwsimgsrcURL
			// takes the already-awsimgsrc branch and produces a ps.jpg URL on
			// the same host), but intercept HTTP requests via a custom transport
			// that routes awsimgsrc.dmm.com to our local test server. This lets
			// us exercise the dimension-checking logic without hitting the real
			// CDN.
			testCoverURL := "https://awsimgsrc.dmm.com/dig/digital/video/sone00860/sone00860pl.jpg"
			client := &http.Client{
				Timeout: 5 * time.Second,
				Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = server.URL[:strings.Index(server.URL, ":")]
					req.URL.Host = server.URL[strings.Index(server.URL, ":")+3:]
					return http.DefaultTransport.RoundTrip(req)
				}),
			}
			posterURL, shouldCrop := GetOptimalPosterURL(testCoverURL, client)

			if shouldCrop != tt.expectedCrop {
				t.Errorf("shouldCrop = %v, want %v (posterURL=%s)", shouldCrop, tt.expectedCrop, posterURL)
			}
		})
	}
}

func TestGetImageDimensions(t *testing.T) {
	tests := []struct {
		name           string
		imageData      []byte
		imageWidth     int
		imageHeight    int
		statusCode     int
		expectError    bool
		expectedWidth  int
		expectedHeight int
	}{
		{
			name:           "valid image",
			imageWidth:     800,
			imageHeight:    600,
			statusCode:     http.StatusOK,
			expectError:    false,
			expectedWidth:  800,
			expectedHeight: 600,
		},
		{
			name:           "large image",
			imageWidth:     1920,
			imageHeight:    1080,
			statusCode:     http.StatusOK,
			expectError:    false,
			expectedWidth:  1920,
			expectedHeight: 1080,
		},
		{
			name:        "404 not found",
			imageWidth:  0,
			imageHeight: 0,
			statusCode:  http.StatusNotFound,
			expectError: true,
		},
		{
			name:        "500 server error",
			imageWidth:  0,
			imageHeight: 0,
			statusCode:  http.StatusInternalServerError,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify headers are set correctly
				if r.Header.Get("User-Agent") == "" {
					t.Error("User-Agent header not set")
				}
				if r.Header.Get("Referer") == "" {
					t.Error("Referer header not set")
				}

				if tt.statusCode != http.StatusOK {
					w.WriteHeader(tt.statusCode)
					return
				}

				imageData := createTestJPEG(t, tt.imageWidth, tt.imageHeight)
				w.Header().Set("Content-Type", "image/jpeg")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(imageData)
			}))
			defer server.Close()

			client := &http.Client{Timeout: 5 * time.Second}
			width, height, err := GetImageDimensions(server.URL, client)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if width != tt.expectedWidth {
				t.Errorf("Width = %d, want %d", width, tt.expectedWidth)
			}

			if height != tt.expectedHeight {
				t.Errorf("Height = %d, want %d", height, tt.expectedHeight)
			}
		})
	}
}

func TestGetImageDimensions_WithNilClient(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		imageData := createTestJPEG(t, 640, 480)
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(imageData)
	}))
	defer server.Close()

	// Call with nil client - should create default client
	width, height, err := GetImageDimensions(server.URL, nil)

	if err != nil {
		t.Fatalf("Unexpected error with nil client: %v", err)
	}

	if width != 640 || height != 480 {
		t.Errorf("Dimensions = %dx%d, want 640x480", width, height)
	}
}

func TestGetImageDimensions_InvalidImage(t *testing.T) {
	// Create server that returns non-image data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	_, _, err := GetImageDimensions(server.URL, client)

	if err == nil {
		t.Error("Expected error for invalid image data, got nil")
	}
}

func TestGetImageDimensions_InvalidURL(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	_, _, err := GetImageDimensions("not-a-valid-url://invalid", client)

	if err == nil {
		t.Error("Expected error for invalid URL, got nil")
	}
}

func TestConstructAwsimgsrcPosterURL_UnknownPattern(t *testing.T) {
	// Test URL that doesn't match digital/video or mono/movie patterns
	// but has the correct ID pattern
	testCases := []struct {
		name        string
		coverURL    string
		expectedURL string
	}{
		{
			name:        "unknown path with valid ID pattern returns empty",
			coverURL:    "https://pics.dmm.co.jp/some/other/path/abc123/abc123pl.jpg",
			expectedURL: "",
		},
		{
			name:        "URL with different extension",
			coverURL:    "https://pics.dmm.co.jp/digital/video/sone00860/sone00860.png",
			expectedURL: "",
		},
		{
			name:        "URL without ID repetition - uses last ID",
			coverURL:    "https://pics.dmm.co.jp/digital/video/sone00860/differentidpl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/video/differentid/differentidps.jpg",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := constructAwsimgsrcPosterURL(tc.coverURL)
			if result != tc.expectedURL {
				t.Errorf("constructAwsimgsrcPosterURL() = %v, want %v", result, tc.expectedURL)
			}
		})
	}
}

func TestNormalizeThenConstructPosterURL(t *testing.T) {
	testCases := []struct {
		name        string
		rawCoverURL string
		expectedURL string
	}{
		{
			name:        "awsimgsrc CDN rewritten then poster constructed",
			rawCoverURL: "https://awsimgsrc.dmm.co.jp/pics_dig/digital/video/sone00860/sone00860pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/video/sone00860/sone00860ps.jpg",
		},
		{
			name:        "digital video cover produces correct poster",
			rawCoverURL: "https://pics.dmm.co.jp/digital/video/sone00860/sone00860pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/video/sone00860/sone00860ps.jpg",
		},
		{
			name:        "digital amateur cover produces correct poster",
			rawCoverURL: "https://pics.dmm.co.jp/digital/amateur/oreco183/oreco183pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/amateur/oreco183/oreco183ps.jpg",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			normalized := NormalizeDMMScreenshotURL(tc.rawCoverURL)
			result := constructAwsimgsrcPosterURL(normalized)
			if result != tc.expectedURL {
				t.Errorf("NormalizeDMMScreenshotURL(%q) → %q, constructAwsimgsrcPosterURL() = %q, want %q",
					tc.rawCoverURL, normalized, result, tc.expectedURL)
			}
		})
	}
}

// TestConstructAwsimgsrcURL_HostValidation verifies the blocker fix: only
// pics.dmm.co.jp (or already-awsimgsrc) URLs are rewritten. Non-DMM URLs
// that happen to match the path pattern must be left alone (return "").
func TestConstructAwsimgsrcURL_HostValidation(t *testing.T) {
	tests := []struct {
		name        string
		coverURL    string
		suffix      string
		expectedURL string
	}{
		{
			name:        "non-DMM host with digital video path returns empty",
			coverURL:    "https://example.com/digital/video/foo/foopl.jpg",
			suffix:      "pl.jpg",
			expectedURL: "",
		},
		{
			name:        "non-DMM host with matching pattern returns empty",
			coverURL:    "https://evil.com/awsimgsrc.dmm.com/dig/video/foo/foopl.jpg",
			suffix:      "ps.jpg",
			expectedURL: "",
		},
		{
			name:        "already-awsimgsrc host suffix swap",
			coverURL:    "https://awsimgsrc.dmm.com/dig/digital/video/ipx00535/ipx00535pl.jpg",
			suffix:      "ps.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/video/ipx00535/ipx00535ps.jpg",
		},
		{
			name:        "already-awsimgsrc ps.jpg to pl.jpg",
			coverURL:    "https://awsimgsrc.dmm.com/dig/digital/video/ipx00535/ipx00535ps.jpg",
			suffix:      "pl.jpg",
			expectedURL: "https://awsimgsrc.dmm.com/dig/digital/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name:        "awsimgsrc.dmm.co.jp host recognized",
			coverURL:    "https://awsimgsrc.dmm.co.jp/pics_dig/video/sone00860/sone00860pl.jpg",
			suffix:      "ps.jpg",
			expectedURL: "https://awsimgsrc.dmm.co.jp/pics_dig/video/sone00860/sone00860ps.jpg",
		},
		{
			name:        "pics host unknown path returns empty",
			coverURL:    "https://pics.dmm.co.jp/unknown/segment/foo/foopl.jpg",
			suffix:      "ps.jpg",
			expectedURL: "",
		},
		{
			name:        "pics host no pl.jpg suffix returns empty",
			coverURL:    "https://pics.dmm.co.jp/digital/video/foo/foops.jpg",
			suffix:      "pl.jpg",
			expectedURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructAwsimgsrcURL(tt.coverURL, tt.suffix)
			if result != tt.expectedURL {
				t.Errorf("constructAwsimgsrcURL(%q, %q) = %q, want %q",
					tt.coverURL, tt.suffix, result, tt.expectedURL)
			}
		})
	}
}

// TestConstructAwsimgsrcURL_UnparseableURL covers the url.Parse error
// branch (lines 102-104): a cover URL containing a raw control character is
// rejected by url.Parse, so constructAwsimgsrcURL must return "".
func TestConstructAwsimgsrcURL_UnparseableURL(t *testing.T) {
	// \x7f (DEL) in the host is rejected by url.Parse with
	// "invalid control character in URL".
	result := constructAwsimgsrcURL("https://exa\x7fmple.com/image.jpg", "pl.jpg")
	assert.Equal(t, "", result, "unparseable URL should return empty string")
}

// TestConstructAwsimgsrcURL_AwsimgsrcUnknownSuffix covers the default branch
// of swapDMMCoverSuffix (lines 151-152): an already-awsimgsrc URL whose
// filename ends in a suffix other than pl.jpg/ps.jpg (here jp.jpg) must be
// returned unchanged.
func TestConstructAwsimgsrcURL_AwsimgsrcUnknownSuffix(t *testing.T) {
	coverURL := "https://awsimgsrc.dmm.com/dig/digital/video/sone00560/sone00560jp.jpg"
	// swapDMMCoverSuffix hits the default case since the URL ends in jp.jpg,
	// not pl.jpg or ps.jpg; the URL is returned unchanged.
	result := constructAwsimgsrcURL(coverURL, "pl.jpg")
	assert.Equal(t, coverURL, result)
}

// TestGetImageDimensions_NewRequestError covers the http.NewRequest error
// branch (lines 165-167): a URL containing a raw space is rejected by
// http.NewRequest ("invalid character in host name"), which fails before
// client.Do is ever called.
func TestGetImageDimensions_NewRequestError(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	_, _, err := GetImageDimensions("https://exa mple.com/image.jpg", client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
