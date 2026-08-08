package temp

import (
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

func extForContentType(contentType string) string {
	mediaType, _, _ := mime.ParseMediaType(strings.TrimSpace(contentType))
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/avif":
		return ".avif"
	case "image/apng":
		return ".png"
	default:
		return ".jpg"
	}
}

func contentTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".avif":
		return "image/avif"
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

func get(fs afero.Fs, cacheDir, rawURL string, ttl time.Duration) (file afero.File, contentType string, state CacheState) {
	shardDir, hashPrefix := pathFor(cacheDir, rawURL)
	path, ext, ok := resolveEntry(fs, shardDir, hashPrefix)
	if !ok {
		return nil, "", CacheAbsent
	}
	info, err := fs.Stat(path)
	if err != nil {
		return nil, "", CacheAbsent
	}
	file, err = fs.Open(path)
	if err != nil {
		return nil, "", CacheAbsent
	}
	contentType = contentTypeForExt(ext)
	if time.Since(info.ModTime()) < ttl {
		return file, contentType, CacheFresh
	}
	return file, contentType, CacheStale
}

type fetchResult struct {
	cachedPath    string
	tempPath      string
	contentType   string
	err           error
	persistFailed bool
}

func fetchAndCache(ctx context.Context, fs afero.Fs, cacheDir, cacheKey, fetchURL string, client *http.Client, userAgent, referer string) fetchResult {
	fetchCtx := context.WithoutCancel(ctx)
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return fetchResult{err: fmt.Errorf("create request: %w", err)}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
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

	upstreamCT := resp.Header.Get("Content-Type")
	normalizedCT := contentTypeForExt(extForContentType(upstreamCT))

	shardDir, hashPrefix := pathFor(cacheDir, cacheKey)
	if err := fs.MkdirAll(shardDir, 0o755); err != nil {
		logging.Warnf("image cache: mkdir shard failed: %v", err)
		return fetchResult{err: fmt.Errorf("mkdir shard: %w", err), persistFailed: true}
	}
	tmpD := tempDir(cacheDir)
	if err := fs.MkdirAll(tmpD, 0o755); err != nil {
		logging.Warnf("image cache: mkdir tmp failed: %v", err)
		return fetchResult{err: fmt.Errorf("mkdir tmp: %w", err), persistFailed: true}
	}
	rb := make([]byte, 8)
	if _, err := randRead(rb); err != nil {
		return fetchResult{err: fmt.Errorf("rand: %w", err)}
	}
	tmpPath := filepath.Join(tmpD, hex.EncodeToString(rb))
	f, err := fs.Create(tmpPath)
	if err != nil {
		logging.Warnf("image cache: create temp failed: %v", err)
		return fetchResult{err: fmt.Errorf("create temp: %w", err), persistFailed: true}
	}
	werr := &writeTracker{w: f}
	n, copyErr := io.Copy(werr, io.LimitReader(resp.Body, maxImageProxyResponseSize+1))
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

	ext := extForContentType(upstreamCT)
	dstPath := filepath.Join(shardDir, hashPrefix+ext)
	if err := atomicRename(fs, tmpPath, dstPath); err != nil {
		logging.Warnf("image cache: rename %s -> %s failed, serving from temp: %v", tmpPath, dstPath, err)
		return fetchResult{tempPath: tmpPath, contentType: normalizedCT}
	}

	if matches, _, found := resolveAllEntries(fs, shardDir, hashPrefix); found {
		for _, m := range matches {
			if m != dstPath {
				_ = fs.Remove(m)
			}
		}
	}
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
