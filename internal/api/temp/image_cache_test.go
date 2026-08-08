package temp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
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

const testJpegMagic = "\xff\xd8\xff\xe0"

func jpegBytes(s string) []byte { return append([]byte(testJpegMagic), s...) }
func jpegStr(s string) string   { return testJpegMagic + s }

func newCacheTestDeps(t *testing.T, enabled bool, ttlHours int) (*core.APIDeps, afero.Fs, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = enabled
	cfg.System.ImageCacheTTLHours = ttlHours
	cfg.System.TempDir = tempDir
	fs := afero.NewMemMapFs()
	deps := &core.APIDeps{Fs: fs}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)
	return deps, fs, tempDir
}

func cacheTestRouter(deps *core.APIDeps) *gin.Engine {
	r := gin.New()
	r.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))
	return r
}

func requestImage(router *gin.Engine, rawURL string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(rawURL), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func lookupPublicIP(host string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("8.8.8.8")}, nil
}

func lookupOffline(host string) ([]net.IP, error) {
	return nil, fmt.Errorf("DNS resolution failed (offline)")
}

func TestImageCache_FreshHitServesFromDiskNoFetch(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	var fetchCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes("fake-png-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w1 := requestImage(router, upstream.URL+"/img.png")
	require.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount), "first request should fetch")

	w2 := requestImage(router, upstream.URL+"/img.png")
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "private, max-age=86400", w2.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", w2.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount), "second request should NOT fetch")
	assert.Contains(t, w2.Body.String(), "fake-png-data")
}

func TestImageCache_OfflineHitServedFromDisk(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("offline-test-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w1 := requestImage(router, upstream.URL+"/img.jpg")
	require.Equal(t, http.StatusOK, w1.Code)
	assert.Contains(t, w1.Body.String(), "offline-test-data")

	cleanup2 := ssrf.SetLookupIPForTest(lookupOffline)
	t.Cleanup(cleanup2)

	w2 := requestImage(router, upstream.URL+"/img.jpg")
	require.Equal(t, http.StatusOK, w2.Code, "offline hit should serve from cache")
	assert.Equal(t, "private, max-age=86400", w2.Header().Get("Cache-Control"))
	assert.Contains(t, w2.Body.String(), "offline-test-data")
}

func TestImageCache_DisabledNoDiskWrites(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("uncached"))
	}))
	t.Cleanup(upstream.Close)

	deps, fs, tempDir := newCacheTestDeps(t, false, 0)
	router := cacheTestRouter(deps)

	w := requestImage(router, upstream.URL+"/img.jpg")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))

	exists, _ := afero.DirExists(fs, filepath.Join(tempDir, "image-cache"))
	assert.False(t, exists, "no cache dir should be created when disabled")
}

func TestImageCache_ContentTypeNormalization(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp; charset=UTF-8")
		w.Write(webpBytes("webp-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w := requestImage(router, upstream.URL+"/img.webp")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/webp", w.Header().Get("Content-Type"), "params stripped on miss")

	w2 := requestImage(router, upstream.URL+"/img.webp")
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "image/webp", w2.Header().Get("Content-Type"), "normalized on hit")
}

func TestImageCache_SVGRejectedOnMiss(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(jpegBytes("<svg></svg>"))
	}))
	t.Cleanup(upstream.Close)

	deps, fs, tempDir := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w := requestImage(router, upstream.URL+"/img.svg")
	require.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "failed to fetch image")

	exists, _ := afero.Exists(fs, filepath.Join(tempDir, "image-cache"))
	assert.False(t, exists, "rejected SVG must not be persisted")
}

func TestImageCache_StaleIfErrorServesStaleBytes(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("stale-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, fs, tempDir := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)
	imgURL := upstream.URL + "/img.jpg"

	w1 := requestImage(router, imgURL)
	require.Equal(t, http.StatusOK, w1.Code)

	shardDir, hashPrefix := pathFor(tempDir, imgURL)
	entryPath, _, found := resolveEntry(fs, shardDir, hashPrefix)
	require.True(t, found, "cache entry should exist")
	pastTime := time.Now().Add(-200 * time.Hour)
	require.NoError(t, fs.Chtimes(entryPath, pastTime, pastTime))

	upstream.Close()

	w2 := requestImage(router, imgURL)
	require.Equal(t, http.StatusOK, w2.Code, "stale entry should be served on re-fetch failure")
	assert.Equal(t, "no-cache", w2.Header().Get("Cache-Control"))
	assert.Contains(t, w2.Body.String(), "stale-data")
	assert.Equal(t, "nosniff", w2.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "image/jpeg", w2.Header().Get("Content-Type"), "stale serve should have proper MIME type, not extension")
}

func TestImageCache_ConcurrentCoalescing(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	var fetchCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("coalesced"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := requestImage(router, upstream.URL+"/img.jpg")
			assert.Equal(t, http.StatusOK, w.Code)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount), "only one fetch should occur")
}

func TestImageCache_AbsentEntryFetchFailureReturns502(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w := requestImage(router, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusBadGateway, w.Code, "absent entry + non-200 should return 502")
}

