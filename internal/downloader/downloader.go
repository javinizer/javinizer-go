package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	imageutil "github.com/javinizer/javinizer-go/internal/image"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// Downloader handles media file downloads
type Downloader struct {
	fs                  afero.Fs
	config              *config.OutputConfig
	httpClient          httpclient.HTTPClient
	userAgent           string
	actorJapaneseNames  bool // Use Japanese names for actress files
	actorFirstNameOrder bool // true = FirstName LastName, false = LastName FirstName
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

// MultipartInfo holds multipart file information for template rendering
type MultipartInfo struct {
	IsMultiPart bool   // Whether this is a multi-part file
	PartNumber  int    // Part number (1, 2, 3, etc.) - 0 means single file
	PartSuffix  string // Original part suffix detected from filename (e.g., "-pt1", "-A")
}

// NewHTTPClientForDownloader creates an HTTP client for production use with proxy and timeout configuration
func NewHTTPClientForDownloader(cfg *config.Config) (httpclient.HTTPClient, error) {
	return NewHTTPClientForDownloaderWithRegistry(cfg, nil)
}

// NewHTTPClientForDownloaderWithRegistry creates an HTTP client for production use
// with proxy and timeout configuration. When a scraper registry is provided,
// scraper-specific media host proxy routing is injected from scraper implementations.
func NewHTTPClientForDownloaderWithRegistry(cfg *config.Config, registry *models.ScraperRegistry) (httpclient.HTTPClient, error) {
	outputCfg := &cfg.Output
	// Use configured timeout, default to 60 seconds if not set
	timeout := outputCfg.DownloadTimeout
	if timeout <= 0 {
		timeout = 60
	}

	timeoutDuration := time.Duration(timeout) * time.Second

	adaptiveClient := &adaptiveDownloaderHTTPClient{
		timeout:        timeoutDuration,
		cfg:            cfg,
		clients:        make(map[string]httpclient.HTTPClient),
		proxyResolvers: collectDownloadProxyResolvers(cfg, registry),
	}

	// Explicit download proxy override still takes precedence when configured.
	if outputCfg.DownloadProxy.Enabled {
		resolvedDownloadProxy := config.ResolveScraperProxy(cfg.Scrapers.Proxy, &outputCfg.DownloadProxy)
		if resolvedDownloadProxy != nil && resolvedDownloadProxy.URL != "" {
			client, err := httpclient.NewHTTPClient(resolvedDownloadProxy, timeoutDuration)
			if err != nil {
				logging.Errorf("Downloader: Failed to create download proxy client: %v, using adaptive routing", err)
			} else {
				logging.Infof("Downloader: Using download proxy %s", httpclient.SanitizeProxyURL(resolvedDownloadProxy.URL))
				adaptiveClient.forceClient = client
				return adaptiveClient, nil
			}
		}
	}

	// Default direct client
	directClient, err := httpclient.NewHTTPClient(nil, timeoutDuration)
	if err != nil {
		logging.Errorf("Downloader: Failed to create direct HTTP client: %v, using standard http client", err)
		directClient = &http.Client{
			Timeout: timeoutDuration,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
				MaxIdleConnsPerHost: 2,
			},
		}
	}
	adaptiveClient.directClient = directClient

	return adaptiveClient, nil
}

func collectDownloadProxyResolvers(cfg *config.Config, registry *models.ScraperRegistry) []models.ScraperDownloadProxyResolver {
	if cfg == nil || registry == nil {
		return nil
	}

	resolvers := make([]models.ScraperDownloadProxyResolver, 0)
	seen := make(map[string]struct{})
	add := func(scraper models.Scraper) {
		if scraper == nil {
			return
		}
		name := scraper.Name()
		if _, ok := seen[name]; ok {
			return
		}
		resolver, ok := scraper.(models.ScraperDownloadProxyResolver)
		if !ok || resolver == nil {
			return
		}
		resolvers = append(resolvers, resolver)
		seen[name] = struct{}{}
	}

	for _, name := range cfg.Scrapers.Priority {
		scraper, exists := registry.Get(name)
		if exists {
			add(scraper)
		}
	}

	remaining := make([]string, 0)
	for _, scraper := range registry.GetAll() {
		if scraper == nil {
			continue
		}
		if _, ok := seen[scraper.Name()]; ok {
			continue
		}
		remaining = append(remaining, scraper.Name())
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		scraper, exists := registry.Get(name)
		if exists {
			add(scraper)
		}
	}

	return resolvers
}

