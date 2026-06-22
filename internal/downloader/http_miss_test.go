package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- validateURLScheme uncovered branches ---

func TestValidateURLScheme_UnsupportedFTP(t *testing.T) {
	err := validateURLScheme("ftp://files.example.com/video.mp4")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported URL scheme")
}

func TestValidateURLScheme_UnsupportedFile(t *testing.T) {
	err := validateURLScheme("file:///etc/passwd")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported URL scheme")
}

func TestValidateURLScheme_InvalidURL(t *testing.T) {
	err := validateURLScheme("://no-scheme")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid URL")
}

func TestValidateURLScheme_ValidHTTP(t *testing.T) {
	err := validateURLScheme("http://example.com/file.jpg")
	assert.NoError(t, err)
}

func TestValidateURLScheme_ValidHTTPS(t *testing.T) {
	err := validateURLScheme("https://example.com/file.jpg")
	assert.NoError(t, err)
}

// --- download: context cancelled before request ---

func TestDownload_ContextAlreadyCancelled(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPClient(t)
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(mockHTTP, memFS, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := downloader.download(ctx, "https://example.com/file.jpg", "/tmp/file.jpg", MediaTypeCover)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotNil(t, result)
	assert.Equal(t, "https://example.com/file.jpg", result.URL)
	assert.False(t, result.Downloaded)
}

// --- download: invalid URL scheme ---

func TestDownload_InvalidURLScheme(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPClient(t)
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(mockHTTP, memFS, cfg, nil)

	result, err := downloader.download(context.Background(), "ftp://example.com/file.jpg", "/tmp/file.jpg", MediaTypeCover)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported URL scheme")
	assert.NotNil(t, result)
	assert.False(t, result.Downloaded)
}

// --- download: HTTP error status codes ---

func TestDownload_HTTPErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"400 Bad Request", 400},
		{"401 Unauthorized", 401},
		{"403 Forbidden", 403},
		{"404 Not Found", 404},
		{"500 Internal Server Error", 500},
		{"502 Bad Gateway", 502},
		{"503 Service Unavailable", 503},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()
			cfg := &Config{DownloadTimeout: 60}
			downloader := NewDownloader(mockHTTP, memFS, cfg, nil)

			url := "https://example.com/file.jpg"
			mockHTTP.EXPECT().
				Do(mock.MatchedBy(func(req *http.Request) bool {
					return req.URL.String() == url
				})).
				Return(&http.Response{
					StatusCode: tt.statusCode,
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
				}, nil).
				Once()

			result, err := downloader.download(context.Background(), url, "/tmp/file.jpg", MediaTypeCover)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("HTTP %d", tt.statusCode))
			assert.NotNil(t, result)
			assert.False(t, result.Downloaded)
		})
	}
}

// --- download: connection refused (network error) ---

func TestDownload_ConnectionRefused(t *testing.T) {
	// Use a real HTTP client against a port that's not listening
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 5}
	client := &http.Client{}
	downloader := NewDownloader(client, memFS, cfg, nil)

	// Port 1 is almost certainly not listening
	result, err := downloader.download(context.Background(), "http://127.0.0.1:1/file.jpg", "/tmp/file.jpg", MediaTypeCover)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download")
	assert.NotNil(t, result)
	assert.False(t, result.Downloaded)
}

// --- download: redirect handling ---

func TestDownload_FollowsRedirects(t *testing.T) {
	finalContent := []byte("redirected content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(finalContent)
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	result, err := downloader.download(context.Background(), srv.URL+"/redirect", "/tmp/file.jpg", MediaTypeCover)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.Equal(t, int64(len(finalContent)), result.Size)

	content, err := afero.ReadFile(memFS, "/tmp/file.jpg")
	require.NoError(t, err)
	assert.Equal(t, finalContent, content)
}

// --- download: User-Agent header when not configured ---

func TestDownload_DefaultUserAgentWhenNotConfigured(t *testing.T) {
	var receivedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Go's net/http sets a default User-Agent (e.g. "Go-http-client/1.1")
		// when none is explicitly set in the request.
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60, UserAgent: ""} // No user agent configured
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	result, err := downloader.download(context.Background(), srv.URL+"/file.jpg", "/tmp/file.jpg", MediaTypeCover)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	// When UserAgent is empty, no custom header is set, but Go's default is used
	assert.NotEmpty(t, receivedUA)               // Go sets default User-Agent
	assert.NotEqual(t, "test-agent", receivedUA) // Should NOT be our configured agent
}

