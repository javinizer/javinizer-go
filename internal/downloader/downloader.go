package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	imageutil "github.com/javinizer/javinizer-go/internal/image"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Downloader handles media file downloads
type Downloader struct {
	config     *config.OutputConfig
	httpClient *http.Client
	userAgent  string
}

// DownloadResult represents the result of a download operation
type DownloadResult struct {
	URL        string
	LocalPath  string
	Size       int64
	Downloaded bool
	Error      error
	Type       MediaType
	Duration   time.Duration
}

// MediaType represents the type of media being downloaded
type MediaType string

const (
	MediaTypeCover       MediaType = "cover"
	MediaTypePoster      MediaType = "poster"
	MediaTypeExtrafanart MediaType = "extrafanart"
	MediaTypeTrailer     MediaType = "trailer"
	MediaTypeActress     MediaType = "actress"
)

// NewDownloader creates a new media downloader
func NewDownloader(cfg *config.OutputConfig, userAgent string) *Downloader {
	// Use configured timeout, default to 60 seconds if not set
	timeout := cfg.DownloadTimeout
	if timeout <= 0 {
		timeout = 60
	}

	return &Downloader{
		config: cfg,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
				MaxIdleConnsPerHost: 2,
			},
		},
		userAgent: userAgent,
	}
}

// generateFilename generates a filename using the configured template
func (d *Downloader) generateFilename(movie *models.Movie, templateStr string, index int) string {
	if templateStr == "" {
		return ""
	}

	ctx := template.NewContextFromMovie(movie)
	ctx.Index = index // Set index for screenshot numbering

	engine := template.NewEngine()
	filename, err := engine.Execute(templateStr, ctx)
	if err != nil {
		// Fallback to ID-based naming if template fails
		return fmt.Sprintf("%s-unknown", ctx.ID)
	}

	return filename
}

// SetDownloadExtrafanart sets whether extrafanart downloads are enabled
func (d *Downloader) SetDownloadExtrafanart(enabled bool) {
	d.config.DownloadExtrafanart = enabled
}

// DownloadCover downloads the movie cover image (fanart)
func (d *Downloader) DownloadCover(movie *models.Movie, destDir string) (*DownloadResult, error) {
	if !d.config.DownloadCover || movie.CoverURL == "" {
		return &DownloadResult{Type: MediaTypeCover, Downloaded: false}, nil
	}

	filename := d.generateFilename(movie, d.config.FanartFormat, 0)
	if filename == "" {
		// Fallback to hardcoded format
		filename = fmt.Sprintf("%s-fanart.jpg", movie.ID)
	}
	destPath := filepath.Join(destDir, filename)

	return d.download(movie.CoverURL, destPath, MediaTypeCover)
}

