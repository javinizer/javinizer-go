package r18devdump

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// LatestDumpURL is the r18.dev redirect endpoint that resolves to the most
// recent dated dump on S3-compatible storage. It is a var (not a const) so
// tests can point it at an httptest server.
var LatestDumpURL = "https://r18.dev/dumps/latest"

// downloadUserAgent is sent on dump requests. r18.dev sits behind Cloudflare,
// which rejects the default Go-http-client User-Agent with a 403.
const downloadUserAgent = "Mozilla/5.0 (compatible; Javinizer/1.0; +https://github.com/javinizer/javinizer-go)"

// maxResumeAttempts caps consecutive stream-resume retries. The counter
// resets whenever the stream delivers at least one byte, so a flaky
// connection that always makes progress can survive arbitrarily many
// truncations while a genuinely dead endpoint still fails fast.
const maxResumeAttempts = 8

// resumeBackoffBase is the initial delay between stream-resume attempts; it
// doubles per consecutive failure, capped at resumeBackoffMax. Package-level
// vars so tests can shrink them.
var (
	resumeBackoffBase = 500 * time.Millisecond
	resumeBackoffMax  = 10 * time.Second
)

// DumpURLOverride returns the dump endpoint to use, honoring the
// JAVINIZER_R18DEV_DUMP_URL env var when set. This lets users point at a
// mirror/cache and lets tests point the binary at an httptest server.
func DumpURLOverride() string {
	if u := os.Getenv("JAVINIZER_R18DEV_DUMP_URL"); u != "" {
		return u
	}
	return LatestDumpURL
}

// DownloadResult describes a completed (or skipped) download.
type DownloadResult struct {
	FinalURL   string // redirect target (dated dump URL)
	SourceDate string // date parsed from FinalURL, e.g. "2026-04-28"
	Bytes      int64  // compressed bytes transferred (0 if skipped)
	Unchanged  bool   // true when the version matches currentSourceURL
}

// Download fetches the latest r18.dev dump, gunzips it, and pipes the
// decompressed stream to importFn. The response body is streamed through gzip
// and the parser, so the full decompressed dump (multiple GB) never resides in
// memory.
//
// The import paces the network stream (the HTTP body is only read as fast as
// SQLite accepts writes), so the transfer can run for several minutes at a
// server- or NAT-unfriendly trickle. When the server or a middlebox aborts
// such a stream mid-body, the body reader returns io.ErrUnexpectedEOF, which
// the gzip layer surfaces as "scanning dump: unexpected EOF". To survive
// that, the body is wrapped in a resumeReader that re-opens the final URL
// with an HTTP Range request from the exact byte already consumed, guarded by
// If-Range so an object swap is detected.
//
// When currentSourceURL is non-empty and equals the redirect target, the
// download is skipped (Unchanged=true) and importFn is not called — this lets
// `javinizer dump update` no-op when the dump hasn't changed.
//
// progress, if non-nil, receives cumulative compressed byte counts during the
// transfer. totalBytes is the response Content-Length when known (0 if
// unknown, e.g. chunked/streamed responses).
func Download(ctx context.Context, client *http.Client, currentSourceURL string,
	progress func(compressedBytes, totalBytes int64), importFn func(io.Reader, DownloadResult) error) (DownloadResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DumpURLOverride(), nil)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("build request: %w", err)
	}
	// r18.dev frontends with Cloudflare, which 403s the default Go User-Agent.
	req.Header.Set("User-Agent", downloadUserAgent)
	req.Header.Set("Accept", "*/*")
	resp, err := client.Do(req)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("fetch dump: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return DownloadResult{}, fmt.Errorf("dump endpoint returned status %d", resp.StatusCode)
	}

	finalURL := resp.Request.URL.String()
	res := DownloadResult{
		FinalURL:   finalURL,
		SourceDate: extractSourceDate(finalURL),
	}

	if currentSourceURL != "" && finalURL == currentSourceURL {
		_ = resp.Body.Close()
		res.Unchanged = true
		return res, nil
	}

	stream := &resumeReader{
		ctx:     ctx,
		client:  client,
		url:     finalURL,
		etag:    resp.Header.Get("ETag"),
		lastMod: resp.Header.Get("Last-Modified"),
		total:   resp.ContentLength,
		body:    resp.Body,
	}
	defer func() { _ = stream.Close() }()

	body := io.Reader(stream)
	if progress != nil {
		total := resp.ContentLength
		if total < 0 {
			total = 0
		}
		body = &countingReader{r: stream, total: total, report: progress}
	}

	gz, err := gzip.NewReader(body)
	if err != nil {
		return res, fmt.Errorf("gunzip dump: %w", err)
	}
	defer func() { _ = gz.Close() }()

	if err := importFn(gz, res); err != nil {
		return res, err
	}
	if cr, ok := body.(*countingReader); ok {
		res.Bytes = cr.n
	}
	return res, nil
}

