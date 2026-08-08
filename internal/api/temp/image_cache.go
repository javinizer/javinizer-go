package temp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"golang.org/x/sync/singleflight"
)

const maxImageProxyResponseSize = 50 * 1024 * 1024

var randRead = rand.Read

const defaultContentType = "image/jpeg"

var imageCacheGroup singleflight.Group

// CacheState represents the freshness state of a cache entry.
type CacheState int

const (
	// CacheAbsent means no cache entry exists.
	CacheAbsent CacheState = iota
	// CacheFresh means the entry exists and is within the TTL.
	CacheFresh
	// CacheStale means the entry exists but has exceeded the TTL.
	CacheStale
)

var entryExt = regexp.MustCompile(`^([0-9a-f]{64})\.(jpg|png|webp|gif|avif)$`)

func cacheRoot(cacheDir string) string {
	return filepath.Join(cacheDir, "image-cache")
}

func tempDir(cacheDir string) string {
	return filepath.Join(cacheRoot(cacheDir), ".tmp")
}

func pathFor(cacheDir, rawURL string) (shardDir, hashPrefix string) {
	h := sha256.Sum256([]byte(rawURL))
	hash := hex.EncodeToString(h[:])
	return filepath.Join(cacheRoot(cacheDir), hash[:2]), hash
}

const (
	imageTypeJpeg = "image/jpeg"
	imageTypePng  = "image/png"
	imageTypeWebp = "image/webp"
	imageTypeGif  = "image/gif"
	imageTypeAvif = "image/avif"
	imageTypeApng = "image/apng"

	octetStream = "application/octet-stream"
)

func extForContentType(contentType string) string {
	mediaType, _, _ := mime.ParseMediaType(strings.TrimSpace(contentType))
	switch mediaType {
	case imageTypeJpeg:
		return ".jpg"
	case imageTypePng:
		return ".png"
	case imageTypeWebp:
		return ".webp"
	case imageTypeGif:
		return ".gif"
	case imageTypeAvif:
		return ".avif"
	case imageTypeApng:
		return ".png"
	default:
		return ".jpg"
	}
}

func contentTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg":
		return imageTypeJpeg
	case ".png":
		return imageTypePng
	case ".webp":
		return imageTypeWebp
	case ".gif":
		return imageTypeGif
	case ".avif":
		return imageTypeAvif
	default:
		return defaultContentType
	}
}

func resolveEntry(fs afero.Fs, shardDir, hashPrefix string) (path, ext string, ok bool) {
	entries, err := afero.ReadDir(fs, shardDir)
	if err != nil {
		return "", "", false
	}
	var best string
	var bestMtime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := entryExt.FindStringSubmatch(e.Name())
		if m == nil || m[1] != hashPrefix {
			continue
		}
		if best == "" || e.ModTime().After(bestMtime) {
			best = filepath.Join(shardDir, e.Name())
			bestMtime = e.ModTime()
			ext = "." + m[2]
		}
	}
	if best == "" {
		return "", "", false
	}
	return best, ext, true
}

func resolveAllEntries(fs afero.Fs, shardDir, hashPrefix string) ([]string, string, bool) {
	entries, err := afero.ReadDir(fs, shardDir)
	if err != nil {
		return nil, "", false
	}
	var paths []string
	var bestExt string
	var bestMtime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := entryExt.FindStringSubmatch(e.Name())
		if m == nil || m[1] != hashPrefix {
			continue
		}
		paths = append(paths, filepath.Join(shardDir, e.Name()))
		if bestExt == "" || e.ModTime().After(bestMtime) {
			bestExt = "." + m[2]
			bestMtime = e.ModTime()
		}
	}
	if len(paths) == 0 {
		return nil, "", false
	}
	return paths, bestExt, true
}

func get(fs afero.Fs, cacheDir, rawURL string, ttl time.Duration) (file afero.File, contentType string, remaining time.Duration, state CacheState) {
	shardDir, hashPrefix := pathFor(cacheDir, rawURL)
	path, ext, ok := resolveEntry(fs, shardDir, hashPrefix)
	if !ok {
		return nil, "", 0, CacheAbsent
	}
	info, err := fs.Stat(path)
	if err != nil {
		return nil, "", 0, CacheAbsent
	}
	file, err = fs.Open(path)
	if err != nil {
		return nil, "", 0, CacheAbsent
	}
	contentType = contentTypeForExt(ext)
	remaining = ttl - time.Since(info.ModTime())
	if remaining > 0 {
		return file, contentType, remaining, CacheFresh
	}
	return file, contentType, remaining, CacheStale
}