// DownloadPoster downloads and crops the movie poster
// The poster is created by cropping the right 47.2% of the cover image
// This matches the original Javinizer's behavior
func (d *Downloader) DownloadPoster(movie *models.Movie, destDir string) (*DownloadResult, error) {
	if !d.config.DownloadPoster {
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	// Use PosterURL if available, otherwise fall back to CoverURL
	posterURL := movie.PosterURL
	if posterURL == "" {
		posterURL = movie.CoverURL
	}
	if posterURL == "" {
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	filename := d.generateFilename(movie, d.config.PosterFormat, 0)
	if filename == "" {
		// Fallback to hardcoded format
		filename = fmt.Sprintf("%s-poster.jpg", movie.ID)
	}
	destPath := filepath.Join(destDir, filename)

	// Check if poster already exists
	if _, err := os.Stat(destPath); err == nil {
		// Already exists
		info, _ := os.Stat(destPath)
		return &DownloadResult{
			Type:       MediaTypePoster,
			LocalPath:  destPath,
			Size:       info.Size(),
			Downloaded: false,
		}, nil
	}

	// Download the source image to a temporary location
	tempPath := destPath + ".full.tmp"
	result, err := d.download(posterURL, tempPath, MediaTypePoster)
	if err != nil || !result.Downloaded {
		os.Remove(tempPath) // Clean up if exists
		return result, err
	}

	// Crop the poster from the downloaded image
	if err := imageutil.CropPosterFromCover(tempPath, destPath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		result.Error = fmt.Errorf("failed to crop poster: %w", err)
		result.Downloaded = false
		return result, result.Error
	}

	// Clean up the temporary full image
	os.Remove(tempPath)

	// Update result with final path and size
	if info, err := os.Stat(destPath); err == nil {
		result.LocalPath = destPath
		result.Size = info.Size()
	}

	return result, nil
}

// DownloadExtrafanart downloads screenshots to the extrafanart subdirectory
// Extrafanart is used by media centers like Kodi/Plex for background images
// Note: In the original Javinizer, screenshots and extrafanart are the same thing
func (d *Downloader) DownloadExtrafanart(movie *models.Movie, destDir string) ([]DownloadResult, error) {
	if !d.config.DownloadExtrafanart || len(movie.Screenshots) == 0 {
		return []DownloadResult{}, nil
	}

	// Create extrafanart subdirectory using configurable folder name
	extrafanartDir := filepath.Join(destDir, d.config.ScreenshotFolder)

	results := make([]DownloadResult, 0, len(movie.Screenshots))

	for i, url := range movie.Screenshots {
		// Use configurable screenshot format with index for numbering
		filename := d.generateFilename(movie, d.config.ScreenshotFormat, i+1)
		if filename == "" {
			// Fallback to hardcoded format with configurable padding
			if d.config.ScreenshotPadding > 0 {
				filename = fmt.Sprintf("fanart%0*d.jpg", d.config.ScreenshotPadding, i+1)
			} else {
				filename = fmt.Sprintf("fanart%d.jpg", i+1)
			}
		}
		destPath := filepath.Join(extrafanartDir, filename)

		result, err := d.download(url, destPath, MediaTypeExtrafanart)
		if err != nil {
			result = &DownloadResult{
				URL:   url,
				Type:  MediaTypeExtrafanart,
				Error: err,
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

// DownloadTrailer downloads the movie trailer
func (d *Downloader) DownloadTrailer(movie *models.Movie, destDir string) (*DownloadResult, error) {
	if !d.config.DownloadTrailer || movie.TrailerURL == "" {
		return &DownloadResult{Type: MediaTypeTrailer, Downloaded: false}, nil
	}

	// Determine extension from URL
	ext := filepath.Ext(movie.TrailerURL)
	if ext == "" {
		ext = ".mp4" // Default to mp4
	}

	filename := d.generateFilename(movie, d.config.TrailerFormat, 0)
	if filename == "" {
		// Fallback to hardcoded format
		filename = fmt.Sprintf("%s-trailer%s", movie.ID, ext)
	} else {
		// Ensure template filename has the correct extension
		if filepath.Ext(filename) == "" {
			filename += ext
		}
	}
	destPath := filepath.Join(destDir, filename)

	return d.download(movie.TrailerURL, destPath, MediaTypeTrailer)
}

// DownloadActressImages downloads actress thumbnail images
func (d *Downloader) DownloadActressImages(movie *models.Movie, destDir string) ([]DownloadResult, error) {
	if !d.config.DownloadActress || len(movie.Actresses) == 0 {
		return []DownloadResult{}, nil
	}

	// Create actress subdirectory using configurable folder name
	actressDir := filepath.Join(destDir, d.config.ActressFolder)

	results := make([]DownloadResult, 0)

	for _, actress := range movie.Actresses {
		if actress.ThumbURL == "" {
			continue
		}

		// Use configurable template for actress filenames
		// Create a temporary movie with actress data for template processing
		actressMovie := &models.Movie{
			ID:    movie.ID,
			Title: actress.FullName(),
		}

		filename := d.generateFilename(actressMovie, "actress-<ACTORNAME>.jpg", 0)
		if filename == "" {
			// Fallback to hardcoded format
			name := template.SanitizeFilename(actress.FullName())
			filename = fmt.Sprintf("actress-%s.jpg", name)
		}
		destPath := filepath.Join(actressDir, filename)

		result, err := d.download(actress.ThumbURL, destPath, MediaTypeActress)
		if err != nil {
			result = &DownloadResult{
				URL:   actress.ThumbURL,
				Type:  MediaTypeActress,
				Error: err,
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

// DownloadAll downloads all enabled media types for a movie
// partNumber: 0 = single file or first part, 1+ = subsequent parts
// For multi-part files, only downloads shared media (cover/poster/trailer/actresses) for part 0 or 1
func (d *Downloader) DownloadAll(movie *models.Movie, destDir string, partNumber int) ([]DownloadResult, error) {
	results := make([]DownloadResult, 0)

	// Download shared media only for single files (partNumber == 0) or first part (partNumber == 1)
	shouldDownloadShared := partNumber == 0 || partNumber == 1

	if shouldDownloadShared {
		// Download cover
		if coverResult, err := d.DownloadCover(movie, destDir); err == nil && coverResult != nil {
			results = append(results, *coverResult)
		}

		// Download poster
		if posterResult, err := d.DownloadPoster(movie, destDir); err == nil && posterResult != nil {
			results = append(results, *posterResult)
		}

		// Download extrafanart (screenshots)
		if extrafanart, err := d.DownloadExtrafanart(movie, destDir); err == nil {
			results = append(results, extrafanart...)
		}

		// Download trailer
		if trailerResult, err := d.DownloadTrailer(movie, destDir); err == nil && trailerResult != nil {
			results = append(results, *trailerResult)
		}

		// Download actress images
		if actresses, err := d.DownloadActressImages(movie, destDir); err == nil {
			results = append(results, actresses...)
		}
	}

	return results, nil
}

// download performs the actual HTTP download
func (d *Downloader) download(url, destPath string, mediaType MediaType) (*DownloadResult, error) {
	startTime := time.Now()

	result := &DownloadResult{
		URL:        url,
		LocalPath:  destPath,
		Type:       mediaType,
		Downloaded: false,
	}

	// Check if file already exists
	if info, err := os.Stat(destPath); err == nil {
		result.Size = info.Size()
		result.Downloaded = false // Already exists, not downloaded
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Create destination directory
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Set user agent
	if d.userAgent != "" {
		req.Header.Set("User-Agent", d.userAgent)
	}

	// Execute request
	resp, err := d.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to download: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Errorf("bad status code: %d", resp.StatusCode)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Create temporary file
	tempPath := destPath + ".tmp"
	outFile, err := os.Create(tempPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to create file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Download to temp file
	written, err := io.Copy(outFile, resp.Body)
	outFile.Close()

	if err != nil {
		os.Remove(tempPath)
		result.Error = fmt.Errorf("failed to write file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Rename temp file to final destination
	if err := os.Rename(tempPath, destPath); err != nil {
		os.Remove(tempPath)
		result.Error = fmt.Errorf("failed to rename file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Size = written
	result.Downloaded = true
	result.Duration = time.Since(startTime)

	return result, nil
}

// GetImageExtension determines the image extension from a URL
func GetImageExtension(url string) string {
	url = strings.ToLower(url)

	// Check common image extensions
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp"} {
		if strings.Contains(url, ext) {
			return ext
		}
	}

	// Default to jpg
	return ".jpg"
}

// CleanupPartialDownloads removes .tmp files from a directory
func CleanupPartialDownloads(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.HasSuffix(entry.Name(), ".tmp") {
			path := filepath.Join(dir, entry.Name())
			os.Remove(path) // Ignore errors
		}
	}

	return nil
}