// resumeReader wraps a streaming HTTP response body and transparently resumes
// the transfer with HTTP Range requests when the connection dies mid-body.
// The gzip decoder sees one continuous byte stream: bytes 0..offset came from
// earlier connections, bytes offset.. come from the resumed response, and the
// splice is exact because each resume starts at the number of bytes already
// delivered.
type resumeReader struct {
	ctx     context.Context
	client  *http.Client
	url     string // final post-redirect URL, requested directly on resume
	etag    string // If-Range validator from the original response
	lastMod string
	total   int64 // expected total compressed bytes (-1 when unknown)
	offset  int64 // compressed bytes delivered so far
	body    io.ReadCloser

	consecFailures int // resume attempts since the stream last made progress
}

func (r *resumeReader) Read(p []byte) (int, error) {
	for {
		n, err := r.body.Read(p)
		r.offset += int64(n)
		if n > 0 {
			r.consecFailures = 0
			return n, nil
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			if r.total < 0 {
				// Unknown length (close-delimited/chunked): EOF may be the
				// genuine end or a premature close. Probe before concluding.
				if r.classifyUnknownEOF() {
					return 0, io.EOF
				}
				continue // resumed body installed, or total refined — keep reading
			}
			if r.offset >= r.total {
				return 0, io.EOF
			}
		}
		// A canceled context (user abort, stall watchdog) must fail fast —
		// retrying would just re-arm a doomed request.
		if r.ctx.Err() != nil {
			return 0, err
		}
		// Truncated stream: io.ErrUnexpectedEOF (clean close before
		// Content-Length), a reset connection, or an HTTP/2 stream error.
		// Re-open from the exact consumed offset and keep going.
		_ = r.body.Close()
		body, resumeErr := r.resume()
		if resumeErr != nil {
			if r.ctx.Err() != nil {
				return 0, err
			}
			return 0, fmt.Errorf("dump stream interrupted at %s: %w", r.progressLabel(), resumeErr)
		}
		r.body = body
	}
}

// ifRangeValidator returns the validator to send in If-Range on resume/probe
// requests, or "" when the original response carried no usable one. RFC 9110
// 13.1.4 requires a strong validator for If-Range — a weak entity-tag
// (W/"...") must not match and makes servers answer 200 — so a weak ETag
// falls back to Last-Modified. An empty result means the object identity
// cannot be verified across connections: splicing resumed bytes would risk
// merging two different objects, so resumes and EOF probes are refused.
func (r *resumeReader) ifRangeValidator() string {
	if r.etag != "" && !strings.HasPrefix(r.etag, "W/") {
		return r.etag
	}
	return r.lastMod
}

// resume re-opens the dump object with a Range request starting at the
// consumed offset, retrying with backoff until the consecutive-failure cap.
func (r *resumeReader) resume() (io.ReadCloser, error) {
	if r.ifRangeValidator() == "" {
		return nil, fmt.Errorf("cannot resume safely: the original response has no strong ETag or Last-Modified to verify the object is unchanged")
	}
	var lastErr error
	for r.consecFailures < maxResumeAttempts {
		r.consecFailures++
		if !sleepContext(r.ctx, resumeBackoffFor(r.consecFailures)) {
			return nil, r.ctx.Err()
		}
		body, err := r.openRange()
		if err == nil {
			return body, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("resume failed after %d attempts: %w", maxResumeAttempts, lastErr)
}

// openRange issues one resume request and validates that the server honored
// the range against the same object.
func (r *resumeReader) openRange() (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build resume request: %w", err)
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", r.offset))
	req.Header.Set("If-Range", r.ifRangeValidator())
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("resume request: %w", err)
	}
	if resp.StatusCode != http.StatusPartialContent {
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status == http.StatusOK {
			return nil, fmt.Errorf("server ignored the Range request (status 200); the dump object may have changed mid-download")
		}
		return nil, fmt.Errorf("resume request returned status %d", status)
	}
	start, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	if start != r.offset {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("Content-Range starts at byte %d, want %d", start, r.offset)
	}
	if r.total >= 0 && total >= 0 && total != r.total {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("dump object changed mid-download: size %d, started with %d", total, r.total)
	}
	return resp.Body, nil
}

// classifyUnknownEOF runs when the stream reports a clean EOF but the
// response carried no Content-Length, so a premature connection close is
// indistinguishable from the end of the object. It PROBES the final URL with
// a Range request from the consumed offset:
//
// HTTP 416 with total == offset → genuine end (returns true).
// HTTP 206 starting at offset   → truncated; the response body continues the
// stream and is adopted, with r.total recorded (returns false).
// HTTP 416 with total > offset  → truncated; r.total is recorded and the
// caller's standard resume path re-opens the stream (returns false).
// Anything else (probe error, Range ignored, malformed headers) is
// indeterminate → return true and hand io.EOF to the gzip layer, which
// validates stream completeness — a mid-member EOF surfaces there as
// "unexpected EOF" instead of silently truncating data.
func (r *resumeReader) classifyUnknownEOF() bool {
	// A stream that repeatedly stops without progress must not loop probes
	// forever — treat a run of them as indeterminate and let gzip enforce
	// integrity, with one bounded budget shared with the resume path.
	// Without a validator the probe could splice bytes from a different
	// object — degrade to io.EOF and let the gzip layer enforce integrity.
	validator := r.ifRangeValidator()
	if validator == "" {
		return true
	}
	if r.consecFailures >= maxResumeAttempts {
		return true
	}
	r.consecFailures++
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return true
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", r.offset))
	req.Header.Set("If-Range", validator)
	resp, err := r.client.Do(req)
	if err != nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusPartialContent:
		start, total, perr := parseContentRange(resp.Header.Get("Content-Range"))
		if perr != nil || start != r.offset {
			_ = resp.Body.Close()
			return true
		}
		if total >= 0 {
			r.total = total
		}
		_ = r.body.Close()
		r.body = resp.Body
		return false
	case http.StatusRequestedRangeNotSatisfiable:
		size, ok := parseRangeNotSatisfiableTotal(resp.Header.Get("Content-Range"))
		_ = resp.Body.Close()
		if !ok || size <= r.offset {
			return true // genuine end (size == offset expected) or unparseable
		}
		r.total = size // truncated — the standard resume path re-opens the stream
		return false
	default:
		_ = resp.Body.Close()
		return true
	}
}

