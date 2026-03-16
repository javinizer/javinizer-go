package downloader

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestDownload_Success tests successful download scenarios using MockHTTPClient
func TestDownload_Success(t *testing.T) {
	// Load golden file content once for reuse
	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	tests := []struct {
		name           string
		url            string
		destPath       string
		goldenFile     string
		mockStatus     int
		wantDownloaded bool
		wantFileExists bool
		wantError      bool
	}{
		{
			name:           "success - download poster",
			url:            "https://example.com/poster.jpg",
			destPath:       "/tmp/output.jpg",
			goldenFile:     goldenPath,
			mockStatus:     200,
			wantDownloaded: true,
			wantFileExists: true,
			wantError:      false,
		},
		{
			name:           "success - download cover",
			url:            "https://example.com/cover.jpg",
			destPath:       "/tmp/cover.jpg",
			goldenFile:     goldenPath,
			mockStatus:     200,
			wantDownloaded: true,
			wantFileExists: true,
			wantError:      false,
		},
		{
			name:           "skip - file already exists",
			url:            "https://example.com/existing.jpg",
			destPath:       "/tmp/existing.jpg",
			goldenFile:     goldenPath,
			mockStatus:     200,
			wantDownloaded: false, // Expect false because file already exists
			wantFileExists: true,
			wantError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// For "skip - file already exists" test, pre-create the file
			if tt.wantDownloaded == false && tt.name == "skip - file already exists" {
				require.NoError(t, afero.WriteFile(memFS, tt.destPath, goldenData, 0644))
			}

			// Configure mock expectations only if we expect a download
			if tt.wantDownloaded {
				mockHTTP.EXPECT().
					Do(mock.MatchedBy(func(req *http.Request) bool {
						return req.URL.String() == tt.url
					})).
					Return(&http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(bytes.NewReader(goldenData)),
					}, nil).
					Once()
			}

			cfg := &config.OutputConfig{
				DownloadTimeout: 60,
			}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute
			result, err := downloader.download(tt.url, tt.destPath, MediaTypePoster)

			// Assert
			if tt.wantError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tt.wantDownloaded, result.Downloaded)
			assert.Equal(t, tt.url, result.URL)
			assert.Equal(t, tt.destPath, result.LocalPath)
			assert.Equal(t, MediaTypePoster, result.Type)

			if tt.wantFileExists {
				exists, _ := afero.Exists(memFS, tt.destPath)
				assert.True(t, exists, "Expected file to exist at %s", tt.destPath)

				content, _ := afero.ReadFile(memFS, tt.destPath)
				assert.Equal(t, goldenData, content, "File content should match golden file")
				assert.Equal(t, int64(len(goldenData)), result.Size, "Result size should match golden file size")
			}
		})
	}
}

// TestDownload_ErrorHandling tests error scenarios using MockHTTPClient
func TestDownload_ErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		destPath       string
		mockStatus     int
		mockError      error
		wantError      bool
		wantFileExists bool
		expectedErrMsg string
	}{
		{
			name:           "error - 404 not found",
			url:            "https://example.com/missing.jpg",
			destPath:       "/tmp/missing.jpg",
			mockStatus:     404,
			mockError:      nil,
			wantError:      true,
			wantFileExists: false,
			expectedErrMsg: "bad status code: 404",
		},
		{
			name:           "error - 500 server error",
			url:            "https://example.com/error.jpg",
			destPath:       "/tmp/error.jpg",
			mockStatus:     500,
			mockError:      nil,
			wantError:      true,
			wantFileExists: false,
			expectedErrMsg: "bad status code: 500",
		},
		{
			name:           "error - 503 service unavailable",
			url:            "https://example.com/unavailable.jpg",
			destPath:       "/tmp/unavailable.jpg",
			mockStatus:     503,
			mockError:      nil,
			wantError:      true,
			wantFileExists: false,
			expectedErrMsg: "bad status code: 503",
		},
		{
			name:           "error - network timeout",
			url:            "https://example.com/timeout.jpg",
			destPath:       "/tmp/timeout.jpg",
			mockStatus:     0, // Not used when mockError is set
			mockError:      fmt.Errorf("context deadline exceeded"),
			wantError:      true,
			wantFileExists: false,
			expectedErrMsg: "failed to download",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// Configure mock expectations
			if tt.mockError != nil {
				// Network error case
				mockHTTP.EXPECT().
					Do(mock.MatchedBy(func(req *http.Request) bool {
						return req.URL.String() == tt.url
					})).
					Return((*http.Response)(nil), tt.mockError).
					Once()
			} else {
				// HTTP error status case
				mockHTTP.EXPECT().
					Do(mock.MatchedBy(func(req *http.Request) bool {
						return req.URL.String() == tt.url
					})).
					Return(&http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(bytes.NewReader([]byte{})),
					}, nil).
					Once()
			}

			cfg := &config.OutputConfig{
				DownloadTimeout: 60,
			}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute
			result, err := downloader.download(tt.url, tt.destPath, MediaTypePoster)

			// Assert
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErrMsg)
			assert.NotNil(t, result)
			assert.False(t, result.Downloaded)
			assert.Equal(t, tt.url, result.URL)

			// Verify no partial files left in filesystem
			exists, _ := afero.Exists(memFS, tt.destPath)
			assert.False(t, exists, "Expected no file at %s after error", tt.destPath)

			// Also check for .tmp file
			tmpPath := tt.destPath + ".tmp"
			tmpExists, _ := afero.Exists(memFS, tmpPath)
			assert.False(t, tmpExists, "Expected no .tmp file at %s after error", tmpPath)
		})
	}
}

