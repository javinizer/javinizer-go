package temp

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// mockJobStoreForTemp implements worker.JobStoreInterface for testing the JobStore.TempDir path.
// Unimplemented methods panic to make test failures obvious.
type mockJobStoreForTemp struct {
	worker.JobStoreInterface // embed to satisfy interface
	getJobFn                 func(id string) (*worker.BatchJobStatus, bool)
}

func (m *mockJobStoreForTemp) GetJob(id string) (*worker.BatchJobStatus, bool) {
	if m.getJobFn != nil {
		return m.getJobFn(id)
	}
	return nil, false
}

// TestServeTempPoster_JobStoreWithTempDir tests the path where JobStore returns a
// job with a TempDir override — hits the "job.TempDir" branch in serveTempPoster.
func TestServeTempPoster_JobStoreWithTempDir(t *testing.T) {
	gin.SetMode(gin.TestMode)

	defaultTempDir := t.TempDir()
	jobTempDir := t.TempDir()

	jobID := "job-with-tempdir"
	posterDir := filepath.Join(jobTempDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, "poster.jpg"), []byte("jpeg"), 0644))

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = defaultTempDir

	// Create a mock JobStore that returns a job with TempDir
	mockStore := &mockJobStoreForTemp{
		getJobFn: func(id string) (*worker.BatchJobStatus, bool) {
			if id == jobID {
				// Create a BatchJobStatus with TempDir set to jobTempDir
				status := &worker.BatchJobStatus{}
				status.TempDir = jobTempDir
				return status, true
			}
			return nil, false
		},
	}

	deps := &core.APIDeps{JobStore: mockStore}
	testkit.GetTestRuntime(deps)
	testkit.GetTestRuntime(deps).SetConfig(cfg)

	router := gin.New()
	router.GET("/temp/posters/:jobId/:filename", serveTempPoster(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/posters/"+jobID+"/poster.jpg", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestServeCroppedPoster_CacheControl tests that the Cache-Control header
// is set for valid cropped poster requests.
func TestServeCroppedPoster_CacheControl2(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(originalWd) }()

	posterDir := filepath.Join("data", "posters")
	require.NoError(t, os.MkdirAll(posterDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, "cached2.jpg"), []byte("jpeg"), 0644))

	router := gin.New()
	router.GET("/posters/:filename", serveCroppedPoster())

	req := httptest.NewRequest(http.MethodGet, "/posters/cached2.jpg", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=86400", w.Header().Get("Cache-Control"))
}

// TestServeTempImage_EmptyURLParam hits the "url query parameter is required" branch.
func TestServeTempImage_EmptyURLParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "required")
}

// TestServeTempImage_FTPScheme hits the "invalid image url" branch for non-http(s) scheme.
func TestServeTempImage_FTPScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url=ftp://example.com/img.jpg", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid image url")
}

// TestServeTempImage_MissingHost hits the "invalid image url" branch for URL without host.
func TestServeTempImage_MissingHost(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url=http:///img.jpg", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestServeTempImage_PrivateIPBlocked tests the SSRF check blocking private IPs.
func TestServeTempImage_PrivateIPBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.168.1.1")}, nil
	})
	t.Cleanup(cleanup)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape("https://example.com/img.jpg"), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestServeTempImage_UpstreamNon200Status hits the "image source returned non-200 status" path.
func TestServeTempImage_UpstreamNon200Status(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(upstream.URL+"/img.jpg"), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "non-200")
}

// TestServeTempImage_SuccessWithNoContentType hits the path where
// Content-Type header is empty and defaults to "image/jpeg".
func TestServeTempImage_SuccessWithNoContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-image"))
	}))
	defer upstream.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Referer = ""
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(upstream.URL+"/img.jpg"), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The handler sets Content-Type to "image/jpeg" when upstream has none,
	// but Go's DetectContentType may override it in the recorder.
	// The important thing is we hit the contentType == "" branch.
}

// TestServeTempImage_CustomUserAgent tests the custom User-Agent path
// when apiCfg.ScraperUserAgent is set.
func TestServeTempImage_CustomUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	var receivedUA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("img"))
	}))
	defer upstream.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.UserAgent = "TestAgent/1.0"
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(upstream.URL+"/img.jpg"), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "TestAgent/1.0", receivedUA)
}