// parseRangeNotSatisfiableTotal parses the total size from a 416 response's
// "Content-Range: bytes */<total>" header.
func parseRangeNotSatisfiableTotal(s string) (int64, bool) {
	const prefix = "bytes */"
	if !strings.HasPrefix(s, prefix) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(s, prefix), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (r *resumeReader) Close() error {
	if r.body == nil {
		return nil
	}
	return r.body.Close()
}

func (r *resumeReader) progressLabel() string {
	if r.total > 0 {
		return fmt.Sprintf("%d of %d bytes", r.offset, r.total)
	}
	return fmt.Sprintf("%d bytes", r.offset)
}

func resumeBackoffFor(consecutive int) time.Duration {
	d := resumeBackoffBase
	for i := 1; i < consecutive; i++ {
		d *= 2
		if d >= resumeBackoffMax {
			return resumeBackoffMax
		}
	}
	return d
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// parseContentRange parses a "Content-Range: bytes <start>-<end>/<total>"
// header. A total of "*" parses as -1 (unknown).
func parseContentRange(s string) (start, total int64, err error) {
	total = -1
	const prefix = "bytes "
	if !strings.HasPrefix(s, prefix) {
		return 0, 0, fmt.Errorf("malformed Content-Range %q", s)
	}
	body := strings.TrimPrefix(s, prefix)
	slash := strings.LastIndexByte(body, '/')
	if slash < 0 {
		return 0, 0, fmt.Errorf("malformed Content-Range %q", s)
	}
	if totalPart := body[slash+1:]; totalPart != "*" {
		if total, err = strconv.ParseInt(totalPart, 10, 64); err != nil {
			return 0, 0, fmt.Errorf("malformed Content-Range %q: %v", s, err)
		}
	}
	dash := strings.IndexByte(body[:slash], '-')
	if dash < 0 {
		return 0, 0, fmt.Errorf("malformed Content-Range %q", s)
	}
	if start, err = strconv.ParseInt(body[:dash], 10, 64); err != nil {
		return 0, 0, fmt.Errorf("malformed Content-Range %q: %v", s, err)
	}
	return start, total, nil
}

// extractSourceDate parses the dump date from a URL like
// https://r18dotdev.s3.../dumps/r18dotdev_dump_2026-04-28.sql.gz
func extractSourceDate(rawURL string) string {
	base := rawURL
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	const marker = "_dump_"
	if i := strings.Index(base, marker); i >= 0 {
		rest := base[i+len(marker):]
		if j := strings.Index(rest, "."); j > 0 {
			return rest[:j]
		}
	}
	return ""
}

// countingReader wraps an io.Reader and reports cumulative bytes read to a
// callback. Used for download progress reporting.
type countingReader struct {
	r      io.Reader
	n      int64
	total  int64
	report func(n, total int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.report != nil && n > 0 {
		c.report(c.n, c.total)
	}
	return n, err
}