// adaptiveDownloaderHTTPClient routes media downloads through per-scraper proxies when needed.
type adaptiveDownloaderHTTPClient struct {
	timeout        time.Duration
	cfg            *config.Config
	forceClient    httpclient.HTTPClient // forced proxy client for all downloads
	directClient   httpclient.HTTPClient
	proxyResolvers []models.ScraperDownloadProxyResolver
	mu             sync.Mutex
	clients        map[string]httpclient.HTTPClient // keyed by proxy fingerprint
}

func (c *adaptiveDownloaderHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// If a force client is configured, always use it.
	if c.forceClient != nil {
		return c.forceClient.Do(req)
	}

	proxyCfg := c.selectProxyForRequest(req)
	if proxyCfg == nil || !proxyCfg.Enabled || proxyCfg.URL == "" {
		return c.directClient.Do(req)
	}

	client, err := c.getOrCreateProxyClient(proxyCfg)
	if err != nil {
		logging.Warnf("Downloader: Failed to create proxy client for %s: %v; falling back to direct", req.URL.Host, err)
		return c.directClient.Do(req)
	}

	return client.Do(req)
}

func (c *adaptiveDownloaderHTTPClient) selectProxyForRequest(req *http.Request) *config.ProxyConfig {
	if req == nil || req.URL == nil || c.cfg == nil {
		return nil
	}

	host := strings.ToLower(req.URL.Hostname())
	if host == "" {
		return nil
	}

	for _, resolver := range c.proxyResolvers {
		downloadOverride, scraperProxy, handled := resolver.ResolveDownloadProxyForHost(host)
		if handled {
			return c.resolveScraperDownloadProxy(downloadOverride, scraperProxy)
		}
	}

	// Fallback to global scraper proxy, if enabled.
	resolvedGlobalProxy := config.ResolveGlobalProxy(c.cfg.Scrapers.Proxy)
	if resolvedGlobalProxy != nil && resolvedGlobalProxy.Enabled && resolvedGlobalProxy.URL != "" {
		return resolvedGlobalProxy
	}
	return nil
}

func (c *adaptiveDownloaderHTTPClient) resolveScraperDownloadProxy(downloadOverride, scraperProxy *config.ProxyConfig) *config.ProxyConfig {
	// Scraper-level download proxy configuration:
	// - When download_proxy.enabled=true, apply configured override.
	// - Legacy compatibility: profile/url/use_main_proxy without enabled=true is still treated as override.
	// - Otherwise, inherit scraper request proxy for downloads.
	if hasConfiguredDownloadOverride(downloadOverride) {
		overrideEnabled := downloadOverride.Enabled || hasLegacyDownloadOverrideFields(downloadOverride)
		if !overrideEnabled {
			resolved := config.ResolveScraperProxy(c.cfg.Scrapers.Proxy, scraperProxy)
			if resolved != nil && resolved.Enabled && resolved.URL != "" {
				return resolved
			}
			return nil
		}

		override := *downloadOverride
		override.Enabled = true

		if override.UseMainProxy {
			resolved := config.ResolveScraperProxy(c.cfg.Scrapers.Proxy, scraperProxy)
			if resolved != nil && resolved.Enabled && resolved.URL != "" {
				return resolved
			}
			return nil
		}

		resolved := config.ResolveScraperProxy(c.cfg.Scrapers.Proxy, &override)
		if resolved != nil && resolved.Enabled && resolved.URL != "" {
			return resolved
		}
		return nil
	}

	// Backward-compatible fallback: scraper request proxy also applies to downloads.
	resolved := config.ResolveScraperProxy(c.cfg.Scrapers.Proxy, scraperProxy)
	if resolved != nil && resolved.Enabled && resolved.URL != "" {
		return resolved
	}
	return nil
}

func hasConfiguredDownloadOverride(downloadOverride *config.ProxyConfig) bool {
	if downloadOverride == nil {
		return false
	}
	return downloadOverride.UseMainProxy ||
		downloadOverride.Profile != "" ||
		downloadOverride.URL != "" ||
		downloadOverride.Username != "" ||
		downloadOverride.Password != "" ||
		downloadOverride.Enabled
}

func hasLegacyDownloadOverrideFields(downloadOverride *config.ProxyConfig) bool {
	if downloadOverride == nil {
		return false
	}
	return downloadOverride.UseMainProxy ||
		downloadOverride.Profile != "" ||
		downloadOverride.URL != "" ||
		downloadOverride.Username != "" ||
		downloadOverride.Password != ""
}

