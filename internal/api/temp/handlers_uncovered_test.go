package temp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestResolveTempImageReferer_Uncovered(t *testing.T) {
	tests := []struct {
		name              string
		downloadURL       string
		configuredReferer string
		expected          string
	}{
		{
			name:              "javbus domain returns javbus referer",
			downloadURL:       "https://www.javbus.com/pics/123.jpg",
			configuredReferer: "",
			expected:          "https://www.javbus.com/",
		},
		{
			name:              "unknown domain with configured referer",
			downloadURL:       "https://example.com/img.jpg",
			configuredReferer: "https://custom-referer.com/",
			expected:          "https://custom-referer.com/",
		},
		{
			name:              "unknown domain with empty referer returns origin",
			downloadURL:       "https://example.com/img.jpg",
			configuredReferer: "",
			expected:          "https://example.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveTempImageReferer(tt.downloadURL, tt.configuredReferer)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServeCroppedPoster_InvalidFilename_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/posters/:filename", serveCroppedPoster())

	tests := []struct {
		name           string
		filename       string
		expectedStatus int
	}{
		{
			name:           "path traversal in filename",
			filename:       "../../../etc/passwd",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "non-jpg extension",
			filename:       "test.png",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "no extension",
			filename:       "testfile",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/posters/"+tt.filename, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestServeTempImage_MissingURL_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeTempImage_InvalidURL_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/temp/image", serveTempImage(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/temp/image?url=not-a-url", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