type fetchResult struct {
	cachedPath    string
	body          []byte
	contentType   string
	err           error
	persistFailed bool
}

func sniffImageType(head []byte) string {
	sniffed := http.DetectContentType(head)
	if sniffed != octetStream || len(head) < 12 {
		return sniffed
	}
	if string(head[0:4]) == "RIFF" && string(head[8:12]) == "WEBP" {
		return imageTypeWebp
	}
	if string(head[4:8]) == "ftyp" && (string(head[8:12]) == "avif" || string(head[8:12]) == "avis") {
		return imageTypeAvif
	}
	return sniffed
}

func isCacheableMediaType(mediaType string) bool {
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "image/avif", "image/apng":
		return true
	}
	return false
}

func fetchAndCache(ctx context.Context, fs afero.Fs, cacheDir, cacheKey, fetchURL string, client *http.Client, userAgent, referer string, maxCacheSizeMB int) fetchResult {
	fetchCtx := context.WithoutCancel(ctx)
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return fetchResult{err: fmt.Errorf("create request: %w", err)}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", imageTypeAvif+","+imageTypeWebp+","+imageTypeApng+","+imageTypeJpeg+","+imageTypePng+","+imageTypeGif)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fetchResult{err: fmt.Errorf("fetch: %w", err)}
	}
	defer func() { _ = httpclient.DrainAndClose(resp.Body) }()

	if resp.StatusCode != http.StatusOK {
		return fetchResult{err: fmt.Errorf("image source returned non-200 status")}
	}

	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	if mediaType != "" && mediaType != octetStream && !isCacheableMediaType(mediaType) {
		if strings.HasPrefix(mediaType, "image/") {
			return fetchResult{err: fmt.Errorf("unsupported image content type %q", mediaType)}
		}
		return fetchResult{err: fmt.Errorf("non-image content type %q", mediaType)}
	}
	var head []byte
	if mediaType == "" || mediaType == octetStream {
		buf := make([]byte, 512)
		hn, _ := io.ReadAtLeast(resp.Body, buf, 1)
		head = buf[:hn]
		sniffed := sniffImageType(head)
		if !isCacheableMediaType(sniffed) {
			return fetchResult{err: fmt.Errorf("uncacheable content in headerless response (sniffed %q)", sniffed)}
		}
	}

	drainForDegrade := func() ([]byte, string) {
		rest, _ := io.ReadAll(io.LimitReader(resp.Body, maxImageProxyResponseSize+1-int64(len(head))))
		full := make([]byte, 0, len(head)+len(rest))
		full = append(full, head...)
		full = append(full, rest...)
		if len(full) == 0 || len(full) > maxImageProxyResponseSize {
			return nil, ""
		}
		ct := sniffImageType(full)
		if !isCacheableMediaType(ct) {
			return nil, ""
		}
		return full, ct
	}

	shardDir, hashPrefix := pathFor(cacheDir, cacheKey)
	if err := fs.MkdirAll(shardDir, 0o755); err != nil {
		logging.Warnf("image cache: mkdir shard failed: %v", err)
		body, dct := drainForDegrade()
		return fetchResult{err: fmt.Errorf("mkdir shard: %w", err), persistFailed: true, body: body, contentType: dct}
	}
	tmpD := tempDir(cacheDir)
	if err := fs.MkdirAll(tmpD, 0o755); err != nil {
		logging.Warnf("image cache: mkdir tmp failed: %v", err)
		body, dct := drainForDegrade()
		return fetchResult{err: fmt.Errorf("mkdir tmp: %w", err), persistFailed: true, body: body, contentType: dct}
	}
	rb := make([]byte, 8)
	if _, err := randRead(rb); err != nil {
		return fetchResult{err: fmt.Errorf("rand: %w", err)}
	}
	tmpPath := filepath.Join(tmpD, hex.EncodeToString(rb))
	f, err := fs.Create(tmpPath)
	if err != nil {
		logging.Warnf("image cache: create temp failed: %v", err)
		body, dct := drainForDegrade()
		return fetchResult{err: fmt.Errorf("create temp: %w", err), persistFailed: true, body: body, contentType: dct}
	}
	werr := &writeTracker{w: f}
	src := io.LimitReader(resp.Body, maxImageProxyResponseSize+1)
	if head != nil {
		src = io.MultiReader(bytes.NewReader(head), io.LimitReader(resp.Body, maxImageProxyResponseSize+1-int64(len(head))))
	}
	n, copyErr := io.Copy(werr, src)
	_ = f.Close()
	if copyErr != nil {
		_ = fs.Remove(tmpPath)
		if werr.writeErr {
			logging.Warnf("image cache: disk write failed, will fall back to uncached: %v", copyErr)
			return fetchResult{err: fmt.Errorf("write temp: %w", copyErr), persistFailed: true}
		}
		return fetchResult{err: fmt.Errorf("write temp: %w", copyErr)}
	}
	if n > maxImageProxyResponseSize {
		_ = fs.Remove(tmpPath)
		return fetchResult{err: fmt.Errorf("response exceeds %d byte cap", maxImageProxyResponseSize)}
	}

	hf, herr := fs.Open(tmpPath)
	if herr != nil {
		_ = fs.Remove(tmpPath)
		return fetchResult{err: fmt.Errorf("verify temp: %w", herr), persistFailed: true}
	}
	vbuf := make([]byte, 512)
	vn, _ := hf.Read(vbuf)
	_ = hf.Close()
	persisted := sniffImageType(vbuf[:vn])
	if !isCacheableMediaType(persisted) {
		_ = fs.Remove(tmpPath)
		return fetchResult{err: fmt.Errorf("invalid image content (sniffed %q)", persisted)}
	}
	normalizedCT := contentTypeForExt(extForContentType(persisted))

	ext := extForContentType(persisted)
	dstPath := filepath.Join(shardDir, hashPrefix+ext)
	if err := atomicRename(fs, tmpPath, dstPath); err != nil {
		body, readErr := afero.ReadFile(fs, tmpPath)
		_ = fs.Remove(tmpPath)
		if readErr != nil {
			return fetchResult{err: fmt.Errorf("rename: %w", err), persistFailed: true}
		}
		logging.Warnf("image cache: rename %s -> %s failed, serving from memory: %v", tmpPath, dstPath, err)
		return fetchResult{body: body, contentType: normalizedCT, persistFailed: true}
	}

	if matches, _, found := resolveAllEntries(fs, shardDir, hashPrefix); found {
		for _, m := range matches {
			if m != dstPath {
				_ = fs.Remove(m)
			}
		}
	}
	evictImageCacheToSize(fs, cacheDir, maxCacheSizeMB, dstPath)
	return fetchResult{cachedPath: dstPath, contentType: normalizedCT}
}

