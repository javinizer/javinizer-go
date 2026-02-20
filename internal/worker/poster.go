package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	imageutil "github.com/javinizer/javinizer-go/internal/image"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// RefererResolver resolves an effective Referer for a download URL.
// This is injected by callers so poster generation does not hardcode host rules.
type RefererResolver func(downloadURL, configuredReferer string) string

// GenerateTempPoster downloads and crops a poster temporarily for the review page
// Returns the relative API URL path for the temp poster
// Updates movie.ShouldCropPoster to false since the temp image is already cropped
//
// Parameters:
//   - ctx: Context for cancellation support
//   - jobID: Batch job ID for organizing temp files by job
//   - movie: Movie model containing poster/cover URLs
//   - httpClient: Pre-configured HTTP client with proxy and timeout settings
//   - userAgent: User-Agent header value from config
//   - referer: Referer header value from config (for CDN compatibility)
//
// Returns:
//   - tempRelativeURL: API URL path like "/api/v1/temp/posters/{jobID}/{movieID}.jpg"
//   - error: Any error encountered during download or cropping
func GenerateTempPoster(
	ctx context.Context,
	jobID string,
	movie *models.Movie,
	httpClient httpclientiface.HTTPClient,
	userAgent string,
	referer string,
	refererResolver RefererResolver,
) (tempRelativeURL string, err error) {
	// Determine poster URL to download
	originalPosterURL := movie.PosterURL
	if originalPosterURL == "" {
		originalPosterURL = movie.CoverURL
	}
	if originalPosterURL == "" {
		return "", fmt.Errorf("no poster or cover URL available")
	}

	// Create temp directory: data/temp/posters/{job_id}/
	// Use DirPermTemp (0700) for owner-only access to sensitive temp files
	tempDir := filepath.Join("data", "temp", "posters", jobID)
	if err := os.MkdirAll(tempDir, config.DirPermTemp); err != nil {
		return "", fmt.Errorf("failed to create temp poster directory: %w", err)
	}

	// Define file paths
	tempFullPath := filepath.Join(tempDir, fmt.Sprintf("%s-full.jpg", movie.ID))
	tempCroppedPath := filepath.Join(tempDir, fmt.Sprintf("%s.jpg", movie.ID))

	// Download the poster
	req, err := http.NewRequestWithContext(ctx, "GET", originalPosterURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers from config (not hardcoded)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if effectiveReferer := resolvePosterReferer(originalPosterURL, referer, refererResolver); effectiveReferer != "" {
		req.Header.Set("Referer", effectiveReferer)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download poster: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poster download failed with status %d", resp.StatusCode)
	}

	// Save to temporary file using atomic write pattern with unique filename
	tmpDownload, err := os.CreateTemp(tempDir, movie.ID+"-full-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempDownloadPath := tmpDownload.Name()

	_, err = io.Copy(tmpDownload, resp.Body)
	tmpDownload.Close()
	if err != nil {
		os.Remove(tempDownloadPath)
		return "", fmt.Errorf("failed to write poster: %w", err)
	}

	// Atomic rename to final path (idempotent: remove destination first for Windows/rescrape compatibility)
	_ = os.Remove(tempFullPath) // Best-effort remove existing dest
	if err := os.Rename(tempDownloadPath, tempFullPath); err != nil {
		os.Remove(tempDownloadPath)
		return "", fmt.Errorf("failed to finalize poster download: %w", err)
	}

	// Check for cancellation before expensive cropping operation
	select {
	case <-ctx.Done():
		os.Remove(tempFullPath)
		os.Remove(tempCroppedPath)
		return "", ctx.Err()
	default:
	}

	// Crop the poster using the smart cropping algorithm
	if err := imageutil.CropPosterFromCover(afero.NewOsFs(), tempFullPath, tempCroppedPath); err != nil {
		os.Remove(tempFullPath)
		os.Remove(tempCroppedPath)
		return "", fmt.Errorf("failed to crop poster: %w", err)
	}

	// Update movie metadata to indicate poster is already cropped
	// This prevents CSS-based cropping in the frontend
	movie.ShouldCropPoster = false

	// Return API URL for frontend to fetch the poster
	tempRelativeURL = fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg", jobID, movie.ID)

	logging.Debugf("[Job %s] Created temp cropped poster at %s (original URL preserved: %s)",
		jobID, tempCroppedPath, originalPosterURL)

	return tempRelativeURL, nil
}

// GenerateCroppedPoster downloads and crops a poster, then persists it to disk
// Returns the API URL for the persisted cropped poster
// Updates movie.ShouldCropPoster to false since the image is already cropped
//
// Parameters:
//   - ctx: Context for cancellation support
//   - movie: Movie model containing poster/cover URLs
//   - httpClient: Pre-configured HTTP client with proxy and timeout settings
//   - userAgent: User-Agent header value from config
//   - referer: Referer header value from config (for CDN compatibility)
//
// Returns:
//   - croppedURL: API URL path like "/api/v1/posters/{movieID}.jpg"
//   - error: Any error encountered during download, cropping, or persistence
//
// Note: Caller is responsible for updating the database with the returned croppedURL
func GenerateCroppedPoster(
	ctx context.Context,
	movie *models.Movie,
	httpClient httpclientiface.HTTPClient,
	userAgent string,
	referer string,
	refererResolver RefererResolver,
) (croppedURL string, err error) {
	// Determine poster URL to download
	originalPosterURL := movie.PosterURL
	if originalPosterURL == "" {
		originalPosterURL = movie.CoverURL
	}
	if originalPosterURL == "" {
		return "", fmt.Errorf("no poster or cover URL available")
	}

	// Create persistent directory: data/posters/
	// Use DirPermConfig (0755) for publicly accessible static files
	posterDir := filepath.Join("data", "posters")
	if err := os.MkdirAll(posterDir, config.DirPermConfig); err != nil {
		return "", fmt.Errorf("failed to create poster directory: %w", err)
	}

	// Define cropped file path
	croppedPath := filepath.Join(posterDir, fmt.Sprintf("%s.jpg", movie.ID))

	// Download the poster
	req, err := http.NewRequestWithContext(ctx, "GET", originalPosterURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers from config (not hardcoded)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if effectiveReferer := resolvePosterReferer(originalPosterURL, referer, refererResolver); effectiveReferer != "" {
		req.Header.Set("Referer", effectiveReferer)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download poster: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poster download failed with status %d", resp.StatusCode)
	}

	// Save to temporary file using atomic write pattern with unique filename
	tmpDownload, err := os.CreateTemp(posterDir, movie.ID+"-full-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempDownloadPath := tmpDownload.Name()

	_, err = io.Copy(tmpDownload, resp.Body)
	tmpDownload.Close()
	if err != nil {
		os.Remove(tempDownloadPath)
		return "", fmt.Errorf("failed to write poster: %w", err)
	}

	// Check for cancellation before expensive cropping operation
	select {
	case <-ctx.Done():
		os.Remove(tempDownloadPath)
		os.Remove(croppedPath)
		return "", ctx.Err()
	default:
	}

	// Crop the poster using the smart cropping algorithm
	// Crop to temp file first for atomic operation with unique filename
	tmpCropped, err := os.CreateTemp(posterDir, movie.ID+"-cropped-*.tmp")
	if err != nil {
		os.Remove(tempDownloadPath)
		return "", fmt.Errorf("failed to create temp cropped file: %w", err)
	}
	tempCroppedPath := tmpCropped.Name()
	tmpCropped.Close() // Close immediately as CropPosterFromCover will create the file

	if err := imageutil.CropPosterFromCover(afero.NewOsFs(), tempDownloadPath, tempCroppedPath); err != nil {
		os.Remove(tempDownloadPath)
		os.Remove(tempCroppedPath)
		return "", fmt.Errorf("failed to crop poster: %w", err)
	}

	// Atomic rename to final destination (idempotent: remove destination first for Windows/rescrape compatibility)
	_ = os.Remove(croppedPath) // Best-effort remove existing dest
	if err := os.Rename(tempCroppedPath, croppedPath); err != nil {
		os.Remove(tempDownloadPath)
		os.Remove(tempCroppedPath)
		return "", fmt.Errorf("failed to rename cropped poster: %w", err)
	}

	// Clean up the full image after successful crop
	os.Remove(tempDownloadPath)

	// Set predictable permissions for static file serving
	if err := os.Chmod(croppedPath, 0644); err != nil {
		logging.Warnf("Failed to set poster permissions: %v", err)
	}

	// Update movie metadata to indicate poster is already cropped
	movie.ShouldCropPoster = false

	// Return API URL for frontend to fetch the poster
	croppedURL = fmt.Sprintf("/api/v1/posters/%s.jpg", movie.ID)

	logging.Debugf("[Movie %s] Created persisted cropped poster at %s (original URL: %s)",
		movie.ID, croppedPath, originalPosterURL)

	return croppedURL, nil
}

// resolvePosterReferer resolves Referer via injected hook first, then falls back
// to configured/origin behavior.
func resolvePosterReferer(downloadURL, configuredReferer string, resolver RefererResolver) string {
	if resolver != nil {
		if injected := strings.TrimSpace(resolver(downloadURL, configuredReferer)); injected != "" {
			return injected
		}
	}

	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return configuredReferer
	}

	if configuredReferer != "" {
		return configuredReferer
	}

	if (parsedURL.Scheme == "http" || parsedURL.Scheme == "https") && parsedURL.Host != "" {
		return parsedURL.Scheme + "://" + parsedURL.Host + "/"
	}

	return ""
}
