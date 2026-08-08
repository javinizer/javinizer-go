package temp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingRenameFs struct {
	afero.Fs
	renameErr error
	failCount int32
	maxFails  int32
}

func (f *failingRenameFs) Rename(oldname, newname string) error {
	if f.renameErr != nil && atomic.AddInt32(&f.failCount, 1) <= f.maxFails {
		return f.renameErr
	}
	return f.Fs.Rename(oldname, newname)
}

type failingFs struct {
	afero.Fs
	mkdirAllErr error
	createErr   error
	renameErr   error
	openErr     error
	statErr     error
	removeErr   error
}

func (f *failingFs) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAllErr != nil {
		return f.mkdirAllErr
	}
	return f.Fs.MkdirAll(path, perm)
}

func (f *failingFs) Create(name string) (afero.File, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.Fs.Create(name)
}

func (f *failingFs) Rename(oldname, newname string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *failingFs) Open(name string) (afero.File, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.Fs.Open(name)
}

func (f *failingFs) Stat(name string) (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return f.Fs.Stat(name)
}

func (f *failingFs) Remove(name string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	return f.Fs.Remove(name)
}

func TestCoverage_ContentTypeForAllExtensions(t *testing.T) {
	assert.Equal(t, "image/jpeg", contentTypeForExt(".jpg"))
	assert.Equal(t, "image/png", contentTypeForExt(".png"))
	assert.Equal(t, "image/webp", contentTypeForExt(".webp"))
	assert.Equal(t, "image/gif", contentTypeForExt(".gif"))
	assert.Equal(t, "image/avif", contentTypeForExt(".avif"))
	assert.Equal(t, "image/jpeg", contentTypeForExt(".unknown"))
	assert.Equal(t, "image/jpeg", contentTypeForExt(""))
}

func TestCoverage_ExtForAllContentTypes(t *testing.T) {
	assert.Equal(t, ".jpg", extForContentType("image/jpeg"))
	assert.Equal(t, ".png", extForContentType("image/png"))
	assert.Equal(t, ".webp", extForContentType("image/webp"))
	assert.Equal(t, ".gif", extForContentType("image/gif"))
	assert.Equal(t, ".avif", extForContentType("image/avif"))
	assert.Equal(t, ".png", extForContentType("image/apng"))
	assert.Equal(t, ".jpg", extForContentType("image/svg+xml"))
	assert.Equal(t, ".jpg", extForContentType(""))
	assert.Equal(t, ".jpg", extForContentType("unknown/type"))
	assert.Equal(t, ".webp", extForContentType("image/webp; charset=UTF-8"))
}

func TestCoverage_ResolveEntryMultipleVariants(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	shardDir, hashPrefix := pathFor(tempDir, "http://example.com/img.jpg")
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))

	jpgPath := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(fs, jpgPath, []byte("old"), 0o644))
	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, fs.Chtimes(jpgPath, oldTime, oldTime))

	pngPath := filepath.Join(shardDir, hashPrefix+".png")
	require.NoError(t, afero.WriteFile(fs, pngPath, []byte("new"), 0o644))

	path, ext, ok := resolveEntry(fs, shardDir, hashPrefix)
	require.True(t, ok)
	assert.Equal(t, ".png", ext)
	assert.Contains(t, path, hashPrefix+".png")
}

func TestCoverage_ResolveEntryNoMatch(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	shardDir, hashPrefix := pathFor(tempDir, "http://example.com/img.jpg")
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(shardDir, "other.jpg"), []byte("x"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(shardDir, hashPrefix+".txt"), []byte("y"), 0o644))

	_, _, ok := resolveEntry(fs, shardDir, hashPrefix)
	assert.False(t, ok, "non-matching hash should not resolve")
}

func TestCoverage_ResolveEntryReadDirError(t *testing.T) {
	fs := &failingFs{Fs: afero.NewMemMapFs()}
	_, _, ok := resolveEntry(fs, "/nonexistent", "abcd")
	assert.False(t, ok)
}

func TestCoverage_ResolveAllEntriesMultiple(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	shardDir, hashPrefix := pathFor(tempDir, "http://example.com/img.jpg")
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(shardDir, hashPrefix+".jpg"), []byte("1"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(shardDir, hashPrefix+".png"), []byte("2"), 0o644))

	paths, ext, ok := resolveAllEntries(fs, shardDir, hashPrefix)
	require.True(t, ok)
	assert.Len(t, paths, 2)
	assert.NotEmpty(t, ext)
}

func TestCoverage_ResolveAllEntriesWithDirs(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	shardDir, hashPrefix := pathFor(tempDir, "http://example.com/img.jpg")
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Join(shardDir, "subdir"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(shardDir, hashPrefix+".jpg"), []byte("1"), 0o644))

	paths, _, ok := resolveAllEntries(fs, shardDir, hashPrefix)
	require.True(t, ok)
	assert.Len(t, paths, 1)
}

func TestCoverage_ResolveAllEntriesMissingDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	_, _, ok := resolveAllEntries(fs, "/nonexistent", "abcd")
	assert.False(t, ok)
}

func TestCoverage_GetAbsent(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	file, _, _, state := get(fs, tempDir, "http://example.com/img.jpg", time.Hour)
	assert.Nil(t, file)
	assert.Equal(t, CacheAbsent, state)
}

func TestCoverage_GetStatError(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	rawURL := "http://example.com/img.jpg"
	shardDir, hashPrefix := pathFor(tempDir, rawURL)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	entryPath := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(fs, entryPath, []byte("data"), 0o644))

	failingStat := &failingFs{Fs: fs, statErr: errors.New("stat failed")}
	file, _, _, state := get(failingStat, tempDir, rawURL, time.Hour)
	assert.Nil(t, file)
	assert.Equal(t, CacheAbsent, state)
}

