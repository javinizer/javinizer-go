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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func newTestDeps(cfg *config.Config) *core.APIDeps {
	deps := &core.APIDeps{}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)
	return deps
}

func TestServeTempPoster(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temp directory structure
	tempDir := t.TempDir()

	// Create temp/posters/test-job-id directory (relative to tempDir)
	jobID := "test-job-id"
	posterDir := filepath.Join(tempDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	// Create a test poster file
	posterPath := filepath.Join(posterDir, "test-poster.jpg")
	require.NoError(t, os.WriteFile(posterPath, []byte("fake jpeg data"), 0644))

	// Create deps with config that has TempDir set to tempDir
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = tempDir
	deps := newTestDeps(cfg)

	tests := []struct {
		name           string
		jobID          string
		filename       string
		expectedStatus int
	}{
		{
			name:           "valid request",
			jobID:          jobID,
			filename:       "test-poster.jpg",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "path traversal in jobID",
			jobID:          "../../../etc",
			filename:       "passwd",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "path traversal in filename",
			jobID:          jobID,
			filename:       "../../../etc/passwd",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "non-jpg extension",
			jobID:          jobID,
			filename:       "test.png",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "non-existent file",
			jobID:          jobID,
			filename:       "nonexistent.jpg",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "jobID with path separator",
			jobID:          "job/id",
			filename:       "test.jpg",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "filename with backslash",
			jobID:          jobID,
			filename:       "test\\poster.jpg",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/temp/posters/:jobId/:filename", serveTempPoster(testkit.GetTestRuntime(deps)))

			req := httptest.NewRequest(http.MethodGet, "/temp/posters/"+tt.jobID+"/"+tt.filename, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestServeTempPoster_PathTraversalDefenseInDepth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temp directory structure
	tempDir := t.TempDir()

	// Create the poster directory: tempDir/posters/jobID
	jobID := "test-job"
	posterDir := filepath.Join(tempDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	// Create a sensitive file outside the poster directory (at same level as "posters")
	sensitiveFile := filepath.Join(tempDir, "sensitive.jpg")
	require.NoError(t, os.WriteFile(sensitiveFile, []byte("sensitive data"), 0644))

	// Create deps with config that has TempDir set to tempDir
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = tempDir
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/posters/:jobId/:filename", serveTempPoster(testkit.GetTestRuntime(deps)))

	// Try to access sensitive.jpg via path traversal (../sensitive.jpg from tempDir/posters/jobID/)
	req := httptest.NewRequest(http.MethodGet, "/temp/posters/"+jobID+"/../sensitive.jpg", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 404 not found (defense-in-depth blocks the traversal)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestServeTempPoster_PosterRevisionHeader pins the stable-cache generation
// token the manual-crop client binds its measured coordinates to
// (X-Poster-Revision, echoed back as PosterCropRequest.expected_poster_revision):
// same file generation → same header; a same-URL refresh that rewrites the
// bytes → a new header; and the HEAD route (no body) carries the identical
// header so the client can read the token without re-downloading the image.
func TestServeTempPoster_PosterRevisionHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	jobID := "rev-job"
	posterDir := filepath.Join(tempDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))
	posterPath := filepath.Join(posterDir, "ABC-001-full.jpg")
	require.NoError(t, os.WriteFile(posterPath, []byte("generation-one"), 0644))

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = tempDir
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/posters/:jobId/:filename", serveTempPoster(testkit.GetTestRuntime(deps)))
	router.HEAD("/temp/posters/:jobId/:filename", serveTempPoster(testkit.GetTestRuntime(deps)))

	get := func(method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/temp/posters/"+jobID+"/ABC-001-full.jpg", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	w := get(http.MethodGet)
	require.Equal(t, http.StatusOK, w.Code)
	rev := w.Header().Get("X-Poster-Revision")
	require.NotEmpty(t, rev, "served temp posters must carry the generation token")
	assert.Regexp(t, `^\d+-\d+$`, rev, "token shape: mtime-nanoseconds + '-' + size")

	// Same generation → identical token; HEAD exposes it with an empty body.
	w2 := get(http.MethodHead)
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, rev, w2.Header().Get("X-Poster-Revision"))
	assert.Empty(t, w2.Body.String())

	// Same-URL refresh (bytes replaced under the same filename) → new token.
	require.NoError(t, os.WriteFile(posterPath, []byte("generation-two-is-longer"), 0644))
	w3 := get(http.MethodGet)
	require.Equal(t, http.StatusOK, w3.Code)
	assert.NotEqual(t, rev, w3.Header().Get("X-Poster-Revision"))
}

func TestServeCroppedPoster(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temp directory structure
	tempDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	defer func() { _ = os.Chdir(originalWd) }()

	// Create data/posters directory
	posterDir := filepath.Join("data", "posters")
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	// Create a test poster file
	posterPath := filepath.Join(posterDir, "test-cropped.jpg")
	require.NoError(t, os.WriteFile(posterPath, []byte("fake jpeg data"), 0644))

	tests := []struct {
		name           string
		filename       string
		expectedStatus int
		checkHeaders   bool
	}{
		{
			name:           "valid request",
			filename:       "test-cropped.jpg",
			expectedStatus: http.StatusOK,
			checkHeaders:   true,
		},
		{
			name:           "path traversal attempt",
			filename:       "../../../etc/passwd",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "non-jpg extension",
			filename:       "test.png",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "uppercase JPG extension",
			filename:       "nonexistent.JPG",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "non-existent file",
			filename:       "nonexistent.jpg",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "filename with path separator",
			filename:       "subdir/test.jpg",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/posters/:filename", serveCroppedPoster())

			req := httptest.NewRequest(http.MethodGet, "/posters/"+tt.filename, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkHeaders && w.Code == http.StatusOK {
				assert.Equal(t, "public, max-age=86400", w.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestServeCroppedPoster_PathTraversalDefenseInDepth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temp directory structure
	tempDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	defer func() { _ = os.Chdir(originalWd) }()

	// Create data/posters directory
	posterDir := filepath.Join("data", "posters")
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	// Create a sensitive file outside the poster directory
	sensitiveFile := filepath.Join("data", "sensitive.jpg")
	require.NoError(t, os.WriteFile(sensitiveFile, []byte("sensitive data"), 0644))

	router := gin.New()
	router.GET("/posters/:filename", serveCroppedPoster())

	// Try to access sensitive.jpg via path traversal
	req := httptest.NewRequest(http.MethodGet, "/posters/../sensitive.jpg", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 404 not found
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeTempPoster_ValidJpgExtensions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temp directory structure
	tempDir := t.TempDir()

	jobID := "test-job"
	posterDir := filepath.Join(tempDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	// Create test files with different extensions
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, "test.jpg"), []byte("jpeg"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, "test.JPG"), []byte("jpeg"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, "test.Jpg"), []byte("jpeg"), 0644))

	// Create deps with config that has TempDir set to tempDir
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = tempDir
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/posters/:jobId/:filename", serveTempPoster(testkit.GetTestRuntime(deps)))

	tests := []struct {
		filename       string
		expectedStatus int
	}{
		{"test.jpg", http.StatusOK},
		{"test.JPG", http.StatusOK},
		{"test.Jpg", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/temp/posters/"+jobID+"/"+tt.filename, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestResolveTempImageReferer(t *testing.T) {
	tests := []struct {
		name       string
		imageURL   string
		configured string
		expected   string
	}{
		{
			name:       "javbus override",
			imageURL:   "https://www.javbus.com/pics/cover/abc.jpg",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://www.javbus.com/",
		},
		{
			name:       "javdb override",
			imageURL:   "https://c0.jdbstatic.com/cover/abc.jpg",
			configured: "https://www.dmm.co.jp/",
			expected:   "https://javdb.com/",
		},
		{
			name:       "dmm override",
			imageURL:   "https://pics.dmm.co.jp/digital/video/abc/abcjp-1.jpg",
			configured: "https://example.com/",
			expected:   "https://www.dmm.co.jp/",
		},
		{
			name:       "aventertainments override",
			imageURL:   "https://imgs02.aventertainments.com/vodimages/screenshot/large/1pon_020326_001/001.webp",
			configured: "https://example.com/",
			expected:   "https://www.aventertainments.com/",
		},
		{
			name:       "caribbeancom override",
			imageURL:   "https://www.caribbeancom.com/moviepages/120614-753/images/l_l.jpg",
			configured: "https://example.com/",
			expected:   "https://www.caribbeancom.com/",
		},
		{
			name:       "configured fallback",
			imageURL:   "https://images.example.com/a.jpg",
			configured: "https://configured.example.com/",
			expected:   "https://configured.example.com/",
		},
		{
			name:       "origin fallback",
			imageURL:   "https://images.example.com/a.jpg",
			configured: "",
			expected:   "https://images.example.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTempImageReferer(tt.imageURL, tt.configured)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestServeTempImage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	tests := []struct {
		name           string
		imageURL       string
		upstreamStatus int
		expectedStatus int
	}{
		{
			name:           "valid image proxy",
			upstreamStatus: http.StatusOK,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "upstream non-200",
			upstreamStatus: http.StatusForbidden,
			expectedStatus: http.StatusBadGateway,
		},
		{
			name:           "invalid image URL",
			imageURL:       "not-a-url",
			upstreamStatus: http.StatusOK,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedReferer := "https://configured.example.com/"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, expectedReferer, r.Header.Get("Referer"))
				assert.NotEmpty(t, r.Header.Get("User-Agent"))
				w.Header().Set("Content-Type", "image/jpeg")
				w.WriteHeader(tt.upstreamStatus)
				_, _ = w.Write([]byte("fake-image"))
			}))
			defer upstream.Close()

			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Referer = expectedReferer
			deps := newTestDeps(cfg)

			router := gin.New()
			router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

			target := tt.imageURL
			if target == "" {
				target = upstream.URL + "/img.jpg"
			}

			req := httptest.NewRequest(http.MethodGet, "/temp/image?url="+url.QueryEscape(target), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.expectedStatus == http.StatusOK {
				assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
				assert.Equal(t, "fake-image", w.Body.String())
			}
		})
	}
}
