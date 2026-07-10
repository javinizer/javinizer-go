package r18devdump

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// LatestDumpURL is the r18.dev redirect endpoint that resolves to the most
// recent dated dump on S3-compatible storage. It is a var (not a const) so
// tests can point it at an httptest server.
var LatestDumpURL = "https://r18.dev/dumps/latest"

// downloadUserAgent is sent on dump requests. r18.dev sits behind Cloudflare,
// which rejects the default Go-http-client User-Agent with a 403.
const downloadUserAgent = "Mozilla/5.0 (compatible; Javinizer/1.0; +https://github.com/javinizer/javinizer-go)"

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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return DownloadResult{}, fmt.Errorf("dump endpoint returned status %d", resp.StatusCode)
	}

	finalURL := resp.Request.URL.String()
	res := DownloadResult{
		FinalURL:   finalURL,
		SourceDate: extractSourceDate(finalURL),
	}

	if currentSourceURL != "" && finalURL == currentSourceURL {
		res.Unchanged = true
		return res, nil
	}

	body := io.Reader(resp.Body)
	if progress != nil {
		total := resp.ContentLength
		if total < 0 {
			total = 0
		}
		body = &countingReader{r: resp.Body, total: total, report: progress}
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
