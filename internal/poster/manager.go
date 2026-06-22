// Package poster provides a deep module that encapsulates poster crop,
// download-from-URL, and SSRF validation logic. Callers interact through
// PosterManagerInterface so the HTTP client, filesystem, and image-crop
// details remain hidden behind a stable contract.
package poster

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/models"
	"strings"
	"time"

	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
)

// maxPosterSize is the maximum allowed poster download size (50 MB).
const maxPosterSize = 50 << 20

// cropResult holds the paths and URL produced by a crop or download
// operation.
type cropResult struct {
	// CroppedPath is the filesystem path to the cropped poster image.
	CroppedPath string
	// FullPath is the filesystem path to the full-size source image.
	FullPath string
	// CroppedURL is the URL that serves the cropped poster via the API.
	CroppedURL string
}

// PosterManagerInterface defines the contract for poster operations.
type PosterManagerInterface interface {
	// CropWithBounds re-crops a poster using explicit pixel bounds.
	// maxPosterHeight caps the output height (0 = no cap, preserve source resolution).
	CropWithBounds(ctx context.Context, jobID, posterID string, x, y, width, height, maxPosterHeight int) (*cropResult, error)
	// DownloadFromURL downloads an image from rawURL and creates both a
	// full-size and a cropped poster file.
	DownloadFromURL(ctx context.Context, jobID, posterID, rawURL, userAgent, referer string) (*cropResult, error)
}

// ssrfCheckFunc is the function signature for URL SSRF validation.
type ssrfCheckFunc func(rawURL string) error

// PosterManager implements PosterManagerInterface using an afero filesystem,
// a temporary directory, and an HTTP client.
type PosterManager struct {
	fs         afero.Fs
	tempDir    string
	httpClient httpclientiface.HTTPClient
	// ssrfCheck validates a URL for SSRF safety. Defaults to ssrf.CheckURL.
	// Override via WithSSRFCheck for testing only.
	ssrfCheck ssrfCheckFunc
}

// NewPosterManager creates a PosterManager backed by the given filesystem,
// temp directory root, and HTTP client.
func NewPosterManager(fs afero.Fs, tempDir string, httpClient httpclientiface.HTTPClient) *PosterManager {
	return &PosterManager{
		fs:         fs,
		tempDir:    tempDir,
		httpClient: httpClient,
		ssrfCheck:  ssrf.CheckURL,
	}
}

// WithSSRFCheck returns a copy of the PosterManager with the provided SSRF
// check function. Intended for tests that need to bypass SSRF validation
// (e.g. when using httptest servers on 127.0.0.1).
func (pm *PosterManager) WithSSRFCheck(fn ssrfCheckFunc) *PosterManager {
	cp := *pm
	cp.ssrfCheck = fn
	return &cp
}

// CropWithBounds re-crops a temp poster for a given job using the supplied
// pixel bounds. posterID is validated for safety (no path traversal) and
// must be a single filename component. jobID is validated similarly.
//
// NOTE: ctx is currently unused because the image crop operation does not
// support cancellation; it is accepted for forward compatibility.
func (pm *PosterManager) CropWithBounds(_ context.Context, jobID, posterID string, x, y, width, height, maxPosterHeight int) (*cropResult, error) {
	if err := ValidateJobID(jobID); err != nil {
		return nil, err
	}
	if err := validatePosterID(posterID); err != nil {
		return nil, err
	}

	tempPosterDir := filepath.Join(pm.tempDir, "posters", jobID)
	sourcePath := filepath.Join(tempPosterDir, fmt.Sprintf("%s-full.jpg", posterID))

	// Fallback for older jobs where the full image was cleaned up.
	if _, err := pm.fs.Stat(sourcePath); err != nil {
		sourcePath = filepath.Join(tempPosterDir, fmt.Sprintf("%s.jpg", posterID))
	}

	if _, err := pm.fs.Stat(sourcePath); err != nil {
		return nil, fmt.Errorf("source poster not found for manual crop: %w", err)
	}

	croppedPath := filepath.Join(tempPosterDir, fmt.Sprintf("%s.jpg", posterID))

	// Defense in depth: ensure both paths are inside tempPosterDir.
	if err := validatePathWithinDir(sourcePath, tempPosterDir); err != nil {
		return nil, err
	}
	if err := validatePathWithinDir(croppedPath, tempPosterDir); err != nil {
		return nil, err
	}

	left := x
	top := y
	right := x + width
	bottom := y + height

	if err := imageutil.CropPosterWithBounds(pm.fs, sourcePath, croppedPath, left, top, right, bottom, maxPosterHeight); err != nil {
		return nil, fmt.Errorf("crop failed: %w", err)
	}

	croppedURL := fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg?v=%d", url.PathEscape(jobID), url.PathEscape(posterID), time.Now().UnixMilli())

	return &cropResult{
		CroppedPath: croppedPath,
		FullPath:    sourcePath,
		CroppedURL:  croppedURL,
	}, nil
}

