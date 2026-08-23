package temp

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
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
// @Failure 404 {object} contracts.ErrorResponse
func serveTempPoster(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiCfg := rt.GetAPIConfig()
		tempCfg := apiCfg.TempConfig()
		deps := rt.Deps()

		jobID := c.Param("jobId")
		filename := c.Param("filename")

		// Validate both jobID and filename to prevent path traversal attacks.
		// Reject "."/".." and any path separators — filepath.Base("..") == "..",
		// so the prior base-name check alone let jobID=".." resolve posters/..
		// to the temp root and serve sibling files.
		if !isSafePathSegment(jobID) || !isSafePathSegment(filename) {
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
		tempDir := tempCfg.TempDir
		if deps.JobStore != nil {
			if job, ok := deps.JobStore.GetJob(jobID); ok && job.TempDir != "" {
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

		// Read once and serve those exact bytes. Hashing the path and then
		// reopening it through c.File leaves a replacement window where the
		// identity headers can describe a different poster than the response body.
		body, err := os.ReadFile(posterPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}
		c.Header("Content-Type", "image/jpeg")
		writeImageBody(c, body)
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
// @Failure 404 {object} contracts.ErrorResponse
func serveCroppedPoster() gin.HandlerFunc {
	return func(c *gin.Context) {
		filename := c.Param("filename")

		// Validate filename to prevent path traversal attacks.
		// Reject "."/".." and path separators in addition to requiring .jpg.
		if !isSafePathSegment(filename) || !strings.HasSuffix(strings.ToLower(filename), ".jpg") {
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
// When server-side image caching is enabled (system.image_cache_enabled), fetched images are persisted
// to disk under {temp_dir}/image-cache/ so they remain available when the source is unreachable.
// Stale-if-error: an expired entry whose re-fetch fails is served from disk rather than erroring.
// @Router /api/v1/temp/image [get]
// @Summary Proxy remote images
// @Description Proxies remote images for preview UI, handling hotlink protection and CORS issues. When image caching is enabled, fetched images are persisted server-side with stale-if-error semantics.
// @Tags temp
// @Param url query string true "Image URL"
// @Success 200 {file} binary
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 403 {object} contracts.ErrorResponse
// @Failure 502 {object} contracts.ErrorResponse
func serveTempImage(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiCfg := rt.GetAPIConfig()
		tempCfg := apiCfg.TempConfig()

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

		cacheEnabled := tempCfg.ImageCacheEnabled && tempCfg.ImageCacheTTLHours > 0
		if !cacheEnabled {
			serveTempImageUncached(c, tempCfg, parsedURL.String(), rawURL)
			return
		}

		fs := rt.Deps().GetFs()
		cacheDir := tempCfg.TempDir
		ttl := time.Duration(tempCfg.ImageCacheTTLHours) * time.Hour

		fetchURL := parsedURL.String()
		keyURL := *parsedURL
		keyURL.Fragment = ""
		normalizedURL := keyURL.String()
		file, contentType, remaining, state := get(fs, cacheDir, normalizedURL, ttl)
		if state == CacheFresh {
			defer func() { _ = file.Close() }()
			body, rerr := io.ReadAll(file)
			if rerr != nil {
				c.AbortWithStatus(http.StatusBadGateway)
				return
			}
			c.Header("Content-Type", contentType)
			c.Header("Cache-Control", cacheControlForTTL(remaining))
			c.Header("X-Content-Type-Options", "nosniff")
			writeImageBody(c, body)
			return
		}

		var staleFile afero.File
		var staleCT string
		if state == CacheStale {
			staleFile = file
			staleCT = contentType
		}
		if staleFile != nil {
			defer func() { _ = staleFile.Close() }()
		}

		if err := ssrf.CheckURL(rawURL); err != nil {
			if staleFile != nil && serveStaleFile(c, staleFile, staleCT) {
				return
			}
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}

		userAgent := tempCfg.ScraperUserAgent
		if userAgent == "" {
			userAgent = config.DefaultUserAgent
		}
		referer := resolveTempImageReferer(fetchURL, tempCfg.ScraperReferer)
		httpClient := ssrf.NewSSRFSafeClient(60 * time.Second)

		v, ferr, _ := imageCacheGroup.Do(normalizedURL, func() (any, error) {
			res := fetchAndCache(c.Request.Context(), fs, cacheDir, normalizedURL, fetchURL, httpClient, userAgent, referer, tempCfg.ImageCacheMaxSizeMB)
			if res.persistFailed && len(res.body) == 0 {
				body, mediaType, berr := fetchBodyToMemory(c.Request.Context(), httpClient, fetchURL, userAgent, referer)
				if berr == nil {
					res.body = body
					res.contentType = mediaType
				}
			}
			return res, nil
		})
		result := v.(fetchResult)
		_ = ferr

		if result.err != nil {
			if result.persistFailed && len(result.body) > 0 {
				c.Header("Cache-Control", "private, max-age=300")
				c.Header("X-Content-Type-Options", "nosniff")
				c.Data(http.StatusOK, result.contentType, result.body)
				return
			}
			if staleFile != nil && serveStaleFile(c, staleFile, staleCT) {
				return
			}
			if result.persistFailed {
				logging.Warnf("image cache: persist failed for %s: %v", redactImageURL(normalizedURL), result.err)
				c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch image"})
				return
			}
			logging.Warnf("image cache: fetch failed for %s: %v", redactImageURL(normalizedURL), result.err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch image"})
			return
		}

		if result.cachedPath != "" {
			c.Header("Content-Type", result.contentType)
			c.Header("Cache-Control", cacheControlForTTL(ttl))
			c.Header("X-Content-Type-Options", "nosniff")
			cachedFile, openErr := fs.Open(result.cachedPath)
			if openErr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "failed to open cached image"})
				return
			}
			body, rerr := io.ReadAll(cachedFile)
			_ = cachedFile.Close()
			if rerr != nil {
				c.AbortWithStatus(http.StatusBadGateway)
				return
			}
			writeImageBody(c, body)
			return
		}

		if len(result.body) > 0 {
			c.Header("Content-Type", result.contentType)
			c.Header("Cache-Control", "private, max-age=300")
			c.Header("X-Content-Type-Options", "nosniff")
			writeImageBody(c, result.body)
		}
	}
}

func serveStaleFile(c *gin.Context, f afero.File, contentType string) bool {
	body, err := io.ReadAll(f)
	if err != nil {
		logging.Warnf("image cache: failed to serve stale entry: %v", err)
		return false
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	writeImageBody(c, body)
	return true
}

func writeImageBody(c *gin.Context, body []byte) {
	assetidentity.SetHeaders(c.Writer, assetidentity.FromBytes(body))
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write(body); err != nil {
		c.AbortWithStatus(http.StatusBadGateway)
	}
}

func serveTempImageUncached(c *gin.Context, tempCfg *core.TempNarrowConfig, downloadURL, rawURL string) {
	if err := ssrf.CheckURL(rawURL); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	httpClient := ssrf.NewSSRFSafeClient(60 * time.Second)

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, downloadURL, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to create request"})
		return
	}

	userAgent := tempCfg.ScraperUserAgent
	if userAgent == "" {
		userAgent = config.DefaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if referer := resolveTempImageReferer(downloadURL, tempCfg.ScraperReferer); referer != "" {
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
		contentType = defaultContentType
	}

	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxImageProxyResponseSize))
	if len(body) > 0 {
		// Preserve the historical partial-body behavior when the upstream
		// Content-Length is wrong: io.Copy used to leave bytes in the recorder
		// before reporting unexpected EOF.
		writeImageBody(c, body)
	}
	if readErr != nil && len(body) == 0 {
		c.AbortWithStatus(http.StatusBadGateway)
	}
}

// resolveTempImageReferer selects a compatible Referer for preview image proxy requests.
func resolveTempImageReferer(downloadURL, configuredReferer string) string {
	return httpclient.ResolveMediaReferer(downloadURL, configuredReferer)
}

// isSafePathSegment reports whether s is a single safe path segment: non-empty,
// not "." or "..", and containing no path separators (os.PathSeparator or '/').
// filepath.Base alone is insufficient because filepath.Base("..") == "..",
// which would let a jobID/filename of ".." escape its intended directory.
func isSafePathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, "/"+string(os.PathSeparator)+"\\") {
		return false
	}
	return s == filepath.Base(s)
}

func cacheControlForTTL(remaining time.Duration) string {
	maxAge := int64(remaining.Seconds())
	if maxAge > 86400 {
		maxAge = 86400
	}
	return fmt.Sprintf("private, max-age=%d", maxAge)
}

func redactImageURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	return u.String()
}
