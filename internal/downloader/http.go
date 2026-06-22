package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
)

func (d *Downloader) download(ctx context.Context, url, destPath string, mediaType MediaType) (*DownloadResult, error) {
	startTime := time.Now()

	result := &DownloadResult{
		URL:        url,
		LocalPath:  destPath,
		Type:       mediaType,
		Downloaded: false,
	}

	if err := validateURLScheme(url); err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		result.Error = ctx.Err()
		result.Duration = time.Since(startTime)
		return result, result.Error
	default:
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
	if err := d.fs.MkdirAll(destDir, config.DirPerm); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Set user agent
	if d.config.UserAgent != "" {
		req.Header.Set("User-Agent", d.config.UserAgent)
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
		_ = httpclient.DrainAndClose(resp.Body)
	}()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		result.Error = &statusError{statusCode: resp.StatusCode}
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

// retryableOperation wraps an attempt function with retry logic for transient errors.
type retryableOperation struct {
	initialDelay time.Duration
	maxDelay     time.Duration
}

// ExecuteWithRetry runs attemptFn with exponential backoff for retryable errors.
// It retries on errors classified as retryable by isRetryableError, and fails
// immediately on non-retryable errors.
// Exponential backoff formula: delay = min(initialDelay * 2^(retryAttempt-1), maxDelay)
// Context cancellation is respected during backoff delays and attempts.
func (ro *retryableOperation) ExecuteWithRetry(ctx context.Context, attemptFn func() error, maxRetries int, url string) error {
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	totalAttempts := maxRetries + 1 // Initial attempt + retries

	for attempt := 0; attempt < totalAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := attemptFn(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if !isRetryableError(lastErr) {
			return fmt.Errorf("download failed after %d attempt(s): %s returned %w", attempt+1, url, lastErr)
		}

		if attempt == totalAttempts-1 {
			break
		}

		retryAttempt := attempt + 1
		delay := ro.initialDelay * time.Duration(1<<uint(retryAttempt-1))
		if delay > ro.maxDelay {
			delay = ro.maxDelay
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return fmt.Errorf("download failed after %d attempt(s): %s returned %w", totalAttempts, url, lastErr)
}

// DownloadWithRetry downloads a file with exponential backoff retry logic for transient errors
// It retries on HTTP 503, 500, 429 and network errors, but fails immediately on 404, 403, 401, 400
// Exponential backoff formula: delay = min(100ms * 2^(retryAttempt-1), 10s) where retryAttempt starts at 1
// Context cancellation is respected during backoff delays and HTTP requests
func (d *Downloader) DownloadWithRetry(ctx context.Context, url, destPath string, maxRetries int) error {
	op := &retryableOperation{
		initialDelay: 100 * time.Millisecond,
		maxDelay:     10 * time.Second,
	}

	return op.ExecuteWithRetry(ctx, func() error {
		_, err := d.download(ctx, url, destPath, "")
		return err
	}, maxRetries, url)
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

	var sErr *statusError
	if errors.As(err, &sErr) {
		switch sErr.statusCode {
		case http.StatusServiceUnavailable, // 503
			http.StatusInternalServerError, // 500
			http.StatusTooManyRequests:     // 429
			return true
		case http.StatusNotFound, // 404
			http.StatusForbidden,    // 403
			http.StatusUnauthorized, // 401
			http.StatusBadRequest:   // 400
			return false
		default:
			return false
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// validateURLScheme checks if the URL uses http or https scheme
func validateURLScheme(urlStr string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme '%s': only http and https are allowed", scheme)
	}

	return nil
}

// ResolveMediaReferer selects a compatible Referer header for media requests.
// Delegates to httpclient.ResolveMediaReferer.
func resolveMediaReferer(downloadURL, configuredReferer string) string {
	return httpclient.ResolveMediaReferer(downloadURL, configuredReferer)
}

// resolveDownloadReferer selects a compatible Referer header for media downloads.
func resolveDownloadReferer(downloadURL string) string {
	return resolveMediaReferer(downloadURL, "")
}