func TestCoverage_GetOpenError(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	rawURL := "http://example.com/img.jpg"
	shardDir, hashPrefix := pathFor(tempDir, rawURL)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	entryPath := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(fs, entryPath, []byte("data"), 0o644))

	failingOpen := &failingFs{Fs: fs, openErr: errors.New("open failed")}
	file, _, _, state := get(failingOpen, tempDir, rawURL, time.Hour)
	assert.Nil(t, file)
	assert.Equal(t, CacheAbsent, state)
}

func TestCoverage_AtomicRenameENOENT(t *testing.T) {
	fs := afero.NewMemMapFs()
	src := "/tmp/src.txt"
	dst := "/nonexistent/dst.txt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("data"), 0o644))

	err := atomicRename(fs, src, dst)
	require.NoError(t, err, "ENOENT should trigger MkdirAll retry")

	data, _ := afero.ReadFile(fs, dst)
	assert.Equal(t, "data", string(data))
}

func TestCoverage_AtomicRenameOverExisting(t *testing.T) {
	fs := afero.NewMemMapFs()
	src := "/tmp/src.txt"
	dst := "/tmp/dst.txt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("old"), 0o644))

	err := atomicRename(fs, src, dst)
	require.NoError(t, err, "rename over existing should succeed via remove+retry")

	data, _ := afero.ReadFile(fs, dst)
	assert.Equal(t, "new", string(data))
}

func TestCoverage_AtomicRenameDirectSuccess(t *testing.T) {
	fs := afero.NewMemMapFs()
	src := "/tmp/src.txt"
	dst := "/tmp/dst.txt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("data"), 0o644))

	err := atomicRename(fs, src, dst)
	require.NoError(t, err)
}

func TestCoverage_AtomicRenameIsExistRetry(t *testing.T) {
	fs := &failingRenameFs{Fs: afero.NewMemMapFs(), renameErr: os.ErrExist, maxFails: 1}
	src := "/tmp/src.txt"
	dst := "/tmp/dst.txt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("old"), 0o644))

	err := atomicRename(fs, src, dst)
	require.NoError(t, err, "IsExist retry should succeed after Remove")

	data, _ := afero.ReadFile(fs, dst)
	assert.Equal(t, "new", string(data))
}

func TestCoverage_AtomicRenameOtherError(t *testing.T) {
	fs := &failingRenameFs{Fs: afero.NewMemMapFs(), renameErr: errors.New("permission denied"), maxFails: 999}
	src := "/tmp/src.txt"
	dst := "/tmp/dst.txt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("data"), 0o644))

	err := atomicRename(fs, src, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestCoverage_FetchAndCacheRequestError(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(60 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, "http://nonexistent.invalid/img.jpg", "http://nonexistent.invalid/img.jpg", client, "test-agent", "", 0)
	assert.Error(t, result.err)
}

func TestCoverage_FetchAndCacheNon200Status(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(60 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	assert.Error(t, result.err)
	assert.False(t, result.persistFailed)
}

func TestCoverage_FetchAndCacheMkdirShardError(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("test"))
	}))
	t.Cleanup(upstream.Close)

	fs := &failingFs{Fs: afero.NewMemMapFs(), mkdirAllErr: errors.New("mkdir failed")}
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(60 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	assert.Error(t, result.err)
	assert.True(t, result.persistFailed)
}

func TestCoverage_FetchAndCacheCreateTempError(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("test"))
	}))
	t.Cleanup(upstream.Close)

	fs := &failingFs{Fs: afero.NewMemMapFs(), createErr: errors.New("create failed")}
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(60 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	assert.Error(t, result.err)
	assert.True(t, result.persistFailed)
}

func TestCoverage_FetchAndCacheRenameFailureDegrades(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("persist-fail-test"))
	}))
	t.Cleanup(upstream.Close)

	fs := &failingRenameFs{Fs: afero.NewMemMapFs(), renameErr: os.ErrExist, maxFails: 999}
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(60 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	assert.NoError(t, result.err, "rename failure degrades to shared in-memory serve, not error")
	assert.Equal(t, jpegBytes("persist-fail-test"), result.body, "downloaded bytes served from memory")
	assert.True(t, result.persistFailed)
}

func TestCoverage_FetchAndCacheSniffsHeaderlessJpeg(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	jpegBytes := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 200)...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "")
		w.Write(jpegBytes)
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(60 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	require.NoError(t, result.err)
	assert.Equal(t, "image/jpeg", result.contentType)
	assert.NotEmpty(t, result.cachedPath)

	cached, err := afero.ReadFile(fs, result.cachedPath)
	require.NoError(t, err)
	assert.Equal(t, jpegBytes, cached, "sniffed head bytes must be preserved in the cached file")
}

func TestCoverage_FetchAndCacheOldExtCleanup(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes("new-png"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	cacheKey := upstream.URL + "/img.jpg"
	shardDir, hashPrefix := pathFor(tempDir, cacheKey)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	oldPath := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(fs, oldPath, []byte("old-jpg"), 0o644))

	client := ssrf.NewSSRFSafeClient(60 * time.Second)
	result := fetchAndCache(context.Background(), fs, tempDir, cacheKey, cacheKey, client, "test-agent", "", 0)
	require.NoError(t, result.err)
	assert.Equal(t, "image/png", result.contentType)

	exists, _ := afero.Exists(fs, oldPath)
	assert.False(t, exists, "old .jpg variant should be removed after successful rename")
}
