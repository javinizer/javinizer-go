package temp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type readFailFile struct {
	afero.File
}

func (f *readFailFile) Read(p []byte) (int, error) { return 0, errors.New("read failed") }

type openHookFs struct {
	afero.Fs
	match     func(name string) bool
	mode      string
	failAfter int32
	opens     int32
}

func (f *openHookFs) Open(name string) (afero.File, error) {
	if f.match(name) && atomic.AddInt32(&f.opens, 1) > f.failAfter {
		if f.mode == "error" {
			return nil, errors.New("open denied")
		}
		real, err := f.Fs.Open(name)
		if err != nil {
			return nil, err
		}
		return &readFailFile{File: real}, nil
	}
	return f.Fs.Open(name)
}

func newImageCacheDeps(t *testing.T, fs afero.Fs) *core.APIDeps {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 1
	cfg.System.TempDir = t.TempDir()
	deps := &core.APIDeps{Fs: fs}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)
	return deps
}

func serveImageRequest(t *testing.T, deps *core.APIDeps, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(rawURL), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func seedCacheEntry(t *testing.T, fs afero.Fs, tempDir, normalizedURL, content string, ageHours int) string {
	t.Helper()
	shardDir, hashPrefix := pathFor(tempDir, normalizedURL)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	entry := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(fs, entry, []byte(content), 0o644))
	if ageHours > 0 {
		old := time.Now().Add(-time.Duration(ageHours) * time.Hour)
		require.NoError(t, fs.Chtimes(entry, old, old))
	}
	return entry
}

func testUpstream(payload string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(payload))
	}))
}

func TestBranch_FreshCacheHit_CopyFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mem := afero.NewMemMapFs()
	rawURL := "http://93.184.216.34/fresh.jpg"
	deps := newImageCacheDeps(t, mem)
	entry := seedCacheEntry(t, mem, depsTempDir(t, deps), rawURL, "cached", 0)

	hooked := &openHookFs{Fs: mem, match: func(name string) bool { return name == entry }, mode: "readfail"}
	deps.Fs = hooked

	w := serveImageRequest(t, deps, rawURL)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func depsTempDir(t *testing.T, deps *core.APIDeps) string {
	t.Helper()
	rt := testkit.GetTestRuntime(deps)
	return rt.GetAPIConfig().TempConfig().TempDir
}

func TestBranch_CachedResultBranch_CopyFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("cached-body")
	t.Cleanup(upstream.Close)

	hooked := &openHookFs{
		Fs:   afero.NewMemMapFs(),
		mode: "readfail",
		match: func(name string) bool {
			return strings.HasSuffix(name, ".jpg") && !strings.Contains(name, "/.tmp/")
		},
	}
	deps := newImageCacheDeps(t, hooked)

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestBranch_RenameFailure_ServesFromTemp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("temp-body")
	t.Cleanup(upstream.Close)

	fs := &renameAlwaysFailFs{Fs: afero.NewMemMapFs()}
	deps := newImageCacheDeps(t, fs)

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, jpegStr("temp-body"), w.Body.String())
	assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))
}

func TestBranch_RenameFailure_TempUnreadable_FallsBackToSharedMemoryServe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("temp-body")
	t.Cleanup(upstream.Close)

	hooked := &openHookFs{
		Fs:        afero.NewMemMapFs(),
		mode:      "error",
		failAfter: 1,
		match:     isTmpCacheFile,
	}
	fs := &renameAlwaysFailFs{Fs: hooked}
	deps := newImageCacheDeps(t, fs)

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, jpegStr("temp-body"), w.Body.String(), "unreadable temp artifact degrades to shared in-memory bytes")
}

func TestBranch_RenameFailure_TempReadbackFails_FallsBackToSharedMemoryServe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("temp-body")
	t.Cleanup(upstream.Close)

	hooked := &openHookFs{
		Fs:        afero.NewMemMapFs(),
		mode:      "readfail",
		failAfter: 1,
		match:     isTmpCacheFile,
	}
	fs := &renameAlwaysFailFs{Fs: hooked}
	deps := newImageCacheDeps(t, fs)

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, jpegStr("temp-body"), w.Body.String(), "temp readback failure degrades to shared in-memory bytes")
}

func isTmpCacheFile(name string) bool {
	return filepath.Base(filepath.Dir(name)) == ".tmp"
}

func TestBranch_StaleEntry_OpenFailureFallsToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mem := afero.NewMemMapFs()
	rawURL := "http://127.0.0.1:9/stale-open.jpg"
	hooked := &openHookFs{Fs: mem, mode: "error", match: func(name string) bool {
		return strings.HasSuffix(name, ".jpg")
	}}
	deps := newImageCacheDeps(t, hooked)
	seedCacheEntry(t, mem, depsTempDir(t, deps), rawURL, "stale", 2)

	w := serveImageRequest(t, deps, rawURL)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestBranch_StaleEntry_CopyFailureFallsToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mem := afero.NewMemMapFs()
	rawURL := "http://127.0.0.1:9/stale-copy.jpg"
	hooked := &openHookFs{Fs: mem, mode: "readfail", match: func(name string) bool {
		return strings.HasSuffix(name, ".jpg")
	}}
	deps := newImageCacheDeps(t, hooked)
	seedCacheEntry(t, mem, depsTempDir(t, deps), rawURL, "stale", 2)

	w := serveImageRequest(t, deps, rawURL)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func newGinTestContext(w *httptest.ResponseRecorder) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/temp/image", nil)
	return c
}