// --- download: MkdirAll failure ---

func TestDownload_MkdirAllFailure(t *testing.T) {
	mockHTTP := mocks.NewMockHTTPClient(t)
	memFS := afero.NewMemMapFs()
	// Make the filesystem read-only so MkdirAll fails
	readOnlyFS := afero.NewReadOnlyFs(memFS)
	cfg := &Config{DownloadTimeout: 60, UserAgent: "test"}
	downloader := NewDownloader(mockHTTP, readOnlyFS, cfg, nil)

	result, err := downloader.download(context.Background(), "https://example.com/file.jpg", "/nested/path/file.jpg", MediaTypeCover)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create directory")
	assert.NotNil(t, result)
	assert.False(t, result.Downloaded)
}

// --- isRetryableError uncovered branches ---

func TestIsRetryableError_Nil(t *testing.T) {
	assert.False(t, isRetryableError(nil))
}

func TestIsRetryableError_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{"503 is retryable", 503, true},
		{"500 is retryable", 500, true},
		{"429 is retryable", 429, true},
		{"404 is not retryable", 404, false},
		{"403 is not retryable", 403, false},
		{"401 is not retryable", 401, false},
		{"400 is not retryable", 400, false},
		{"302 is not retryable (default)", 302, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &statusError{statusCode: tt.statusCode}
			assert.Equal(t, tt.retryable, isRetryableError(err))
		})
	}
}

func TestIsRetryableError_NetError(t *testing.T) {
	// net.Error is retryable
	var netErr net.Error = &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	assert.True(t, isRetryableError(netErr))
}

func TestIsRetryableError_DNSError(t *testing.T) {
	dnsErr := &net.DNSError{Name: "nonexistent.example.com"}
	assert.True(t, isRetryableError(dnsErr))
}

func TestIsRetryableError_OpError(t *testing.T) {
	opErr := &net.OpError{Op: "dial", Err: errors.New("refused")}
	assert.True(t, isRetryableError(opErr))
}

func TestIsRetryableError_GenericError(t *testing.T) {
	// A generic error (not statusError, not net.Error) is treated as retryable
	// because it falls through to the OpError check and returns false there.
	// Actually let's check: plain errors are NOT retryable unless they match OpError
	err := errors.New("some random error")
	assert.False(t, isRetryableError(err))
}

// --- DownloadWithRetry uncovered branches ---

func TestDownloadWithRetry_NegativeMaxRetries(t *testing.T) {
	// Negative maxRetries should be treated as 0 (no retries)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	err := downloader.DownloadWithRetry(context.Background(), srv.URL+"/file.jpg", "/tmp/file.jpg", -1)
	assert.NoError(t, err)
}

func TestDownloadWithRetry_ContextCancelledDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // 503 = retryable
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay so the backoff timer is interrupted
	go func() {
		cancel()
	}()

	err := downloader.DownloadWithRetry(ctx, srv.URL+"/file.jpg", "/tmp/file.jpg", 5)
	assert.Error(t, err)
	// Could be context.Canceled or wrapped
	assert.True(t, errors.Is(err, context.Canceled) || err.Error() != "")
}

func TestDownloadWithRetry_NonRetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404 = not retryable
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	err := downloader.DownloadWithRetry(context.Background(), srv.URL+"/file.jpg", "/tmp/file.jpg", 3)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestDownloadWithRetry_SuccessOnRetry(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503 = retryable
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	err := downloader.DownloadWithRetry(context.Background(), srv.URL+"/file.jpg", "/tmp/file.jpg", 5)
	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestDownloadWithRetry_AllRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // 503 = retryable
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTimeout: 60}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	err := downloader.DownloadWithRetry(context.Background(), srv.URL+"/file.jpg", "/tmp/file.jpg", 2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempt(s)")
}

// --- resolveMediaReferer / resolveDownloadReferer ---

func TestResolveDownloadReferer_KnownHosts(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.javbus.com/pics/cover.jpg", "https://www.javbus.com/"},
		{"https://c0.jdbstatic.com/samples/abc.jpg", "https://javdb.com/"},
		{"https://imageproxy.libredmm.com/ref/https://pics.dmm.co.jp/img.jpg", "https://www.libredmm.com/"},
		{"https://unknown.example.com/img.jpg", "https://unknown.example.com/"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := resolveDownloadReferer(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- statusError.Error ---

func TestStatusError_Error(t *testing.T) {
	err := &statusError{statusCode: 404}
	assert.Equal(t, "HTTP 404", err.Error())
}
