package temp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranch_ResolveEntry_SkipsSubdirectories(t *testing.T) {
	fs := afero.NewMemMapFs()
	shardDir := "/cache/ab"
	hashPrefix := strings.Repeat("ab", 32)
	require.NoError(t, fs.MkdirAll(filepath.Join(shardDir, "nested"), 0o755))
	entry := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(fs, entry, []byte("data"), 0o644))

	path, ext, ok := resolveEntry(fs, shardDir, hashPrefix)
	require.True(t, ok)
	assert.Equal(t, entry, path)
	assert.Equal(t, ".jpg", ext)
}

func TestBranch_ResolveAllEntries_SkipsNonMatching(t *testing.T) {
	fs := afero.NewMemMapFs()
	shardDir := "/cache/cd"
	hashPrefix := strings.Repeat("cd", 32)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(shardDir, "readme.txt"), []byte("junk"), 0o644))
	entry := filepath.Join(shardDir, hashPrefix+".png")
	require.NoError(t, afero.WriteFile(fs, entry, []byte("data"), 0o644))

	paths, ext, ok := resolveAllEntries(fs, shardDir, hashPrefix)
	require.True(t, ok)
	assert.Equal(t, []string{entry}, paths)
	assert.Equal(t, ".png", ext)
}

func TestBranch_ResolveAllEntries_OnlyJunkReturnsFalse(t *testing.T) {
	fs := afero.NewMemMapFs()
	shardDir := "/cache/ef"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(shardDir, "stray.tmp"), []byte("junk"), 0o644))

	paths, _, ok := resolveAllEntries(fs, shardDir, strings.Repeat("ef", 32))
	assert.False(t, ok)
	assert.Nil(t, paths)
}

type openErrFs struct {
	afero.Fs
	target string
}

func (f *openErrFs) Open(name string) (afero.File, error) {
	if name == f.target {
		return nil, errors.New("open denied")
	}
	return f.Fs.Open(name)
}

func TestBranch_Get_OpenFailureAfterResolveReturnsAbsent(t *testing.T) {
	mem := afero.NewMemMapFs()
	cacheDir := t.TempDir()
	rawURL := "http://example.com/open-fail.jpg"
	shardDir, hashPrefix := pathFor(cacheDir, rawURL)
	require.NoError(t, mem.MkdirAll(shardDir, 0o755))
	entry := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(mem, entry, []byte("data"), 0o644))

	fs := &openErrFs{Fs: mem, target: entry}
	file, ct, state := get(fs, cacheDir, rawURL, time.Hour)
	assert.Nil(t, file)
	assert.Empty(t, ct)
	assert.Equal(t, CacheAbsent, state)
}

func TestBranch_FetchAndCache_CreateRequestError(t *testing.T) {
	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), "key", "://no-scheme", client, "ua", "")
	require.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "create request")
}

type mkdirSuffixFailFs struct {
	afero.Fs
	suffix string
}

func (f *mkdirSuffixFailFs) MkdirAll(path string, perm os.FileMode) error {
	if filepath.Base(path) == f.suffix {
		return errors.New("mkdir denied")
	}
	return f.Fs.MkdirAll(path, perm)
}

func TestBranch_FetchAndCache_MkdirTmpError(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("img"))
	}))
	t.Cleanup(upstream.Close)

	fs := &mkdirSuffixFailFs{Fs: afero.NewMemMapFs(), suffix: ".tmp"}
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/a.jpg", upstream.URL+"/a.jpg", client, "ua", "")
	require.Error(t, result.err)
	assert.True(t, result.persistFailed)
	assert.Contains(t, result.err.Error(), "mkdir tmp")
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type errReadBody struct{}

func (errReadBody) Read([]byte) (int, error) { return 0, errors.New("stream reset") }
func (errReadBody) Close() error             { return nil }

func TestBranch_FetchAndCache_ReadSideCopyError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "image/jpeg")
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Header:     h,
			Body:       errReadBody{},
			Request:    req,
		}, nil
	})}

	fs := afero.NewMemMapFs()
	result := fetchAndCache(context.Background(), fs, t.TempDir(), "http://example.com/x.jpg", "http://example.com/x.jpg", client, "ua", "")
	require.Error(t, result.err)
	assert.False(t, result.persistFailed, "read-side failures must not mark the run as persist-failed")
	assert.Contains(t, result.err.Error(), "write temp")
}
