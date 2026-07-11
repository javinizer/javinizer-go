package imageutil

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// MinPosterWidth and MinPosterHeight are the minimum pixel dimensions for a
// usable portrait poster. Real DMM awsimgsrc posters (ps.jpg) range from
// ~714x972 (e.g. MIHD-001) up to ~1032x1469, so the floor is set below the
// smallest observed real poster with margin. Placeholders / thumbnails served
// for missing titles are well under this (and usually 404 anyway), so they are
// still rejected in favor of cropping the high-res cover.
const (
	MinPosterWidth = 600

	MinPosterHeight = 800

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
// (ps.jpg). It is a thin wrapper around constructAwsimgsrcURL.
//
// Example: https://pics.dmm.co.jp/digital/video/sone00860/sone00860pl.jpg
//
//	-> https://awsimgsrc.dmm.com/dig/digital/video/sone00860/sone00860ps.jpg
//
// Example: https://pics.dmm.co.jp/mono/movie/adult/118abw001/118abw001pl.jpg
//
//	-> https://awsimgsrc.dmm.com/dig/mono/movie/118abw001/118abw001ps.jpg
func constructAwsimgsrcPosterURL(coverURL string) string {
	return constructAwsimgsrcURL(coverURL, "ps.jpg")
}

// constructAwsimgsrcURL converts a pics.dmm.co.jp (or already-awsimgsrc)
// cover URL to its awsimgsrc.dmm.com equivalent using the requested filename
// suffix (e.g. "ps.jpg" for posters, "pl.jpg" for high-res covers). Returns
// "" if coverURL does not match a known DMM cover pattern.
//
// Path mapping (verified against the live CDN):
//   - digital/video/{id}/{id}pl.jpg    -> dig/digital/video/{id}/{id}{suffix}
//   - digital/amateur/{id}/{id}pl.jpg  -> dig/digital/amateur/{id}/{id}{suffix}
//   - mono/movie/adult/{id}/{id}pl.jpg -> dig/mono/movie/{id}/{id}{suffix}
//
// When the input is already on awsimgsrc.dmm.com / awsimgsrc.dmm.co.jp, only
// the filename suffix is swapped.
func constructAwsimgsrcURL(coverURL, suffix string) string {
	if coverURL == "" {
		return ""
	}

	u, err := url.Parse(coverURL)
	if err != nil {
		return ""
	}

	if isAwsimgsrcHost(u.Hostname()) {
		return swapDMMCoverSuffix(coverURL, suffix)
	}

	if u.Hostname() != picsDMMHost {
		return ""
	}

	re := regexp.MustCompile(`/([\w\d]+)/([\w\d]+)pl\.jpg$`)
	matches := re.FindStringSubmatch(coverURL)
	if len(matches) < 3 {
		return ""
	}

	contentID := matches[2]

	var awsimgsrcPath string
	switch {
	case strings.Contains(coverURL, "/digital/video/"):
		awsimgsrcPath = fmt.Sprintf("dig/digital/video/%s/%s%s", contentID, contentID, suffix)
	case strings.Contains(coverURL, "/digital/amateur/"):
		awsimgsrcPath = fmt.Sprintf("dig/digital/amateur/%s/%s%s", contentID, contentID, suffix)
	case strings.Contains(coverURL, "/mono/movie/"):
		awsimgsrcPath = fmt.Sprintf("dig/mono/movie/%s/%s%s", contentID, contentID, suffix)
	default:
		return ""
	}

	return fmt.Sprintf("https://awsimgsrc.dmm.com/%s", awsimgsrcPath)
}

func isAwsimgsrcHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "awsimgsrc.dmm.com" || host == "awsimgsrc.dmm.co.jp"
}

// swapDMMCoverSuffix replaces the trailing cover/poster suffix of a DMM
// image URL with the requested suffix. Recognized source suffixes are
// pl.jpg and ps.jpg; URLs ending in anything else are returned unchanged.
func swapDMMCoverSuffix(url, suffix string) string {
	switch {
	case strings.HasSuffix(url, "pl.jpg"):
		return url[:len(url)-len("pl.jpg")] + suffix
	case strings.HasSuffix(url, "ps.jpg"):
		return url[:len(url)-len("ps.jpg")] + suffix
	default:
		return url
	}
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