func TestImageCache_AbsentEntrySSRFFailureReturns403(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupOffline)
	t.Cleanup(cleanup)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w := requestImage(router, "http://nonexistent.invalid/img.jpg")
	assert.Equal(t, http.StatusForbidden, w.Code, "absent entry + SSRF/DNS failure should return 403")
}

func TestImageCache_SessionParamDoesNotFragment(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	var fetchCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("session-test"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	imgURL := upstream.URL + "/img.jpg"

	req1 := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(imgURL)+"&session=abc", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(imgURL)+"&session=xyz", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount), "different session params should not fragment the cache")
}

func TestImageCache_AvifPreserved(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/avif")
		w.Write(avifBytes("avif-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w := requestImage(router, upstream.URL+"/img.avif")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/avif", w.Header().Get("Content-Type"))

	w2 := requestImage(router, upstream.URL+"/img.avif")
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "image/avif", w2.Header().Get("Content-Type"))
}

func TestImageCache_ApngMappedToPng(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/apng")
		w.Write(pngBytes("apng-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w := requestImage(router, upstream.URL+"/img.apng")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"), "apng should be mapped to png")

	w2 := requestImage(router, upstream.URL+"/img.apng")
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "image/png", w2.Header().Get("Content-Type"))
}

func TestImageCache_StaleRefreshOverwritesEntry(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	var data atomic.Value
	data.Store(jpegBytes("old-data"))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(data.Load().([]byte))
	}))
	t.Cleanup(upstream.Close)

	deps, fs, tempDir := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)
	imgURL := upstream.URL + "/img.jpg"

	w1 := requestImage(router, imgURL)
	require.Equal(t, http.StatusOK, w1.Code)
	assert.Contains(t, w1.Body.String(), "old-data")

	shardDir, hashPrefix := pathFor(tempDir, imgURL)
	entryPath, _, found := resolveEntry(fs, shardDir, hashPrefix)
	require.True(t, found)
	pastTime := time.Now().Add(-200 * time.Hour)
	require.NoError(t, fs.Chtimes(entryPath, pastTime, pastTime))

	data.Store(jpegBytes("new-data"))

	w2 := requestImage(router, imgURL)
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "new-data", "should serve refreshed bytes")
}

func TestImageCache_OversizedReturns502(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(make([]byte, maxImageProxyResponseSize+1))
	}))
	t.Cleanup(upstream.Close)

	deps, fs, tempDir := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)

	w := requestImage(router, upstream.URL+"/big.jpg")
	assert.Equal(t, http.StatusBadGateway, w.Code, "oversized response should return 502")

	tmpEntries, _ := afero.ReadDir(fs, filepath.Join(tempDir, "image-cache", ".tmp"))
	assert.Empty(t, tmpEntries, "oversized temp file should be cleaned up")
}

func TestImageCache_SSRFFailureWithStaleServesStale(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("cached-while-online"))
	}))
	t.Cleanup(upstream.Close)

	deps, fs, tempDir := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)
	imgURL := upstream.URL + "/img.jpg"

	w1 := requestImage(router, imgURL)
	require.Equal(t, http.StatusOK, w1.Code)

	shardDir, hashPrefix := pathFor(tempDir, imgURL)
	entryPath, _, found := resolveEntry(fs, shardDir, hashPrefix)
	require.True(t, found)
	pastTime := time.Now().Add(-200 * time.Hour)
	require.NoError(t, fs.Chtimes(entryPath, pastTime, pastTime))

	cleanup2 := ssrf.SetLookupIPForTest(lookupOffline)
	t.Cleanup(cleanup2)

	w2 := requestImage(router, imgURL)
	require.Equal(t, http.StatusOK, w2.Code, "stale entry + SSRF failure should serve stale")
	assert.Equal(t, "no-cache", w2.Header().Get("Cache-Control"))
	assert.Contains(t, w2.Body.String(), "cached-while-online")
	assert.Equal(t, "image/jpeg", w2.Header().Get("Content-Type"), "stale serve should have proper MIME type")
}

func TestImageCache_LeaderDisconnectDoesNotCancelSharedFetch(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	var fetchStarted int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchStarted, 1)
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("leader-disconnect-test"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newCacheTestDeps(t, true, 168)
	router := cacheTestRouter(deps)
	imgURL := upstream.URL + "/img.jpg"

	ctx, cancel := context.WithCancel(context.Background())
	req1 := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(imgURL), nil).WithContext(ctx)
	w1 := httptest.NewRecorder()

	go func() {
		router.ServeHTTP(w1, req1)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	w2 := requestImage(router, imgURL)
	require.Equal(t, http.StatusOK, w2.Code, "follower should still get the image after leader disconnect")
	assert.Contains(t, w2.Body.String(), "leader-disconnect-test")
}

const testPngMagic = "\x89PNG\r\n\x1a\n"
const testWebpHeader = "RIFF\x00\x00\x00\x00WEBP"
const testAvifHeader = "\x00\x00\x00\x1cftypavif"

func pngBytes(s string) []byte  { return append([]byte(testPngMagic), s...) }
func webpBytes(s string) []byte { return append([]byte(testWebpHeader), s...) }
func avifBytes(s string) []byte { return append([]byte(testAvifHeader), s...) }