func TestBranch_Uncached_CreateRequestError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c := newGinTestContext(w)
	serveTempImageUncached(c, &core.TempNarrowConfig{}, "http://exa mple.invalid/x.jpg", "http://93.184.216.34/x.jpg")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "failed to create request")
}

func TestBranch_Uncached_UpstreamFetchError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	srv := testUpstream("x")
	deadURL := srv.URL + "/gone.jpg"
	srv.Close()

	w := httptest.NewRecorder()
	c := newGinTestContext(w)
	serveTempImageUncached(c, &core.TempNarrowConfig{}, deadURL, deadURL)
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "failed to fetch image")
}

func TestBranch_Uncached_EmptyContentTypeUsesDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	w := httptest.NewRecorder()
	c := newGinTestContext(w)
	serveTempImageUncached(c, &core.TempNarrowConfig{}, srv.URL+"/x", srv.URL+"/x")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, defaultContentType, w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
}

func TestBranch_Uncached_CopyFailureAborts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write(jpegBytes("abc"))
	}))
	t.Cleanup(srv.Close)

	w := httptest.NewRecorder()
	c := newGinTestContext(w)
	serveTempImageUncached(c, &core.TempNarrowConfig{}, srv.URL+"/x", srv.URL+"/x")
	assert.Equal(t, jpegStr("abc"), w.Body.String())
}

func TestBranch_PersistFailure_FallsBackToUncached(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("fallback-body")
	t.Cleanup(upstream.Close)

	fs := &mkdirSuffixFailFs{Fs: afero.NewMemMapFs(), suffix: ".tmp"}
	deps := newImageCacheDeps(t, fs)

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, jpegStr("fallback-body"), w.Body.String())
	assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"), "degraded fallback must preserve the validated media type")
}

func TestBranch_NonImageUpstream_ServesStaleEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(jpegBytes("<html>bot check</html>"))
	}))
	t.Cleanup(upstream.Close)

	mem := afero.NewMemMapFs()
	rawURL := upstream.URL + "/blocked.jpg"
	deps := newImageCacheDeps(t, mem)
	seedCacheEntry(t, mem, depsTempDir(t, deps), rawURL, "stale-image-bytes", 2)

	w := serveImageRequest(t, deps, rawURL)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "stale-image-bytes", w.Body.String())
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
}

func TestBranch_FreshCacheHit_MaxAgeBoundedByRemainingTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mem := afero.NewMemMapFs()
	rawURL := "http://93.184.216.34/bounded.jpg"
	deps := newImageCacheDeps(t, mem)

	entry := seedCacheEntry(t, mem, depsTempDir(t, deps), rawURL, "fresh", 0)
	halfTTLAgo := time.Now().Add(-30 * time.Minute)
	require.NoError(t, mem.Chtimes(entry, halfTTLAgo, halfTTLAgo))

	w := serveImageRequest(t, deps, rawURL)
	assert.Equal(t, http.StatusOK, w.Code)

	cc := w.Header().Get("Cache-Control")
	require.True(t, strings.HasPrefix(cc, "private, max-age="), "unexpected Cache-Control %q", cc)
	maxAge, err := strconv.Atoi(strings.TrimPrefix(cc, "private, max-age="))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, maxAge, 1750)
	assert.LessOrEqual(t, maxAge, 1800, "browser max-age must not outlive the remaining server TTL")
}

func TestBranch_FreshCacheHit_MaxAgeCappedAt24h(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mem := afero.NewMemMapFs()
	rawURL := "http://93.184.216.34/capped.jpg"

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 168
	cfg.System.TempDir = t.TempDir()
	deps := &core.APIDeps{Fs: mem}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)

	seedCacheEntry(t, mem, cfg.System.TempDir, rawURL, "fresh", 0)

	w := serveImageRequest(t, deps, rawURL)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "private, max-age=86400", w.Header().Get("Cache-Control"))
}

func TestBranch_QueryOrderDistinctCacheKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes("body:" + r.URL.RawQuery))
	}))
	t.Cleanup(upstream.Close)

	deps := newImageCacheDeps(t, afero.NewMemMapFs())

	w1 := serveImageRequest(t, deps, upstream.URL+"/img?a=1&b=2")
	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, jpegStr("body:a=1&b=2"), w1.Body.String())

	w2 := serveImageRequest(t, deps, upstream.URL+"/img?b=2&a=1")
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, jpegStr("body:b=2&a=1"), w2.Body.String(), "differently-ordered query strings must not share a cache entry")
}

