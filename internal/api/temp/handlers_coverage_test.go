package temp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHandlerCoverageDeps(t *testing.T, enabled bool, ttlHours int) (*core.APIDeps, afero.Fs, string) {
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

func handlerRouter(deps *core.APIDeps) *gin.Engine {
	r := gin.New()
	r.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))
	return r
}

func handlerRequest(router *gin.Engine, rawURL string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(rawURL), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestHandlerCoverage_MissingURL(t *testing.T) {
	deps, _, _ := newHandlerCoverageDeps(t, true, 168)
	w := handlerRouter(deps)
	r := handlerRequest(w, "")
	assert.Equal(t, http.StatusBadRequest, r.Code)
}

func TestHandlerCoverage_InvalidScheme(t *testing.T) {
	deps, _, _ := newHandlerCoverageDeps(t, true, 168)
	w := handlerRouter(deps)
	r := handlerRequest(w, "ftp://example.com/img.jpg")
	assert.Equal(t, http.StatusBadRequest, r.Code)
}

func TestHandlerCoverage_MissingHost(t *testing.T) {
	deps, _, _ := newHandlerCoverageDeps(t, true, 168)
	w := handlerRouter(deps)
	r := handlerRequest(w, "http:///img.jpg")
	assert.Equal(t, http.StatusBadRequest, r.Code)
}

func TestHandlerCoverage_DisabledNoWrites(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newHandlerCoverageDeps(t, true, 0)
	w := handlerRouter(deps)
	r := handlerRequest(w, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusOK, r.Code)
	assert.Equal(t, "private, max-age=300", r.Header().Get("Cache-Control"))
	assert.NotEmpty(t, r.Body.Bytes())
}

func TestHandlerCoverage_HitCacheControl(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("hit-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newHandlerCoverageDeps(t, true, 168)
	w := handlerRouter(deps)
	r := handlerRequest(w, upstream.URL+"/img.jpg")
	require.Equal(t, http.StatusOK, r.Code)
	assert.Equal(t, "private, max-age=86400", r.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", r.Header().Get("X-Content-Type-Options"))
}

func TestHandlerCoverage_SSRFFailureNoStale(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupOffline)
	t.Cleanup(cleanup)

	deps, _, _ := newHandlerCoverageDeps(t, true, 168)
	w := handlerRouter(deps)
	r := handlerRequest(w, "http://nonexistent.invalid/img.jpg")
	assert.Equal(t, http.StatusForbidden, r.Code)
}

func TestHandlerCoverage_UncachedNo200(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newHandlerCoverageDeps(t, false, 0)
	w := handlerRouter(deps)
	r := handlerRequest(w, upstream.URL+"/img.jpg")
	assert.Equal(t, http.StatusBadGateway, r.Code)
}

func TestHandlerCoverage_UncachedSuccess(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes("png-data"))
	}))
	t.Cleanup(upstream.Close)

	deps, _, _ := newHandlerCoverageDeps(t, false, 0)
	w := handlerRouter(deps)
	r := handlerRequest(w, upstream.URL+"/img.png")
	assert.Equal(t, http.StatusOK, r.Code)
	assert.Equal(t, "image/png", r.Header().Get("Content-Type"))
}

func TestHandlerCoverage_TempPathOpenError(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("data"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 168
	cfg.System.TempDir = tempDir

	openFailingFs := &tempOpenFailFs{Fs: fs}
	openFailingFs.tempDir = tempDir
	deps := &core.APIDeps{Fs: openFailingFs}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)

	w := handlerRouter(deps)
	r := handlerRequest(w, upstream.URL+"/img.jpg")
	// fetchAndCache writes to temp successfully (Create works), then rename succeeds,
	// then the handler tries to Open the cached file which fails.
	assert.Equal(t, http.StatusBadGateway, r.Code)
	assert.Contains(t, r.Body.String(), "failed to open")
}

type tempOpenFailFs struct {
	afero.Fs
	tempDir string
}

func (f *tempOpenFailFs) Open(name string) (afero.File, error) {
	if filepath.Base(name) == "" || len(name) == 0 {
		return f.Fs.Open(name)
	}
	if filepath.Ext(name) == ".jpg" {
		return nil, errors.New("open failed")
	}
	return f.Fs.Open(name)
}