// TestDownload_DirectoryCreation tests that parent directories are created
func TestDownload_DirectoryCreation(t *testing.T) {
	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	mockHTTP := mocks.NewMockHTTPClient(t)
	memFS := afero.NewMemMapFs()

	url := "https://example.com/test.jpg"
	destPath := "/nested/path/to/file.jpg"

	mockHTTP.EXPECT().
		Do(mock.MatchedBy(func(req *http.Request) bool {
			return req.URL.String() == url
		})).
		Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(goldenData)),
		}, nil).
		Once()

	cfg := &config.OutputConfig{
		DownloadTimeout: 60,
	}
	downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

	// Execute
	result, err := downloader.download(url, destPath, MediaTypePoster)

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Downloaded)

	// Verify directory was created
	dirExists, _ := afero.DirExists(memFS, "/nested/path/to")
	assert.True(t, dirExists, "Expected parent directory to be created")

	// Verify file exists
	fileExists, _ := afero.Exists(memFS, destPath)
	assert.True(t, fileExists, "Expected file to exist after download")
}

// TestDownload_UserAgent tests that User-Agent header is set correctly
func TestDownload_UserAgent(t *testing.T) {
	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	mockHTTP := mocks.NewMockHTTPClient(t)
	memFS := afero.NewMemMapFs()

	url := "https://example.com/test.jpg"
	destPath := "/tmp/test.jpg"
	expectedUserAgent := "TestAgent/1.0"

	mockHTTP.EXPECT().
		Do(mock.MatchedBy(func(req *http.Request) bool {
			// Verify both URL and User-Agent header
			return req.URL.String() == url && req.Header.Get("User-Agent") == expectedUserAgent
		})).
		Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(goldenData)),
		}, nil).
		Once()

	cfg := &config.OutputConfig{
		DownloadTimeout: 60,
	}
	downloader := NewDownloader(mockHTTP, memFS, cfg, expectedUserAgent)

	// Execute
	result, err := downloader.download(url, destPath, MediaTypePoster)

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Downloaded)
}

func TestDownload_RefererHeader(t *testing.T) {
	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	tests := []struct {
		name            string
		url             string
		expectedReferer string
	}{
		{
			name:            "javbus media uses javbus referer",
			url:             "https://www.javbus.com/pics/cover/77dp_b.jpg",
			expectedReferer: "https://www.javbus.com/",
		},
		{
			name:            "javdb media uses javdb referer",
			url:             "https://c0.jdbstatic.com/samples/abc.jpg",
			expectedReferer: "https://javdb.com/",
		},
		{
			name:            "libredmm media uses libredmm referer",
			url:             "https://imageproxy.libredmm.com/_refabc/https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg",
			expectedReferer: "https://www.libredmm.com/",
		},
		{
			name:            "unknown host falls back to origin",
			url:             "https://images.example.com/a/b.jpg",
			expectedReferer: "https://images.example.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()
			destPath := "/tmp/test.jpg"

			mockHTTP.EXPECT().
				Do(mock.MatchedBy(func(req *http.Request) bool {
					return req.URL.String() == tt.url &&
						req.Header.Get("Referer") == tt.expectedReferer
				})).
				Return(&http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(goldenData)),
				}, nil).
				Once()

			cfg := &config.OutputConfig{DownloadTimeout: 60}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			result, err := downloader.download(tt.url, destPath, MediaTypePoster)
			assert.NoError(t, err)
			assert.True(t, result.Downloaded)
		})
	}
}

// TestDownload_ContextUsage tests that download respects context cancellation
func TestDownload_ContextUsage(t *testing.T) {
	// Note: The current implementation uses context.Background() internally
	// This test verifies the current behavior and can be expanded when
	// context propagation is added (Story 4.2)

	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	mockHTTP := mocks.NewMockHTTPClient(t)
	memFS := afero.NewMemMapFs()

	url := "https://example.com/test.jpg"
	destPath := "/tmp/test.jpg"

	mockHTTP.EXPECT().
		Do(mock.MatchedBy(func(req *http.Request) bool {
			// Verify that request has a context (even if it's Background())
			return req.URL.String() == url && req.Context() != nil
		})).
		Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(goldenData)),
		}, nil).
		Once()

	cfg := &config.OutputConfig{
		DownloadTimeout: 60,
	}
	downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

	// Execute
	result, err := downloader.download(url, destPath, MediaTypePoster)

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Downloaded)
}