func TestBranch_CachedFill_MaxAgeBoundedByConfigTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("fill-body")
	t.Cleanup(upstream.Close)

	deps := newImageCacheDeps(t, afero.NewMemMapFs())

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusOK, w.Code)

	cc := w.Header().Get("Cache-Control")
	require.True(t, strings.HasPrefix(cc, "private, max-age="), "unexpected Cache-Control %q", cc)
	maxAge, err := strconv.Atoi(strings.TrimPrefix(cc, "private, max-age="))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, maxAge, 3550)
	assert.LessOrEqual(t, maxAge, 3600, "cache-fill response must not outlive the configured TTL")
}

func TestBranch_CachedFill_MaxAgeCappedAt24h(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("fill-body")
	t.Cleanup(upstream.Close)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 168
	cfg.System.TempDir = t.TempDir()
	deps := &core.APIDeps{Fs: afero.NewMemMapFs()}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "private, max-age=86400", w.Header().Get("Cache-Control"))
}

func TestBranch_PersistFailure_SharedAcrossWaiters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes("shared-body"))
	}))
	t.Cleanup(upstream.Close)

	fs := &mkdirSuffixFailFs{Fs: afero.NewMemMapFs(), suffix: ".tmp"}
	deps := newImageCacheDeps(t, fs)
	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	const waiters = 5
	results := make([]*httptest.ResponseRecorder, waiters)
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(upstream.URL+"/img.jpg"), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results[idx] = w
		}(i)
	}
	wg.Wait()

	hitCount := atomic.LoadInt32(&hits)
	assert.Equal(t, int32(1), hitCount, "N concurrent waiters cost exactly one upstream fetch; the drained response is shared in memory")
	for _, w := range results {
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, jpegStr("shared-body"), w.Body.String())
	}
}

func TestBranch_PersistFailure_RefetchFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(jpegBytes("once"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	hooked := &openHookFs{Fs: afero.NewMemMapFs(), mode: "error", match: isTmpCacheFile}
	deps := newImageCacheDeps(t, hooked)

	w := serveImageRequest(t, deps, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "failed to fetch image")
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits), "in-flight bytes already consumed by the failed persist; exactly one shared refetch is attempted")
}

func TestBranch_PersistFailureWithStale_PrefersFreshMemoryBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	upstream := testUpstream("fresh-fallback-body")
	t.Cleanup(upstream.Close)

	mem := afero.NewMemMapFs()
	fs := &mkdirSuffixFailFs{Fs: mem, suffix: ".tmp"}
	deps := newImageCacheDeps(t, fs)
	rawURL := upstream.URL + "/gradual.jpg"
	seedCacheEntry(t, mem, depsTempDir(t, deps), rawURL, "stale-should-lose", 2)

	w := serveImageRequest(t, deps, rawURL)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, jpegStr("fresh-fallback-body"), w.Body.String(), "successful degraded fetch must beat the stale disk entry")
	assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))
}

func TestRedactImageURL(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"strips userinfo and query", "https://user:pass@cdn.example.com/img.jpg?sig=abc123&exp=9", "https://cdn.example.com/img.jpg"},
		{"plain url unchanged", "https://cdn.example.com/img.jpg", "https://cdn.example.com/img.jpg"},
		{"fragment kept, query stripped", "https://cdn.example.com/img.jpg?sig=x#frag", "https://cdn.example.com/img.jpg#frag"},
		{"invalid url redacted wholesale", "://nope", "<invalid-url>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactImageURL(tc.in)
			assert.Equal(t, tc.want, got)
			assert.NotContains(t, got, "sig=")
			assert.NotContains(t, got, "user:pass")
		})
	}
}

func TestBranch_CacheFill_QuotaEnforcedAfterCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	payload := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 600_000)...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(upstream.Close)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 24
	cfg.System.ImageCacheMaxSizeMB = 1
	cfg.System.TempDir = t.TempDir()
	fs := afero.NewMemMapFs()
	deps := &core.APIDeps{Fs: fs}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)

	w1 := serveImageRequest(t, deps, upstream.URL+"/first.jpg")
	require.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, len(payload), w1.Body.Len())

	w2 := serveImageRequest(t, deps, upstream.URL+"/second.jpg")
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, len(payload), w2.Body.Len())

	var total int64
	_ = afero.Walk(fs, "/", func(_ string, info os.FileInfo, werr error) error {
		if werr == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	assert.LessOrEqual(t, total, int64(1<<20), "after the second commit the on-disk cache must not exceed the configured quota")
}

func TestBranch_OverQuotaFill_AllWaitersServedFromSharedEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	payload := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 1_200_000)...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(upstream.Close)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 24
	cfg.System.ImageCacheMaxSizeMB = 1
	cfg.System.TempDir = t.TempDir()
	fs := afero.NewMemMapFs()
	deps := &core.APIDeps{Fs: fs}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	const waiters = 3
	results := make([]*httptest.ResponseRecorder, waiters)
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(upstream.URL+"/big.jpg"), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results[idx] = w
		}(i)
	}
	wg.Wait()

	for _, w := range results {
		require.Equal(t, http.StatusOK, w.Code, "every coalesced waiter must serve; eviction must not delete the shared fill")
		assert.Equal(t, len(payload), w.Body.Len())
	}
}
