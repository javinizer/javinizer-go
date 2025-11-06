package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// serveTempPoster serves temporarily cropped posters from data/temp/posters/
// These are created during batch scraping for preview in the review page
func serveTempPoster() gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Construct path and verify it's within tempPosterDir
		tempPosterDir := filepath.Join("data", "temp", "posters", jobID)
		posterPath := filepath.Join(tempPosterDir, filename)

		// Double-check the resolved path is still within tempPosterDir (defense in depth)
		cleanPosterPath := filepath.Clean(posterPath)
		cleanTempDir := filepath.Clean(tempPosterDir) + string(os.PathSeparator)
		if !strings.HasPrefix(cleanPosterPath, cleanTempDir) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Check if file exists before serving
		if _, err := os.Stat(posterPath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Serve the file (no cache headers for temp files as they're ephemeral)
		c.File(posterPath)
	}
}

// serveCroppedPoster serves persistent cropped posters from data/posters/
// These are stored in the database and persist across scraping sessions
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

		// Check if file exists before serving
		if _, err := os.Stat(posterPath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// Set cache headers for better performance
		c.Header("Cache-Control", "public, max-age=86400")
		c.File(posterPath)
	}
}
