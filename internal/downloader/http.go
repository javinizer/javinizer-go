package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/spf13/afero"
)

func (d *Downloader) download(ctx context.Context, url, destPath string, mediaType MediaType, options ...any) (finalResult *DownloadResult, finalErr error) {
	startTime := time.Now()
	overwriteExisting, dedup := resolveDownloadOptions(options)

	result := &DownloadResult{
		URL:        url,
		LocalPath:  destPath,
		Type:       mediaType,
		Downloaded: false,
	}

	var reservation *downloadReservation
	if overwriteExisting {
		var skipped bool
		var reservationErr error
		reservation, skipped, reservationErr = acquireDownloadReservation(ctx, dedup, destPath)
		if reservationErr != nil {
			result.Error = reservationErr
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
		if skipped {
			result.Skipped = true
			result.Duration = time.Since(startTime)
			return result, nil
		}
		defer func() {
			finishDownloadReservation(dedup, destPath, reservation, finalErr == nil)
		}()
	}

	if err := validateURLScheme(url); err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	select {
	case <-ctx.Done():
		result.Error = ctx.Err()
		result.Duration = time.Since(startTime)
		return result, result.Error
	default:
	}

	existed := false
	if overwriteExisting {
		info, err := d.fs.Stat(destPath)
		switch {
		case err == nil:
			existed = true
			result.Size = info.Size()
		case os.IsNotExist(err):
		default:
			result.Error = fmt.Errorf("failed to stat destination: %w", err)
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
	} else if info, err := d.fs.Stat(destPath); err == nil {
		result.Size = info.Size()
		result.Duration = time.Since(startTime)
		return result, nil
	}

	destDir := filepath.Dir(destPath)
	if err := d.fs.MkdirAll(destDir, config.DirPerm); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	if d.config.UserAgent != "" {
		req.Header.Set("User-Agent", d.config.UserAgent)
	}
	if referer := resolveDownloadReferer(url); referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to download: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}
	defer func() {
		_ = httpclient.DrainAndClose(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		result.Error = &statusError{statusCode: resp.StatusCode}
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	tempPath := uniqueTempPath(destPath, "tmp")
	outFile, err := d.fs.Create(tempPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to create file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

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

	// A 200 with an empty body (transient CDN/proxy hiccup) yields (0, nil)
	// from io.Copy — without this guard, replaceFile would swap valid
	// artwork for a zero-byte file and report success. Fatal under
	// --overwrite-existing-media, so refuse before any replacement.
	if written == 0 {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("downloaded 0 bytes for %s", url)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Never swap good media for garbage: refuse before replaceFile when the
	// payload is provably wrong — a declared truncation (Content-Length) or
	// content provably not media (see validateDownloadedMedia: declared
	// text/JSON/XML types or a body that IS HTML/XML/JSON markup). Unknown
	// binary payloads pass through deliberately; this guard only fires on
	// positive evidence of corruption, never on uncertainty.
	// Only a DECLARED positive length can prove truncation; 0 also means
	// "unspecified" for close-delimited responses, and -1 is chunked.
	if resp.ContentLength > 0 && written != resp.ContentLength {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("downloaded %d of %d bytes for %s (truncated)", written, resp.ContentLength, url)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	if err := validateDownloadedMedia(d.fs, tempPath, resp.Header.Get("Content-Type"), destPath); err != nil {
		_ = d.fs.Remove(tempPath)
		result.Error = err
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	if err := replaceFile(d.fs, tempPath, destPath); err != nil {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("failed to replace file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Size = written
	result.Downloaded = true
	result.Replaced = existed
	result.Duration = time.Since(startTime)

	return result, nil
}

// validateDownloadedMedia refuses to let obviously-not-media payloads reach
// replaceFile: a 200-OK HTML challenge page, a JSON error body, or an XML
// error document would otherwise atomically overwrite valid artwork. The
// guard is positive-evidence-only — HTML/XML/JSON are NEVER a valid media
// payload, while unknown binary bytes pass through untouched — so unusual
// but real image/video encodings and fixture bytes are never rejected by
// mistake.
func validateDownloadedMedia(fs afero.Fs, tempPath, contentType, destPath string) error {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	// Any declared text/* type is provably not image/video payload —
	// "text/plain" prose like "rate limit exceeded" must not reach
	// replaceFile. Media arrives as image/*, video/*, octet-stream, or an
	// undeclared type (checked by content below).
	if strings.HasPrefix(ct, "text/") || strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/xml") ||
		strings.HasSuffix(ct, "+xml") {
		return fmt.Errorf("downloaded %q instead of media for %s (likely an auth challenge or proxy error response)", ct, destPath)
	}

	f, err := fs.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to read downloaded file: %w", err)
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, 256)
	n, err := f.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read downloaded file: %w", err)
	}
	trimmed := strings.TrimSpace(strings.ToLower(string(head[:n])))
	if strings.HasPrefix(trimmed, "<!doctype") || strings.HasPrefix(trimmed, "<html") ||
		strings.HasPrefix(trimmed, "<head") || strings.HasPrefix(trimmed, "<?xml") ||
		strings.HasPrefix(trimmed, "<error") || strings.HasPrefix(trimmed, "<response") ||
		strings.HasPrefix(trimmed, "{") {
		return fmt.Errorf("downloaded an HTML/JSON document instead of media for %s (likely an auth challenge or proxy error)", destPath)
	}
	return nil
}

func resolveDownloadOptions(options []any) (bool, *sync.Map) {
	var overwriteExisting bool
	var dedup *sync.Map
	for _, option := range options {
		switch value := option.(type) {
		case bool:
			overwriteExisting = value
		case *sync.Map:
			dedup = value
		}
	}
	return overwriteExisting, dedup
}

type downloadReservation struct {
	done    chan struct{}
	success bool
}

func acquireDownloadReservation(ctx context.Context, dedup *sync.Map, destPath string) (*downloadReservation, bool, error) {
	if dedup == nil {
		return nil, false, nil
	}
	for {
		value, loaded := dedup.LoadOrStore(destPath, &downloadReservation{done: make(chan struct{})})
		if !loaded {
			return value.(*downloadReservation), false, nil
		}
		reservation, ok := value.(*downloadReservation)
		if !ok {
			return nil, true, nil
		}
		select {
		case <-reservation.done:
			if reservation.success {
				return nil, true, nil
			}
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
}

func finishDownloadReservation(dedup *sync.Map, destPath string, reservation *downloadReservation, success bool) {
	if reservation == nil {
		return
	}
	reservation.success = success
	if !success {
		dedup.Delete(destPath)
	}
	close(reservation.done)
}

func uniqueTempPath(destPath, suffix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return destPath + "." + hex.EncodeToString(buf) + "." + suffix
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
