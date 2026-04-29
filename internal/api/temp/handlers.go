package temp

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/ssrf"
)

// serveTempPoster serves temporarily cropped posters from the configured temp directory.
// These are created during batch scraping for preview in the review page.
// @Router /api/v1/temp/posters/{jobId}/{filename} [get]
// @Summary Serve temporary poster image
// @Description Serves temporarily cropped posters from batch jobs. These are ephemeral and preserved when organization fails for retry.
// @Tags temp
// @Param jobId path string true "Job ID"
// @Param filename path string true "Filename"
// @Success 200 {file} binary
// @Failure 404 {object} ErrorResponse
func serveTempPoster(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := deps.GetConfig()
		if cfg == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server configuration unavailable"})
			return
		}

		jobID := c.Param("jobId")
		filename := c.Param("filename")

		// Validate both jobID and filename to prevent path traversal attacks
		// Only allow values without path separators
		if jobID != filepath.Base(jobID) || filename != filepath.Base(filename) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Validate filename has .jpg extension
		if !strings.HasSuffix(strings.ToLower(filename), ".jpg") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Get the job's stored TempDir for consistent path resolution
		// This ensures temp posters remain accessible even if system.temp_dir is changed
		// If job is not in memory (evicted after 24h cleanup), falls back to config TempDir
		// This is acceptable for ephemeral temp posters since they're cleaned up on organization
		tempDir := cfg.System.TempDir
		if deps.JobQueue != nil {
			if job, ok := deps.JobQueue.GetJob(jobID); ok && job.TempDir != "" {
				tempDir = job.TempDir
			}
		}

		// Construct path and verify it's within tempPosterDir
		tempPosterDir := filepath.Join(tempDir, "posters", jobID)
		posterPath := filepath.Join(tempPosterDir, filename)

		// Double-check the resolved path is still within tempPosterDir (defense in depth)
		cleanPosterPath := filepath.Clean(posterPath)
		cleanTempDir := filepath.Clean(tempPosterDir) + string(os.PathSeparator)
		if !strings.HasPrefix(cleanPosterPath, cleanTempDir) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Check if file exists and is accessible before serving
		if _, err := os.Stat(posterPath); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Serve the file (no cache headers for temp files as they're ephemeral)
		c.File(posterPath)
	}
}

// serveCroppedPoster serves persistent cropped posters from data/posters/
// These are stored in the database and persist across scraping sessions
// @Router /api/v1/posters/{filename} [get]
// @Summary Serve cropped poster image
// @Description Serves persistent cropped posters from the database. These persist across scraping sessions.
// @Tags temp
// @Param filename path string true "Filename"
// @Success 200 {file} binary
// @Failure 404 {object} ErrorResponse
func serveCroppedPoster() gin.HandlerFunc {
	return func(c *gin.Context) {
		filename := c.Param("filename")

		// Validate filename to prevent path traversal attacks
		// Only allow filenames without path separators and with .jpg extension
		if filename != filepath.Base(filename) || !strings.HasSuffix(strings.ToLower(filename), ".jpg") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Construct path and verify it's within posterDir
		posterDir := filepath.Join("data", "posters")
		posterPath := filepath.Join(posterDir, filename)

		// Double-check the resolved path is still within posterDir (defense in depth)
		cleanPosterPath := filepath.Clean(posterPath)
		cleanPosterDir := filepath.Clean(posterDir) + string(os.PathSeparator)
		if !strings.HasPrefix(cleanPosterPath, cleanPosterDir) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Check if file exists and is accessible before serving
		if _, err := os.Stat(posterPath); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Set cache headers for better performance
		c.Header("Cache-Control", "public, max-age=86400")
		c.File(posterPath)
	}
}

// serveTempImage proxies remote images for preview UI.
// This is used for hotlink-protected sources (e.g., JavBus) where direct browser loads may return 403.
// @Router /api/v1/temp/image [get]
// @Summary Proxy remote images
// @Description Proxies remote images for preview UI, handling hotlink protection and CORS issues.
// @Tags temp
// @Param url query string true "Image URL"
// @Success 200 {file} binary
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
func serveTempImage(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := deps.GetConfig()
		if cfg == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server configuration unavailable"})
			return
		}

		rawURL := strings.TrimSpace(c.Query("url"))
		if rawURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "url query parameter is required"})
			return
		}

		parsedURL, err := url.Parse(rawURL)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid image url"})
			return
		}

		if err := ssrf.CheckURL(rawURL); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}

		httpClient := ssrf.NewSSRFSafeClient(60 * time.Second)

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, parsedURL.String(), nil)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to create request"})
			return
		}

		userAgent := cfg.Scrapers.UserAgent
		if userAgent == "" {
			userAgent = config.DefaultUserAgent
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
		if referer := resolveTempImageReferer(parsedURL.String(), cfg.Scrapers.Referer); referer != "" {
			req.Header.Set("Referer", referer)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch image"})
			return
		}
		defer func() {
			_ = httpclient.DrainAndClose(resp.Body)
		}()

		if resp.StatusCode != http.StatusOK {
			c.JSON(http.StatusBadGateway, gin.H{"error": "image source returned non-200 status"})
			return
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "image/jpeg"
		}

		c.Header("Content-Type", contentType)
		c.Header("Cache-Control", "private, max-age=300")

		const maxImageProxyResponseSize = 50 * 1024 * 1024 // 50MB
		if _, err := io.Copy(c.Writer, io.LimitReader(resp.Body, maxImageProxyResponseSize)); err != nil {
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
	}
}

// resolveTempImageReferer selects a compatible Referer for preview image proxy requests.
func resolveTempImageReferer(downloadURL, configuredReferer string) string {
	return downloader.ResolveMediaReferer(downloadURL, configuredReferer)
}
