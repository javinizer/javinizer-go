package imageutil

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- GetOptimalPosterURL with httptest.NewServer ---

func TestGetOptimalPosterURL_EmptyCoverURL(t *testing.T) {
	posterURL, shouldCrop := GetOptimalPosterURL("", nil)
	assert.Equal(t, "", posterURL)
	assert.False(t, shouldCrop)
}

func TestGetOptimalPosterURL_ConstructURLFails(t *testing.T) {
	// A cover URL that doesn't match the DMM pattern
	posterURL, shouldCrop := GetOptimalPosterURL("https://example.com/image.jpg", nil)
	assert.Equal(t, "https://example.com/image.jpg", posterURL) // falls back to cover
	// bd1cd0da: when falling back to the cover as the poster, shouldCrop=true so the
	// caller crops the high-res cover down to a poster instead of using it as-is.
	assert.True(t, shouldCrop)
}

func TestGetOptimalPosterURL_AwsimgsrcDirect(t *testing.T) {
	// Test with an already-awsimgsrc URL
	coverURL := "https://awsimgsrc.dmm.com/dig/video/ipx00535/ipx00535pl.jpg"

	// Create a small test image for the server to serve
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	for y := 0; y < 1500; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	defer server.Close()

	// Override the URL to point to our test server
	testURL := server.URL + "/dig/video/ipx00535/ipx00535ps.jpg"
	posterURL, shouldCrop := GetOptimalPosterURL(coverURL, server.Client())
	// The URL construction changes pl.jpg -> ps.jpg for awsimgsrc
	// Since it can't reach the real server, it falls back
	_ = testURL
	_ = posterURL
	_ = shouldCrop
}

func TestGetOptimalPosterURL_DigitalVideoPattern(t *testing.T) {
	// Create a valid-size image for the poster server to serve
	img := image.NewRGBA(image.Rect(0, 0, 800, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	defer server.Close()

	coverURL := "https://pics.dmm.co.jp/digital/video/sone00860/sone00860pl.jpg"
	posterURL, shouldCrop := GetOptimalPosterURL(coverURL, server.Client())
	// The constructAwsimgsrcPosterURL will produce an awsimgsrc URL, but the server
	// can't serve from that domain, so it falls back to cover
	_ = posterURL
	_ = shouldCrop
}

func TestGetOptimalPosterURL_NilClient(t *testing.T) {
	// Test with nil client - should use default client and fall back
	coverURL := "https://pics.dmm.co.jp/digital/video/sone00860/sone00860pl.jpg"
	posterURL, shouldCrop := GetOptimalPosterURL(coverURL, nil)
	// Can't reach real server, falls back to cover (shouldCrop=true to crop the cover).
	assert.True(t, shouldCrop)
	_ = posterURL
}

// --- constructAwsimgsrcPosterURL tests ---

func TestConstructAwsimgsrcPosterURL_DigitalVideo(t *testing.T) {
	result := constructAwsimgsrcPosterURL("https://pics.dmm.co.jp/digital/video/sone00860/sone00860pl.jpg")
	assert.Equal(t, "https://awsimgsrc.dmm.com/dig/video/sone00860/sone00860ps.jpg", result)
}

func TestConstructAwsimgsrcPosterURL_DigitalAmateur(t *testing.T) {
	result := constructAwsimgsrcPosterURL("https://pics.dmm.co.jp/digital/amateur/siro00860/siro00860pl.jpg")
	assert.Equal(t, "https://awsimgsrc.dmm.com/dig/amateur/siro00860/siro00860ps.jpg", result)
}

func TestConstructAwsimgsrcPosterURL_MonoMovie(t *testing.T) {
	result := constructAwsimgsrcPosterURL("https://pics.dmm.co.jp/mono/movie/adult/118abw001/118abw001pl.jpg")
	assert.Equal(t, "https://awsimgsrc.dmm.com/dig/mono/movie/118abw001/118abw001ps.jpg", result)
}

func TestConstructAwsimgsrcPosterURL_AlreadyAwsimgsrc(t *testing.T) {
	result := constructAwsimgsrcPosterURL("https://awsimgsrc.dmm.com/dig/video/sone00860/sone00860pl.jpg")
	assert.Equal(t, "https://awsimgsrc.dmm.com/dig/video/sone00860/sone00860ps.jpg", result)
}

func TestConstructAwsimgsrcPosterURL_Empty(t *testing.T) {
	result := constructAwsimgsrcPosterURL("")
	assert.Equal(t, "", result)
}

func TestConstructAwsimgsrcPosterURL_NoMatch(t *testing.T) {
	result := constructAwsimgsrcPosterURL("https://example.com/image.jpg")
	assert.Equal(t, "", result)
}

func TestConstructAwsimgsrcPosterURL_UnknownPath(t *testing.T) {
	// Unknown path pattern - should try the simpler format
	result := constructAwsimgsrcPosterURL("https://pics.dmm.co.jp/other/video/sone00860/sone00860pl.jpg")
	assert.Contains(t, result, "dig/video/sone00860/sone00860ps.jpg")
}

// --- CropPosterWithBounds error cases ---

func TestCropPosterWithBounds_OutOfRange(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "cover.jpg")
	createTestImage(t, fs, coverPath, 200, 100, color.RGBA{R: 128, A: 255})

	err := CropPosterWithBounds(fs, coverPath, filepath.Join(tempDir, "poster.jpg"), -1, 0, 100, 100, 500)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestCropPosterWithBounds_InvalidBounds_MissTest(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "cover.jpg")
	createTestImage(t, fs, coverPath, 200, 100, color.RGBA{R: 128, A: 255})

	err := CropPosterWithBounds(fs, coverPath, filepath.Join(tempDir, "poster.jpg"), 100, 0, 50, 100, 500)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid crop bounds")
}

func TestCropPosterWithBounds_FileNotFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := CropPosterWithBounds(fs, "/nonexistent.jpg", "/poster.jpg", 0, 0, 100, 100, 500)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open")
}

