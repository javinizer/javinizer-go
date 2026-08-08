package temp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	file, ct, _, state := get(fs, cacheDir, rawURL, time.Hour)
	assert.Nil(t, file)
	assert.Empty(t, ct)
	assert.Equal(t, CacheAbsent, state)
}

func TestBranch_FetchAndCache_CreateRequestError(t *testing.T) {
	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), "key", "://no-scheme", client, "ua", "", 0)
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
		_, _ = w.Write(jpegBytes("img"))
	}))
	t.Cleanup(upstream.Close)

	fs := &mkdirSuffixFailFs{Fs: afero.NewMemMapFs(), suffix: ".tmp"}
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/a.jpg", upstream.URL+"/a.jpg", client, "ua", "", 0)
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
	result := fetchAndCache(context.Background(), fs, t.TempDir(), "http://example.com/x.jpg", "http://example.com/x.jpg", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.False(t, result.persistFailed, "read-side failures must not mark the run as persist-failed")
	assert.Contains(t, result.err.Error(), "write temp")
}

func TestBranch_FetchAndCache_RejectsNonImageContentType(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(jpegBytes("<html>challenge</html>"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/img", upstream.URL+"/img", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "non-image content type")
	assert.False(t, result.persistFailed)
	assert.Empty(t, result.cachedPath)

	entries, _ := afero.ReadDir(fs, "/")
	assert.Empty(t, entries, "nothing must be written for non-image responses")
}

func TestBranch_FetchAndCache_RejectsHeaderlessHTML(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "")
		_, _ = w.Write([]byte("<html><body>bot check</body></html>"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/img", upstream.URL+"/img", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "uncacheable content")
	assert.Empty(t, result.cachedPath)
}

func fakeImageResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	h := make(http.Header)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func TestBranch_FetchBodyToMemory_Success(t *testing.T) {
	var gotReferer string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotReferer = req.Header.Get("Referer")
		return fakeImageResponse(req, http.StatusOK, "image/png", pngBytes("png-bytes")), nil
	})}

	body, gotCT, err := fetchBodyToMemory(context.Background(), client, "http://example.com/a.png", "ua", "https://ref.example/x")
	require.NoError(t, err)
	assert.Equal(t, pngBytes("png-bytes"), body)
	assert.Equal(t, "image/png", gotCT)
	assert.Equal(t, "https://ref.example/x", gotReferer)
}

func TestBranch_FetchBodyToMemory_CreateRequestError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("must not be called")
	})}
	_, _, err := fetchBodyToMemory(context.Background(), client, "://bad-url", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create request")
}

func TestBranch_FetchBodyToMemory_DoError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}
	_, _, err := fetchBodyToMemory(context.Background(), client, "http://example.com/a.png", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch")
}

func TestBranch_FetchBodyToMemory_Non200(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fakeImageResponse(req, http.StatusServiceUnavailable, "image/png", []byte("x")), nil
	})}
	_, _, err := fetchBodyToMemory(context.Background(), client, "http://example.com/a.png", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-200")
}

func TestBranch_FetchBodyToMemory_RejectsNonImageCT(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fakeImageResponse(req, http.StatusOK, "text/html", []byte("<html>")), nil
	})}
	_, _, err := fetchBodyToMemory(context.Background(), client, "http://example.com/a", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncacheable content type")
}

func TestBranch_FetchBodyToMemory_OversizeRejected(t *testing.T) {
	big := make([]byte, maxImageProxyResponseSize+16)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fakeImageResponse(req, http.StatusOK, "image/jpeg", big), nil
	})}
	_, _, err := fetchBodyToMemory(context.Background(), client, "http://example.com/big.jpg", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestBranch_FetchBodyToMemory_HeaderlessSniffOK(t *testing.T) {
	pngMagic := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 300)...)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fakeImageResponse(req, http.StatusOK, "", pngMagic), nil
	})}
	body, gotCT, err := fetchBodyToMemory(context.Background(), client, "http://example.com/raw", "ua", "")
	require.NoError(t, err)
	assert.Equal(t, pngMagic, body)
	assert.Equal(t, "image/png", gotCT, "sniffed media type must be returned with the body")
}

func TestBranch_FetchBodyToMemory_HeaderlessSniffRejectsText(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fakeImageResponse(req, http.StatusOK, "", []byte("plain text challenge")), nil
	})}
	_, _, err := fetchBodyToMemory(context.Background(), client, "http://example.com/raw", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncacheable content type")
}

func TestBranch_FetchBodyToMemory_ReadError(t *testing.T) {
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
	_, _, err := fetchBodyToMemory(context.Background(), client, "http://example.com/x.jpg", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read body")
}

func TestBranch_FetchAndCache_HeaderlessAvisBrandAccepted(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	avisBytes := append([]byte("\x00\x00\x00\x1cftypavis"), make([]byte, 64)...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = w.Write(avisBytes)
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/clip.avif", upstream.URL+"/clip.avif", client, "ua", "", 0)
	require.NoError(t, result.err)
	assert.Equal(t, "image/avif", result.contentType)
	assert.Contains(t, result.cachedPath, ".avif")
}

func TestBranch_FetchAndCache_VerifyTempOpenFailure(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes("verify"))
	}))
	t.Cleanup(upstream.Close)

	hooked := &openHookFs{Fs: afero.NewMemMapFs(), mode: "error", match: isTmpCacheFile}
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), hooked, t.TempDir(), upstream.URL+"/x.jpg", upstream.URL+"/x.jpg", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.True(t, result.persistFailed)
	assert.Contains(t, result.err.Error(), "verify temp")
}