// DownloadFromURL downloads a poster image from rawURL, validates the URL
// with SSRF checks, writes the full image, and attempts an automatic crop
// (falling back to a simple copy if cropping fails).
func (pm *PosterManager) DownloadFromURL(ctx context.Context, jobID, posterID, rawURL, userAgent, referer string) (*cropResult, error) {
	if err := ValidateJobID(jobID); err != nil {
		return nil, err
	}
	if err := validatePosterID(posterID); err != nil {
		return nil, err
	}

	// SSRF mitigation: validate URL scheme and reject private/reserved IPs.
	if err := pm.ssrfCheck(rawURL); err != nil {
		return nil, fmt.Errorf("SSRF validation failed: %w", err)
	}

	tempPosterDir := filepath.Join(pm.tempDir, "posters", jobID)
	if err := pm.fs.MkdirAll(tempPosterDir, configDirPermTemp); err != nil {
		return nil, fmt.Errorf("failed to create temp poster directory: %w", err)
	}

	tempFullPath := filepath.Join(tempPosterDir, fmt.Sprintf("%s-full.jpg", posterID))
	tempCroppedPath := filepath.Join(tempPosterDir, fmt.Sprintf("%s.jpg", posterID))

	// Defense in depth: ensure paths are inside tempPosterDir.
	if err := validatePathWithinDir(tempFullPath, tempPosterDir); err != nil {
		return nil, err
	}
	if err := validatePathWithinDir(tempCroppedPath, tempPosterDir); err != nil {
		return nil, err
	}

	// Build HTTP request.
	downloadReq, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if userAgent != "" {
		downloadReq.Header.Set("User-Agent", userAgent)
	}
	downloadReq.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if referer != "" {
		downloadReq.Header.Set("Referer", referer)
	} else if parsed, parseErr := url.Parse(rawURL); parseErr == nil && parsed.Host != "" {
		downloadReq.Header.Set("Referer", parsed.Scheme+"://"+parsed.Host+"/")
	}

	resp, err := pm.httpClient.Do(downloadReq)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	defer func() { _ = drainAndClose(resp.Body) }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download failed with status %d", resp.StatusCode)
	}

	// Write to a temp file first, then rename to the final path.
	tmpFile, err := afero.TempFile(pm.fs, tempPosterDir, posterID+"-full-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempDownloadPath := tmpFile.Name()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxPosterSize+1))
	closeErr := tmpFile.Close()
	if err != nil {
		_ = pm.fs.Remove(tempDownloadPath)
		return nil, fmt.Errorf("failed to write image: %w", err)
	}
	if closeErr != nil {
		_ = pm.fs.Remove(tempDownloadPath)
		return nil, fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	// Check if the response was truncated (exceeded the size limit).
	if written >= maxPosterSize+1 {
		_ = pm.fs.Remove(tempDownloadPath)
		return nil, fmt.Errorf("image too large (max 50 MB)")
	}

	// Remove any previous full image, then atomically rename.
	_ = pm.fs.Remove(tempFullPath)
	if err := pm.fs.Rename(tempDownloadPath, tempFullPath); err != nil {
		_ = pm.fs.Remove(tempDownloadPath)
		return nil, fmt.Errorf("failed to finalize image download: %w", err)
	}

	// After rename, tempFullPath exists and must be cleaned up if we
	// fail or panic before returning it to the caller.
	success := false
	defer func() {
		if !success {
			_ = pm.fs.Remove(tempFullPath)
			_ = pm.fs.Remove(tempCroppedPath)
		}
	}()

	// Attempt automatic crop; fall back to a full-image copy on failure.
	if err := imageutil.CropPosterFromCover(pm.fs, tempFullPath, tempCroppedPath, 0); err != nil {
		_ = pm.fs.Remove(tempCroppedPath)
		if copyErr := copyFile(pm.fs, tempFullPath, tempCroppedPath); copyErr != nil {
			_ = pm.fs.Remove(tempFullPath)
			return nil, fmt.Errorf("failed to create poster image: %w", copyErr)
		}
	}

	croppedURL := fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg?v=%d", url.PathEscape(jobID), url.PathEscape(posterID), time.Now().UnixMilli())

	success = true
	return &cropResult{
		CroppedPath: tempCroppedPath,
		FullPath:    tempFullPath,
		CroppedURL:  croppedURL,
	}, nil
}

// validatePosterID ensures the posterID is a safe, non-empty filename
// component with no path traversal.
func validatePosterID(posterID string) error {
	if posterID == "" || posterID == "." || posterID == ".." {
		return fmt.Errorf("invalid poster ID: %q", posterID)
	}
	if strings.ContainsAny(posterID, "/\\") {
		return fmt.Errorf("invalid poster ID: %q must not contain path separators", posterID)
	}
	return nil
}

// ValidateJobID ensures the jobID is a safe, non-empty path component
// with no directory traversal. This prevents path traversal attacks when
// jobID is used in filepath.Join and URL construction.
// Exported so that API handlers and worker functions can validate jobID
// before using it in filesystem operations.
func ValidateJobID(jobID string) error {
	_, err := models.ParseJobID(jobID)
	return err
}

// validatePathWithinDir checks that the cleaned path starts with the
// cleaned directory plus a path separator, preventing path traversal.
func validatePathWithinDir(path, dir string) error {
	cleanDir := filepath.Clean(dir) + string(os.PathSeparator)
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath, cleanDir) {
		return fmt.Errorf("path %q escapes directory %q", path, dir)
	}
	return nil
}

// copyFile copies the contents of src to dst using the provided filesystem.
func copyFile(fs afero.Fs, src, dst string) (retErr error) {
	in, err := fs.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := fs.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close destination file: %w", closeErr)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	return nil
}

// drainAndClose drains and closes an HTTP response body.
func drainAndClose(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	return body.Close()
}

// Compile-time interface satisfaction check.
var _ PosterManagerInterface = (*PosterManager)(nil)

// configDirPermTemp mirrors config.DirPermTemp for directory creation
// without importing the config package (avoids heavy dependency chain).
const configDirPermTemp = 0755