func (c *adaptiveDownloaderHTTPClient) getOrCreateProxyClient(proxyCfg *config.ProxyConfig) (httpclient.HTTPClient, error) {
	key := fmt.Sprintf("%s|%s|%s", proxyCfg.URL, proxyCfg.Username, proxyCfg.Password)

	c.mu.Lock()
	defer c.mu.Unlock()

	if client, ok := c.clients[key]; ok {
		return client, nil
	}

	client, err := httpclient.NewHTTPClient(proxyCfg, c.timeout)
	if err != nil {
		return nil, err
	}
	c.clients[key] = client
	logging.Infof("Downloader: Using scraper-level proxy for media host via %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	return client, nil
}

// NewDownloader creates a new media downloader
func NewDownloader(client httpclient.HTTPClient, fs afero.Fs, cfg *config.OutputConfig, userAgent string) *Downloader {
	return &Downloader{
		fs:                  fs,
		config:              cfg,
		httpClient:          client,
		userAgent:           userAgent,
		actorJapaneseNames:  false, // Default: use English names
		actorFirstNameOrder: true,  // Default: FirstName LastName
	}
}

// NewDownloaderWithNFOConfig creates a new media downloader with NFO config for actress name formatting
func NewDownloaderWithNFOConfig(client httpclient.HTTPClient, fs afero.Fs, cfg *config.OutputConfig, userAgent string, actorJapaneseNames, actorFirstNameOrder bool) *Downloader {
	d := NewDownloader(client, fs, cfg, userAgent)
	d.actorJapaneseNames = actorJapaneseNames
	d.actorFirstNameOrder = actorFirstNameOrder
	return d
}

// generateFilename generates a filename using the configured template
func (d *Downloader) generateFilename(movie *models.Movie, templateStr string, index int, multipart *MultipartInfo) string {
	if templateStr == "" {
		return ""
	}

	ctx := template.NewContextFromMovie(movie)
	ctx.Index = index // Set index for screenshot numbering
	ctx.GroupActress = d.config.GroupActress

	// Set multipart info if provided
	if multipart != nil {
		ctx.IsMultiPart = multipart.IsMultiPart
		ctx.PartNumber = multipart.PartNumber
		ctx.PartSuffix = multipart.PartSuffix
	}

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
func (d *Downloader) DownloadCover(movie *models.Movie, destDir string, multipart *MultipartInfo) (*DownloadResult, error) {
	if !d.config.DownloadCover || movie.CoverURL == "" {
		return &DownloadResult{Type: MediaTypeCover, Downloaded: false}, nil
	}

	filename := d.generateFilename(movie, d.config.FanartFormat, 0, multipart)
	if filename == "" {
		// Fallback to hardcoded format
		filename = fmt.Sprintf("%s-fanart.jpg", movie.ID)
	}
	destPath := filepath.Join(destDir, filename)

	return d.download(movie.CoverURL, destPath, MediaTypeCover)
}

// DownloadPoster downloads the movie poster
// If ShouldCropPoster is true, the poster is created by cropping the right 47.2% of the cover image
// If ShouldCropPoster is false, the poster is downloaded directly without cropping (high-quality poster)
func (d *Downloader) DownloadPoster(movie *models.Movie, destDir string, multipart *MultipartInfo) (*DownloadResult, error) {
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

	filename := d.generateFilename(movie, d.config.PosterFormat, 0, multipart)
	if filename == "" {
		// Fallback to hardcoded format
		filename = fmt.Sprintf("%s-poster.jpg", movie.ID)
	}
	destPath := filepath.Join(destDir, filename)

	// Check if poster already exists
	if _, err := d.fs.Stat(destPath); err == nil {
		// Already exists
		info, _ := d.fs.Stat(destPath)
		return &DownloadResult{
			Type:       MediaTypePoster,
			LocalPath:  destPath,
			Size:       info.Size(),
			Downloaded: false,
		}, nil
	}

	// Check if we need to crop the poster or use it directly
	if !movie.ShouldCropPoster {
		// High-quality poster - download directly without cropping
		result, err := d.download(posterURL, destPath, MediaTypePoster)
		return result, err
	}

	// Low-quality poster - download and crop from cover
	tempPath := destPath + ".full.tmp"
	result, err := d.download(posterURL, tempPath, MediaTypePoster)
	if err != nil || !result.Downloaded {
		_ = d.fs.Remove(tempPath) // Clean up if exists
		return result, err
	}

	// Crop the poster from the downloaded image
	if err := imageutil.CropPosterFromCover(d.fs, tempPath, destPath); err != nil {
		_ = d.fs.Remove(tempPath) // Clean up temp file
		result.Error = fmt.Errorf("failed to crop poster: %w", err)
		result.Downloaded = false
		return result, result.Error
	}

	// Clean up the temporary full image
	_ = d.fs.Remove(tempPath)

	// Update result with final path and size
	if info, err := d.fs.Stat(destPath); err == nil {
		result.LocalPath = destPath
		result.Size = info.Size()
	}

	return result, nil
}

// DownloadExtrafanart downloads screenshots to the extrafanart subdirectory
// Extrafanart is used by media centers like Kodi/Plex for background images
// Note: In the original Javinizer, screenshots and extrafanart are the same thing
func (d *Downloader) DownloadExtrafanart(movie *models.Movie, destDir string, multipart *MultipartInfo) ([]DownloadResult, error) {
	if !d.config.DownloadExtrafanart || len(movie.Screenshots) == 0 {
		return []DownloadResult{}, nil
	}

	// Create extrafanart subdirectory using configurable folder name
	extrafanartDir := filepath.Join(destDir, d.config.ScreenshotFolder)

	results := make([]DownloadResult, 0, len(movie.Screenshots))

	for i, url := range movie.Screenshots {
		// Use configurable screenshot format with index for numbering
		filename := d.generateFilename(movie, d.config.ScreenshotFormat, i+1, multipart)
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
func (d *Downloader) DownloadTrailer(movie *models.Movie, destDir string, multipart *MultipartInfo) (*DownloadResult, error) {
	if !d.config.DownloadTrailer || movie.TrailerURL == "" {
		return &DownloadResult{Type: MediaTypeTrailer, Downloaded: false}, nil
	}

	// Determine extension from URL
	ext := filepath.Ext(movie.TrailerURL)
	if ext == "" {
		ext = ".mp4" // Default to mp4
	}

	filename := d.generateFilename(movie, d.config.TrailerFormat, 0, multipart)
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

// formatActressName formats an actress name according to NFO settings
// This mirrors the logic in the NFO generator to ensure consistency
func (d *Downloader) formatActressName(actress models.Actress) string {
	// Use Japanese name if configured and available
	if d.actorJapaneseNames && actress.JapaneseName != "" {
		return actress.JapaneseName
	}

	// Format based on name order preference
	firstName := actress.FirstName
	lastName := actress.LastName

	if firstName != "" && lastName != "" {
		if d.actorFirstNameOrder {
			// FirstName LastName
			return firstName + " " + lastName
		}
		// LastName FirstName
		return lastName + " " + firstName
	}

	// Single name only
	if firstName != "" {
		return firstName
	}
	if lastName != "" {
		return lastName
	}

	// Fallback to Japanese name if English names are empty
	if actress.JapaneseName != "" {
		return actress.JapaneseName
	}

	// Last resort: use FullName() which tries all options
	return actress.FullName()
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

		// Format actress name according to NFO settings (Japanese vs English)
		formattedName := d.formatActressName(actress)

		// Use configurable template for actress filenames
		// Create a temporary movie with actress data for template processing
		actressMovie := &models.Movie{
			ID:    movie.ID,
			Title: formattedName,
		}

		filename := d.generateFilename(actressMovie, d.config.ActressFormat, 0, nil)
		if filename == "" {
			// Fallback to default format
			name := template.SanitizeFilename(formattedName)
			filename = fmt.Sprintf("%s.jpg", name)
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
// multipart: nil for single files, or MultipartInfo for multi-part files
// Each download method checks if the file already exists (file-exists deduplication).
// Templates without multipart placeholders produce the same filename for all parts,
// so subsequent parts will skip re-downloading (Downloaded=false).
// Templates with <IF:MULTIPART> or <PART> produce different filenames, so each part
// gets its own file. Actress images are only downloaded for single files or first part.
func (d *Downloader) DownloadAll(movie *models.Movie, destDir string, multipart *MultipartInfo) ([]DownloadResult, error) {
	results := make([]DownloadResult, 0)

	// Download cover (fanart)
	// Note: Each download method has a file-exists check, so if templates produce
	// the same filename for different parts, the file won't be re-downloaded.
	// If templates use <IF:MULTIPART> or <PART>, each part gets its own file.
	if coverResult, err := d.DownloadCover(movie, destDir, multipart); err == nil && coverResult != nil {
		results = append(results, *coverResult)
	}

	// Download poster
	if posterResult, err := d.DownloadPoster(movie, destDir, multipart); err == nil && posterResult != nil {
		results = append(results, *posterResult)
	}

	// Download extrafanart (screenshots)
	if extrafanart, err := d.DownloadExtrafanart(movie, destDir, multipart); err == nil {
		results = append(results, extrafanart...)
	}

	// Download trailer
	if trailerResult, err := d.DownloadTrailer(movie, destDir, multipart); err == nil && trailerResult != nil {
		results = append(results, *trailerResult)
	}

	// Download actress images (doesn't use multipart - shared across all parts)
	// Only download for single files or first part to avoid duplicate downloads
	partNumber := 0
	if multipart != nil {
		partNumber = multipart.PartNumber
	}
	if partNumber == 0 || partNumber == 1 {
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
	if info, err := d.fs.Stat(destPath); err == nil {
		result.Size = info.Size()
		result.Downloaded = false // Already exists, not downloaded
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Create destination directory
	destDir := filepath.Dir(destPath)
	if err := d.fs.MkdirAll(destDir, 0777); err != nil {
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
	if referer := resolveDownloadReferer(url); referer != "" {
		req.Header.Set("Referer", referer)
	}

	// Execute request
	resp, err := d.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to download: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Errorf("bad status code: %d", resp.StatusCode)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Create temporary file
	tempPath := destPath + ".tmp"
	outFile, err := d.fs.Create(tempPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to create file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Download to temp file
	written, err := io.Copy(outFile, resp.Body)
	closeErr := outFile.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}

	if err != nil {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("failed to write file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Rename temp file to final destination
	if err := d.fs.Rename(tempPath, destPath); err != nil {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("failed to rename file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Size = written
	result.Downloaded = true
	result.Duration = time.Since(startTime)

	return result, nil
}

// DownloadWithRetry downloads a file with exponential backoff retry logic for transient errors
// It retries on HTTP 503, 500, 429 and network errors, but fails immediately on 404, 403, 401, 400
// Exponential backoff formula: delay = min(100ms * 2^(retryAttempt-1), 10s) where retryAttempt starts at 1
// Context cancellation is respected during backoff delays and HTTP requests
func (d *Downloader) DownloadWithRetry(ctx context.Context, url, destPath string, maxRetries int) error {
	const (
		initialDelay = 100 * time.Millisecond
		maxDelay     = 10 * time.Second
	)

	// Treat negative maxRetries as 0 (only initial attempt, no retries)
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	totalAttempts := maxRetries + 1 // Initial attempt + retries

	for attempt := 0; attempt < totalAttempts; attempt++ {
		// Check context cancellation before each attempt
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Attempt download
		err := d.downloadSimple(ctx, url, destPath)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			// Non-retryable error (404, 403, 401, 400) - fail immediately
			return fmt.Errorf("download failed after %d attempt(s): %s returned %w", attempt+1, url, err)
		}

		// If this was the last attempt, don't sleep - just return error
		if attempt == totalAttempts-1 {
			break
		}

		// Calculate exponential backoff delay: 100ms * 2^(retryAttempt-1)
		// Attempt 0 = initial (no delay before it)
		// Attempt 1 = first retry: 100ms * 2^0 = 100ms
		// Attempt 2 = second retry: 100ms * 2^1 = 200ms
		// Attempt 3 = third retry: 100ms * 2^2 = 400ms
		retryAttempt := attempt + 1 // Convert to 1-indexed for formula
		delay := initialDelay * time.Duration(1<<uint(retryAttempt-1))
		if delay > maxDelay {
			delay = maxDelay
		}

		// Sleep with context cancellation support
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Continue to next retry
		}
	}

	// All retries exhausted
	return fmt.Errorf("download failed after %d attempt(s): %s returned %w", totalAttempts, url, lastErr)
}

// downloadSimple is a simplified download helper that returns just an error (not *DownloadResult)
// This is used by DownloadWithRetry for cleaner retry logic
func (d *Downloader) downloadSimple(ctx context.Context, url, destPath string) error {
	// Validate URL scheme (only http/https allowed)
	if err := validateURLScheme(url); err != nil {
		return err
	}

	// Check if file already exists
	if _, err := d.fs.Stat(destPath); err == nil {
		return nil // File exists, skip download
	}

	// Create destination directory
	destDir := filepath.Dir(destPath)
	if err := d.fs.MkdirAll(destDir, 0777); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent
	if d.userAgent != "" {
		req.Header.Set("User-Agent", d.userAgent)
	}
	if referer := resolveDownloadReferer(url); referer != "" {
		req.Header.Set("Referer", referer)
	}

	// Execute request
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code and return status error
	if resp.StatusCode != http.StatusOK {
		return &statusError{statusCode: resp.StatusCode}
	}

	// Create temporary file
	tempPath := destPath + ".tmp"
	outFile, err := d.fs.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	// Download to temp file
	_, err = io.Copy(outFile, resp.Body)
	closeErr := outFile.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}

	if err != nil {
		_ = d.fs.Remove(tempPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Rename temp file to final destination
	if err := d.fs.Rename(tempPath, destPath); err != nil {
		_ = d.fs.Remove(tempPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

// statusError represents an HTTP status code error
type statusError struct {
	statusCode int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("HTTP %d", e.statusCode)
}

// isRetryableError determines if an error is retryable (503, 500, 429, network errors)
// Returns false for non-retryable errors (404, 403, 401, 400)
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a statusError
	var sErr *statusError
	if statusErr, ok := err.(*statusError); ok {
		sErr = statusErr
	} else {
		// Try to find statusError in wrapped errors
		for e := err; e != nil; {
			if se, ok := e.(*statusError); ok {
				sErr = se
				break
			}
			// Try to unwrap
			if unwrapper, ok := e.(interface{ Unwrap() error }); ok {
				e = unwrapper.Unwrap()
			} else {
				break
			}
		}
	}

	if sErr != nil {
		switch sErr.statusCode {
		case http.StatusServiceUnavailable, // 503
			http.StatusInternalServerError, // 500
			http.StatusTooManyRequests:     // 429
			return true // Retryable
		case http.StatusNotFound, // 404
			http.StatusForbidden,    // 403
			http.StatusUnauthorized, // 401
			http.StatusBadRequest:   // 400
			return false // Non-retryable
		default:
			// Other status codes: treat as non-retryable by default
			return false
		}
	}

	// Network errors (connection refused, DNS failures, etc.) are retryable
	// Check for common network error strings
	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "i/o timeout") {
		return true
	}

	// Default: non-retryable
	return false
}

// validateURLScheme checks if the URL uses http or https scheme
func validateURLScheme(urlStr string) error {
	parsedURL, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsedURL.URL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme '%s': only http and https are allowed", scheme)
	}

	return nil
}

// ResolveMediaReferer selects a compatible Referer header for media requests.
// Priority:
// 1) Known host overrides (hotlink-protected hosts)
// 2) Configured referer fallback (if provided)
// 3) URL origin fallback
func ResolveMediaReferer(downloadURL, configuredReferer string) string {
	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return configuredReferer
	}

	host := strings.ToLower(parsedURL.Hostname())
	switch {
	case strings.HasSuffix(host, "jdbstatic.com"), strings.HasSuffix(host, "javdb.com"):
		return "https://javdb.com/"
	case strings.HasSuffix(host, "javbus.com"), strings.HasSuffix(host, "javbus.org"):
		return "https://www.javbus.com/"
	case strings.HasSuffix(host, "aventertainments.com"):
		return "https://www.aventertainments.com/"
	case strings.HasSuffix(host, "caribbeancom.com"):
		return "https://www.caribbeancom.com/"
	case strings.HasSuffix(host, "libredmm.com"):
		return "https://www.libredmm.com/"
	case strings.HasSuffix(host, "dmm.co.jp"), strings.HasSuffix(host, "dmm.com"), strings.Contains(host, ".dmm."):
		return "https://www.dmm.co.jp/"
	}

	if configuredReferer != "" {
		return configuredReferer
	}

	if (parsedURL.Scheme == "http" || parsedURL.Scheme == "https") && parsedURL.Host != "" {
		return parsedURL.Scheme + "://" + parsedURL.Host + "/"
	}

	return ""
}

// resolveDownloadReferer selects a compatible Referer header for media downloads.
func resolveDownloadReferer(downloadURL string) string {
	return ResolveMediaReferer(downloadURL, "")
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
func CleanupPartialDownloads(fs afero.Fs, dir string) error {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.HasSuffix(entry.Name(), ".tmp") {
			path := filepath.Join(dir, entry.Name())
			_ = fs.Remove(path) // Ignore errors
		}
	}

	return nil
}
