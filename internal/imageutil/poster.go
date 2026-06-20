package imageutil

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const (
	MinPosterWidth = 800

	MinPosterHeight = 1000

	maxDimensionReadBytes = 256 * 1024
)

// GetOptimalPosterURL attempts to find the highest quality poster URL.
// It tries the awsimgsrc URL first, checks its resolution, and falls back to
// the cover image when no high-quality portrait poster is available.
//
// When a high-quality portrait poster is found (>= MinPosterWidth x
// MinPosterHeight), it is returned with shouldCrop=false so the downloader
// downloads it directly.
//
// When no high-quality portrait poster is available (awsimgsrc 404, too
// small, or unconstructable), the cover URL is returned with
// shouldCrop=true. The downloader then crops the cover into a portrait
// poster. Returning shouldCrop=false here would cause the downloader to
// save the landscape cover unchanged as poster.jpg (see issue #31).
func GetOptimalPosterURL(coverURL string, client *http.Client) (posterURL string, shouldCrop bool) {
	if coverURL == "" {
		return "", false
	}

	// Extract the content ID and construct awsimgsrc poster URL
	awsimgsrcPosterURL := constructAwsimgsrcPosterURL(coverURL)
	if awsimgsrcPosterURL == "" {
		logging.Debug("ImageUtil: Could not construct awsimgsrc poster URL, will crop cover")
		return coverURL, true
	}

	// Check the resolution of the awsimgsrc poster
	width, height, err := GetImageDimensions(awsimgsrcPosterURL, client)
	if err != nil {
		logging.Debugf("ImageUtil: Failed to check awsimgsrc poster dimensions: %v, will crop cover", err)
		return coverURL, true
	}

	// Check if the poster meets quality requirements
	if width >= MinPosterWidth && height >= MinPosterHeight {
		logging.Debugf("ImageUtil: Using high-quality awsimgsrc poster (%dx%d): %s", width, height, awsimgsrcPosterURL)
		return awsimgsrcPosterURL, false
	}

	logging.Debugf("ImageUtil: Awsimgsrc poster too small (%dx%d), will crop cover", width, height)
	return coverURL, true
}

// constructAwsimgsrcPosterURL converts a cover URL to an awsimgsrc poster URL
// Example: https://pics.dmm.co.jp/digital/video/sone00860/sone00860pl.jpg
//
//	-> https://awsimgsrc.dmm.com/dig/video/sone00860/sone00860ps.jpg
//
// Example: https://pics.dmm.co.jp/mono/movie/adult/118abw001/118abw001pl.jpg
//
//	-> https://awsimgsrc.dmm.com/dig/mono/movie/118abw001/118abw001ps.jpg
func constructAwsimgsrcPosterURL(coverURL string) string {
	if coverURL == "" {
		return ""
	}

	// Pattern 1: digital/video/[id]/[id]pl.jpg -> dig/video/[id]/[id]ps.jpg
	// Pattern 2: mono/movie/adult/[id]/[id]pl.jpg -> dig/mono/movie/[id]/[id]ps.jpg
	// Pattern 3: awsimgsrc already - just replace pl.jpg with ps.jpg

	// If it's already awsimgsrc, just replace pl.jpg with ps.jpg
	if strings.Contains(coverURL, "awsimgsrc.dmm.com") || strings.Contains(coverURL, "awsimgsrc.dmm.co.jp") {
		return strings.Replace(coverURL, "pl.jpg", "ps.jpg", 1)
	}

	// Extract the content ID from the URL
	// Pattern: .../[id]/[id]pl.jpg
	re := regexp.MustCompile(`/([\w\d]+)/([\w\d]+)pl\.jpg$`)
	matches := re.FindStringSubmatch(coverURL)
	if len(matches) < 3 {
		return ""
	}

	contentID := matches[2] // The ID part (e.g., sone00860, 118abw001)

	// Determine the path structure
	var awsimgsrcPath string
	if strings.Contains(coverURL, "/digital/video/") {
		// Digital video pattern: dig/video/[id]/[id]ps.jpg
		awsimgsrcPath = fmt.Sprintf("dig/video/%s/%sps.jpg", contentID, contentID)
	} else if strings.Contains(coverURL, "/digital/amateur/") {
		// Amateur pattern: dig/amateur/[id]/[id]ps.jpg
		awsimgsrcPath = fmt.Sprintf("dig/amateur/%s/%sps.jpg", contentID, contentID)
	} else if strings.Contains(coverURL, "/mono/movie/") {
		// Mono movie pattern: dig/mono/movie/[id]/[id]ps.jpg
		awsimgsrcPath = fmt.Sprintf("dig/mono/movie/%s/%sps.jpg", contentID, contentID)
	} else {
		// Unknown pattern, try the simpler format
		awsimgsrcPath = fmt.Sprintf("dig/video/%s/%sps.jpg", contentID, contentID)
	}

	return fmt.Sprintf("https://awsimgsrc.dmm.com/%s", awsimgsrcPath)
}

// GetImageDimensions fetches an image and returns its dimensions
func GetImageDimensions(url string, client *http.Client) (width, height int, err error) {
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers to mimic a browser request
	req.Header.Set("User-Agent", "Javinizer (+https://github.com/javinizer/javinizer-go)")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Referer", "https://www.dmm.co.jp/")

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch image: %w", err)
	}
	defer func() { _ = httpclient.DrainAndClose(resp.Body) }()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("image not found (status %d)", resp.StatusCode)
	}

	lr := io.LimitReader(resp.Body, maxDimensionReadBytes)
	img, _, err := image.DecodeConfig(lr)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode image: %w", err)
	}

	return img.Width, img.Height, nil
}