func TestBranch_FetchAndCache_RejectsDeclaredImageButGarbageBytes(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("this is not actually an image"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/fake.jpg", upstream.URL+"/fake.jpg", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "invalid image content")
	assert.Empty(t, result.cachedPath)
	var files []string
	_ = afero.Walk(fs, "/", func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			files = append(files, info.Name())
		}
		return nil
	})
	assert.Empty(t, files, "rejected content must not leave any persisted file")
}

func TestBranch_FetchAndCache_HeaderlessBinaryGarbageRejected(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	junk := make([]byte, 128)
	for i := range junk {
		junk[i] = byte(i*37 + 11)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = w.Write(junk)
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/blob", upstream.URL+"/blob", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "uncacheable content in headerless response")
}

func TestBranch_FetchAndCache_OctetStreamCT_SniffedAsImage(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(jpegBytes("octet-body"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "ua", "", 0)
	require.NoError(t, result.err)
	assert.Equal(t, "image/jpeg", result.contentType)
	assert.Contains(t, result.cachedPath, ".jpg")
}

func TestBranch_FetchAndCache_OctetStreamCT_WithGarbageRejected(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("<html>actually an error page</html>"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/x", upstream.URL+"/x", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "uncacheable content")
	assert.Empty(t, result.cachedPath)
	assert.Empty(t, result.body)
}

func TestBranch_FetchBodyToMemory_OctetStreamSniffed(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fakeImageResponse(req, http.StatusOK, "application/octet-stream", jpegBytes("via-sniff")), nil
	})}
	body, gotCT, err := fetchBodyToMemory(context.Background(), client, "http://example.com/bin", "ua", "")
	require.NoError(t, err)
	assert.Equal(t, jpegBytes("via-sniff"), body)
	assert.Equal(t, "image/jpeg", gotCT)
}

func TestBranch_FetchBodyToMemory_RejectsGarbageWithImageCT(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fakeImageResponse(req, http.StatusOK, "image/jpeg", []byte("totally not a jpeg")), nil
	})}
	_, _, err := fetchBodyToMemory(context.Background(), client, "http://example.com/x", "ua", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncacheable content type")
}

func TestBranch_FetchAndCache_PersistFail_DrainUnusable_RefetchEmpty(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	fs := &atomicStubFs{Fs: afero.NewMemMapFs(), mkdirErr: errors.New("read-only fs")}
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/empty.jpg", upstream.URL+"/empty.jpg", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.True(t, result.persistFailed)
	assert.Empty(t, result.body, "an empty upstream body cannot be salvaged for degraded serving")
}

func TestBranch_FetchAndCache_PersistFail_DrainRejectsGarbage(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("declared image, actually junk"))
	}))
	t.Cleanup(upstream.Close)

	fs := &atomicStubFs{Fs: afero.NewMemMapFs(), mkdirErr: errors.New("read-only fs")}
	client := ssrf.NewSSRFSafeClient(30 * time.Second)
	result := fetchAndCache(context.Background(), fs, t.TempDir(), upstream.URL+"/junk.jpg", upstream.URL+"/junk.jpg", client, "ua", "", 0)
	require.Error(t, result.err)
	assert.True(t, result.persistFailed)
	assert.Empty(t, result.body, "junk drained from the failed persist must not be served")
}

func TestBranch_EvictImageCacheToSize(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := t.TempDir()
	seed := filepath.Join(dir, "image-cache", "ab", "a.jpg")
	require.NoError(t, mem.MkdirAll(filepath.Dir(seed), 0o755))
	require.NoError(t, afero.WriteFile(mem, seed, make([]byte, 2048), 0o644))

	evictImageCacheToSize(mem, dir, 0)
	exists, _ := afero.Exists(mem, seed)
	assert.True(t, exists, "zero quota is a no-op")

	evictImageCacheToSize(mem, dir, 1)
	exists, _ = afero.Exists(mem, seed)
	assert.True(t, exists, "2 KB stays under a 1 MB quota")

	big := filepath.Join(dir, "image-cache", "cd", "b.jpg")
	require.NoError(t, mem.MkdirAll(filepath.Dir(big), 0o755))
	require.NoError(t, afero.WriteFile(mem, big, make([]byte, 2<<20), 0o644))
	evictImageCacheToSize(mem, dir, 1)
	exists, _ = afero.Exists(mem, seed)
	assert.False(t, exists, "oldest entry evicted once over quota")

	broken := &openErrFs{Fs: mem, target: filepath.Join(dir, "image-cache")}
	evictImageCacheToSize(broken, dir, 1)
}
