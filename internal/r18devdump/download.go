package r18devdump

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	neturl "net/url"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/ssrf"
)

// LatestDumpURL is the r18.dev redirect endpoint that resolves to the most
// recent dated dump on S3-compatible storage. It is a var (not a const) so
// tests can point it at an httptest server.
var LatestDumpURL = "https://r18.dev/dumps/latest"

func isTestDumpURL(rawURL string) bool {
	if u, err := neturl.Parse(rawURL); err == nil {
		h := strings.ToLower(u.Hostname())
		if h == "127.0.0.1" || h == "localhost" {
			return true
		}
	}
	return false
}

const maxDumpDecompressedBytes int64 = 4 << 30

var allowedDumpHosts = regexp.MustCompile(`^(.+\.)?(r18\.dev|amazonaws\.com|wasabisys\.com)$`)

const downloadUserAgent = "Mozilla/5.0 (compatible; Javinizer/1.0; +https://github.com/javinizer/javinizer-go)"

// DumpURLOverride returns the dump endpoint to use, honoring the
// JAVINIZER_R18DEV_DUMP_URL env var when set.
func isOverrideHost(redirectHost string) bool {
	if override := os.Getenv("JAVINIZER_R18DEV_DUMP_URL"); override != "" {
		if u, err := neturl.Parse(override); err == nil {
			return strings.EqualFold(strings.ToLower(redirectHost), strings.ToLower(u.Hostname()))
		}
	}
	return false
}

// DumpURLOverride returns the dump endpoint to use, honoring the
// JAVINIZER_R18DEV_DUMP_URL env var when set.
func DumpURLOverride() string {
	if u := os.Getenv("JAVINIZER_R18DEV_DUMP_URL"); u != "" {
		return u
	}
	return LatestDumpURL
}

// DownloadResult describes a completed (or skipped) download.
type DownloadResult struct {
	FinalURL   string
	SourceDate string
	Bytes      int64
	Unchanged  bool
}

// Download fetches the latest r18.dev dump, gunzips it, and pipes the
// decompressed stream to importFn. The response body is streamed through gzip
// and the parser, so the full decompressed dump never resides in memory.
func Download(ctx context.Context, client *http.Client, currentSourceURL string,
	progress func(compressedBytes, totalBytes int64), importFn func(io.Reader, DownloadResult) error) (DownloadResult, error) {

	hardenedClient := client
	dumpURL := DumpURLOverride()
	if !isTestDumpURL(dumpURL) {
		transport, ok := client.Transport.(*http.Transport)
		if !ok {
			if client.Transport == nil {
				transport, ok = http.DefaultTransport.(*http.Transport)
			}
			if !ok {
				return DownloadResult{}, fmt.Errorf("r18dev dump: client transport must be *http.Transport for SSRF pinning (got %T)", client.Transport)
			}
		}
		pinned, err := ssrf.NewPinnedDialTransport(transport)
		if err != nil {
			return DownloadResult{}, fmt.Errorf("r18dev dump: failed to install pinned dial transport: %w", err)
		}
		copied := *client
		copied.Transport = pinned
		hardenedClient = &copied
		logging.Debugf("r18dev dump: installed SSRF pinned dial transport")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dumpURL, nil)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	req.Header.Set("Accept", "*/*")

	checkRedirect := hardenedClient.CheckRedirect
	hardenedClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("r18dev dump: stopped after 10 redirects")
		}
		host := strings.ToLower(req.URL.Hostname())
		if !allowedDumpHosts.MatchString(host) && !isTestDumpURL(via[0].URL.String()) && !isOverrideHost(host) {
			return fmt.Errorf("r18dev dump: refusing redirect to %s", req.URL.Redacted())
		}
		if checkRedirect != nil {
			if err := checkRedirect(req, via); err != nil {
				return err
			}
			host = strings.ToLower(req.URL.Hostname())
			if !allowedDumpHosts.MatchString(host) && !isTestDumpURL(via[0].URL.String()) && !isOverrideHost(host) {
				return fmt.Errorf("r18dev dump: callback redirected to unallowed host %s", req.URL.Redacted())
			}
		}
		return nil
	}
	defer func() { hardenedClient.CheckRedirect = checkRedirect }()

	resp, err := hardenedClient.Do(req)
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

	cappedReader := &overflowReader{r: gz, max: maxDumpDecompressedBytes}

	if err := importFn(cappedReader, res); err != nil {
		return res, err
	}
	if cr, ok := body.(*countingReader); ok {
		res.Bytes = cr.n
	}
	return res, nil
}

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