type writeTracker struct {
	w        io.Writer
	writeErr bool
}

func (t *writeTracker) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if err != nil {
		t.writeErr = true
	}
	return n, err
}

func atomicRename(fs afero.Fs, src, dst string) error {
	if err := fs.Rename(src, dst); err == nil {
		return nil
	} else if os.IsNotExist(err) {
		if mkErr := fs.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return mkErr
		}
		return fs.Rename(src, dst)
	} else if !os.IsExist(err) {
		return err
	}
	_ = fs.Remove(dst)
	return fs.Rename(src, dst)
}

func fetchBodyToMemory(ctx context.Context, client *http.Client, fetchURL, userAgent, referer string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", imageTypeAvif+","+imageTypeWebp+","+imageTypeApng+","+imageTypeJpeg+","+imageTypePng+","+imageTypeGif)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = httpclient.DrainAndClose(resp.Body) }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image source returned non-200 status")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageProxyResponseSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxImageProxyResponseSize {
		return nil, "", fmt.Errorf("response exceeds %d byte cap", maxImageProxyResponseSize)
	}
	ct := sniffImageType(body)
	if !isCacheableMediaType(ct) {
		return nil, "", fmt.Errorf("uncacheable content type %q", ct)
	}
	return body, ct, nil
}

func evictImageCacheToSize(fs afero.Fs, cacheDir string, maxSizeMB int, keep ...string) {
	if maxSizeMB <= 0 {
		return
	}
	if _, removed, err := worker.EvictImageCacheToSize(fs, cacheDir, int64(maxSizeMB)<<20, keep...); err != nil {
		logging.Warnf("image cache: size eviction failed: %v", err)
	} else if removed > 0 {
		logging.Infof("image cache: evicted %d entr(ies) to enforce the %d MB quota", removed, maxSizeMB)
	}
}