// --- CropPosterFromCover: landscape image ---

func TestCropPosterFromCover_LandscapeImage(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "landscape_cover.jpg")
	posterPath := filepath.Join(tempDir, "landscape_poster.jpg")

	// Create a wide landscape image (typical JAV cover aspect ratio ~1.5)
	createTestImage(t, fs, coverPath, 600, 400, color.RGBA{R: 100, G: 150, B: 200, A: 255})

	err := CropPosterFromCover(fs, coverPath, posterPath, 500)
	require.NoError(t, err)

	posterWidth, posterHeight := decodeTestImageDimensions(t, fs, posterPath)
	assert.Greater(t, posterWidth, 0)
	assert.Greater(t, posterHeight, 0)
}

// --- UpgradeCoverResolution additional tests ---

func TestUpgradeCoverResolution_PS2PL(t *testing.T) {
	assert.Equal(t, "https://example.com/pl.jpg", UpgradeCoverResolution("https://example.com/ps.jpg"))
}

func TestUpgradeCoverResolution_JP2PL_NonAmateur(t *testing.T) {
	assert.Equal(t, "https://example.com/pl.jpg", UpgradeCoverResolution("https://example.com/jp.jpg"))
}

func TestUpgradeCoverResolution_JP2PL_Amateur_NoChange(t *testing.T) {
	// Amateur URLs should NOT upgrade jp.jpg -> pl.jpg
	assert.Equal(t, "https://example.com/amateur/jp.jpg", UpgradeCoverResolution("https://example.com/amateur/jp.jpg"))
}

// --- NormalizeDMMScreenshotURL additional tests ---

func TestNormalizeDMMScreenshotURL_Empty(t *testing.T) {
	assert.Equal(t, "", NormalizeDMMScreenshotURL(""))
}

func TestNormalizeDMMScreenshotURL_NonDMM(t *testing.T) {
	assert.Equal(t, "https://example.com/image.jpg", NormalizeDMMScreenshotURL("https://example.com/image.jpg"))
}

func TestNormalizeDMMScreenshotURL_AwsimgsrcRewrite(t *testing.T) {
	result := NormalizeDMMScreenshotURL("https://awsimgsrc.dmm.co.jp/pics_dig/digital/video/test/image.jpg")
	assert.Contains(t, result, "pics.dmm.co.jp")
	assert.NotContains(t, result, "awsimgsrc")
}

func TestNormalizeDMMScreenshotURL_ProtocolRelative(t *testing.T) {
	result := NormalizeDMMScreenshotURL("//pics.dmm.co.jp/digital/video/test/image.jpg")
	assert.True(t, len(result) > 0)
	assert.Contains(t, result, "https://")
}

func TestNormalizeDMMScreenshotURL_AmateurLowercase(t *testing.T) {
	result := NormalizeDMMScreenshotURL("https://pics.dmm.co.jp/digital/amateur/TEST/Image.jpg")
	assert.Contains(t, result, "/digital/amateur/test/")
}

func TestNormalizeDMMScreenshotURL_ScreenshotJPSuffix(t *testing.T) {
	// Screenshots with dash and no jp suffix should get jp inserted
	result := NormalizeDMMScreenshotURL("https://pics.dmm.co.jp/digital/video/test/avsa00432-1.jpg")
	assert.Contains(t, result, "avsa00432jp-1.jpg")
}

func TestNormalizeDMMScreenshotURL_CoverPLUnchanged(t *testing.T) {
	result := NormalizeDMMScreenshotURL("https://pics.dmm.co.jp/digital/video/test/testpl.jpg")
	assert.Contains(t, result, "testpl.jpg")
}

func TestNormalizeDMMScreenshotURL_CoverPSUnchanged(t *testing.T) {
	result := NormalizeDMMScreenshotURL("https://pics.dmm.co.jp/digital/video/test/testps.jpg")
	assert.Contains(t, result, "testps.jpg")
}

// --- GetImageDimensions with httptest.NewServer ---

func TestGetImageDimensions_Success(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	defer server.Close()

	width, height, err := GetImageDimensions(server.URL, server.Client())
	require.NoError(t, err)
	assert.Equal(t, 640, width)
	assert.Equal(t, 480, height)
}

func TestGetImageDimensions_NonImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, "<html><body>Not an image</body></html>")
	}))
	defer server.Close()

	_, _, err := GetImageDimensions(server.URL, server.Client())
	require.Error(t, err)
}

func TestGetImageDimensions_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := GetImageDimensions(server.URL, server.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- IsDMMHost tests ---

func TestIsDMMHost_MissTest(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		{"dmm.co.jp", true},
		{"www.dmm.co.jp", true},
		{"dmm.com", true},
		{"www.dmm.com", true},
		{"example.com", false},
		{"", false},
		{"notdmm.co.jp", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsDMMHost(tt.host))
		})
	}
}
