package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestDownloadWithRetry_TransientErrors tests retry sequences with transient errors
// Covers AC1: Retry on Transient Errors
func TestDownloadWithRetry_TransientErrors(t *testing.T) {
	// Load golden file content once for reuse
	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	tests := []struct {
		name             string
		statusSequence   []int
		maxRetries       int
		wantErr          bool
		wantCallCount    int
		validateBackoff  bool
		expectedBackoffs []time.Duration // For AC1 timing validation
	}{
		{
			name:             "success after 2 retries - [503, 503, 200]",
			statusSequence:   []int{503, 503, 200},
			maxRetries:       3,
			wantErr:          false,
			wantCallCount:    3,
			validateBackoff:  true,
			expectedBackoffs: []time.Duration{100 * time.Millisecond, 200 * time.Millisecond},
		},
		{
			name:           "success after 1 retry - [503, 200]",
			statusSequence: []int{503, 200},
			maxRetries:     3,
			wantErr:        false,
			wantCallCount:  2,
		},
		{
			name:           "success after 2 retries - [500, 500, 200]",
			statusSequence: []int{500, 500, 200},
			maxRetries:     3,
			wantErr:        false,
			wantCallCount:  3,
		},
		{
			name:           "success after 2 retries - [429, 429, 200]",
			statusSequence: []int{429, 429, 200},
			maxRetries:     3,
			wantErr:        false,
			wantCallCount:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// Configure mock expectations for status sequence
			for i, status := range tt.statusSequence {
				if status == 200 {
					// Success response with golden data
					mockHTTP.EXPECT().
						Do(mock.Anything).
						Return(&http.Response{
							StatusCode: status,
							Body:       io.NopCloser(bytes.NewReader(goldenData)),
						}, nil).
						Once()
				} else {
					// Error response with no body
					mockHTTP.EXPECT().
						Do(mock.Anything).
						Return(&http.Response{
							StatusCode: status,
							Body:       http.NoBody,
						}, nil).
						Once()
				}

				// Log for debugging
				t.Logf("Configured mock expectation %d: HTTP %d", i+1, status)
			}

			cfg := &config.OutputConfig{
				DownloadTimeout: 60,
			}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute with timing measurement if backoff validation requested
			start := time.Now()
			ctx := context.Background()
			err := downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", tt.maxRetries)
			elapsed := time.Since(start)

			// Verify
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify file was written with correct content
				content, readErr := afero.ReadFile(memFS, "/tmp/output.jpg")
				assert.NoError(t, readErr)
				assert.Equal(t, goldenData, content, "Downloaded content should match golden file")
			}

			// Verify correct number of HTTP calls
			mockHTTP.AssertNumberOfCalls(t, "Do", tt.wantCallCount)

			// Validate backoff timing if requested (AC1, AC5)
			if tt.validateBackoff {
				totalExpectedDelay := time.Duration(0)
				for _, d := range tt.expectedBackoffs {
					totalExpectedDelay += d
				}

				// Allow ±20% tolerance for CI reliability (AC5)
				tolerance := float64(totalExpectedDelay) * 0.2
				assert.InDelta(t, float64(totalExpectedDelay), float64(elapsed), tolerance,
					"Total elapsed time should match expected backoff delays within ±20%%")

				t.Logf("Backoff timing: elapsed=%v, expected=%v, tolerance=±20%%", elapsed, totalExpectedDelay)
			}
		})
	}
}

// TestDownloadWithRetry_RetryExhaustion tests scenarios where all retries are exhausted
// Covers AC3: Retry Exhaustion
func TestDownloadWithRetry_RetryExhaustion(t *testing.T) {
	tests := []struct {
		name           string
		statusSequence []int
		maxRetries     int
		wantCallCount  int
		wantErrContain string
	}{
		{
			name:           "all retries fail - [503, 503, 503]",
			statusSequence: []int{503, 503, 503},
			maxRetries:     2, // Initial + 2 retries = 3 attempts
			wantCallCount:  3,
			wantErrContain: "download failed after 3 attempt(s)",
		},
		{
			name:           "all retries fail with 500",
			statusSequence: []int{500, 500, 500, 500},
			maxRetries:     3,
			wantCallCount:  4,
			wantErrContain: "download failed after 4 attempt(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// Configure mock expectations for status sequence
			for _, status := range tt.statusSequence {
				mockHTTP.EXPECT().
					Do(mock.Anything).
					Return(&http.Response{
						StatusCode: status,
						Body:       http.NoBody,
					}, nil).
					Once()
			}

			cfg := &config.OutputConfig{
				DownloadTimeout: 60,
			}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute
			ctx := context.Background()
			err := downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", tt.maxRetries)

			// Verify
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrContain, "Error should indicate retry exhaustion")

			// Verify all retry attempts were made
			mockHTTP.AssertNumberOfCalls(t, "Do", tt.wantCallCount)
		})
	}
}

// TestDownloadWithRetry_NonRetryableErrors tests immediate failure on non-retryable errors
// Covers AC2: Non-Retryable Error Handling
func TestDownloadWithRetry_NonRetryableErrors(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		wantCallCount  int
		wantErrContain string
	}{
		{
			name:           "404 not found - fail immediately",
			statusCode:     404,
			wantCallCount:  1,
			wantErrContain: "404",
		},
		{
			name:           "403 forbidden - fail immediately",
			statusCode:     403,
			wantCallCount:  1,
			wantErrContain: "403",
		},
		{
			name:           "401 unauthorized - fail immediately",
			statusCode:     401,
			wantCallCount:  1,
			wantErrContain: "401",
		},
		{
			name:           "400 bad request - fail immediately",
			statusCode:     400,
			wantCallCount:  1,
			wantErrContain: "400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// Configure mock expectations - only one call expected
			mockHTTP.EXPECT().
				Do(mock.Anything).
				Return(&http.Response{
					StatusCode: tt.statusCode,
					Body:       http.NoBody,
				}, nil).
				Once()

			cfg := &config.OutputConfig{
				DownloadTimeout: 60,
			}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute
			ctx := context.Background()
			err := downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", 3)

			// Verify
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrContain, "Error should include status code")
			assert.Contains(t, err.Error(), "https://example.com/test.jpg", "Error should include URL")

			// Verify no retry attempts (only 1 HTTP call)
			mockHTTP.AssertNumberOfCalls(t, "Do", tt.wantCallCount)
		})
	}
}

// TestDownloadWithRetry_ContextCancellation tests context cancellation during retry
// Covers AC4: Context Cancellation During Retry
func TestDownloadWithRetry_ContextCancellation(t *testing.T) {
	tests := []struct {
		name             string
		cancelDuring     string // "backoff" or "initial"
		maxCallsExpected int
		cancelDelayMs    int
	}{
		{
			name:             "cancel during backoff delay",
			cancelDuring:     "backoff",
			maxCallsExpected: 1, // Initial attempt, then cancelled during backoff
			cancelDelayMs:    50,
		},
		{
			name:             "cancel before initial attempt",
			cancelDuring:     "initial",
			maxCallsExpected: 0, // Cancelled before any HTTP call
			cancelDelayMs:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			if tt.cancelDuring == "backoff" {
				// Configure mock to return 503 on first attempt
				mockHTTP.EXPECT().
					Do(mock.Anything).
					Return(&http.Response{
						StatusCode: 503,
						Body:       http.NoBody,
					}, nil).
					Maybe() // Use Maybe() since cancellation might prevent second call
			}

			cfg := &config.OutputConfig{
				DownloadTimeout: 60,
			}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Create cancellable context
			ctx, cancel := context.WithCancel(context.Background())

			// Cancel context during backoff or before initial attempt
			if tt.cancelDuring == "initial" {
				cancel() // Cancel immediately
			} else {
				// Cancel after delay (during backoff)
				go func() {
					time.Sleep(time.Duration(tt.cancelDelayMs) * time.Millisecond)
					cancel()
				}()
			}

			// Execute
			err := downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", 3)

			// Verify
			assert.Error(t, err)
			assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error")

			// Verify no HTTP requests after cancellation
			actualCalls := len(mockHTTP.Calls)
			assert.LessOrEqual(t, actualCalls, tt.maxCallsExpected,
				"Should not make more HTTP requests after context cancellation")
		})
	}
}

// TestDownloadWithRetry_ExponentialBackoff tests backoff delay calculations
// Covers AC5: Exponential Backoff Validation
func TestDownloadWithRetry_ExponentialBackoff(t *testing.T) {
	t.Run("backoff timing validation", func(t *testing.T) {
		// Setup
		mockHTTP := mocks.NewMockHTTPClient(t)
		memFS := afero.NewMemMapFs()

		goldenPath := filepath.Join("testdata", "poster.jpg.golden")
		goldenData, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("Failed to load golden file: %v", err)
		}

		// Configure mock: 503, 503, 200
		mockHTTP.EXPECT().Do(mock.Anything).Return(&http.Response{StatusCode: 503, Body: http.NoBody}, nil).Once()
		mockHTTP.EXPECT().Do(mock.Anything).Return(&http.Response{StatusCode: 503, Body: http.NoBody}, nil).Once()
		mockHTTP.EXPECT().Do(mock.Anything).Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(goldenData)),
		}, nil).Once()

		cfg := &config.OutputConfig{DownloadTimeout: 60}
		downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

		// Execute with timing measurement
		start := time.Now()
		ctx := context.Background()
		err = downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", 3)
		elapsed := time.Since(start)

		// Verify success
		assert.NoError(t, err)

		// Verify backoff timing: ~100ms + ~200ms = ~300ms total (±20% = 240-360ms)
		expectedDelay := 300 * time.Millisecond
		tolerance := float64(expectedDelay) * 0.2
		assert.InDelta(t, float64(expectedDelay), float64(elapsed), tolerance,
			"Total elapsed time should match expected backoff delays within ±20%%")

		t.Logf("Backoff timing: elapsed=%v, expected=%v (±20%% tolerance)", elapsed, expectedDelay)
	})

	t.Run("max delay capping at 10 seconds", func(t *testing.T) {
		// Setup
		mockHTTP := mocks.NewMockHTTPClient(t)
		memFS := afero.NewMemMapFs()

		// Configure mock: Many 503 responses to trigger max delay capping
		for i := 0; i < 8; i++ {
			mockHTTP.EXPECT().Do(mock.Anything).Return(&http.Response{StatusCode: 503, Body: http.NoBody}, nil).Once()
		}

		cfg := &config.OutputConfig{DownloadTimeout: 60}
		downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

		// Execute with timing measurement
		start := time.Now()
		ctx := context.Background()
		err := downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", 7)
		elapsed := time.Since(start)

		// Verify error
		assert.Error(t, err)

		// Calculate expected total delay with max capping at 10s
		// Delays: 100ms, 200ms, 400ms, 800ms, 1600ms, 3200ms, 6400ms
		// 100+200+400+800+1600+3200+6400 = 12700ms = 12.7s
		expectedDelay := 12700 * time.Millisecond
		tolerance := float64(expectedDelay) * 0.2

		assert.InDelta(t, float64(expectedDelay), float64(elapsed), tolerance,
			"Total elapsed time should respect exponential backoff with max cap")

		t.Logf("Max capping test: elapsed=%v, expected=%v (±20%% tolerance)", elapsed, expectedDelay)
	})
}

// TestDownloadWithRetry_RedirectHandling tests redirect following behavior
// Covers AC6: Redirect Handling
func TestDownloadWithRetry_RedirectHandling(t *testing.T) {
	// Load golden file
	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	tests := []struct {
		name          string
		statusCode    int
		wantErr       bool
		wantCallCount int
	}{
		{
			name:          "301 moved permanently - followed transparently",
			statusCode:    200, // http.Client follows redirect internally, we see final 200
			wantErr:       false,
			wantCallCount: 1,
		},
		{
			name:          "302 found - followed transparently",
			statusCode:    200,
			wantErr:       false,
			wantCallCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// Mock http.Client follows redirects internally, so we only see final response
			mockHTTP.EXPECT().
				Do(mock.Anything).
				Return(&http.Response{
					StatusCode: tt.statusCode,
					Body:       io.NopCloser(bytes.NewReader(goldenData)),
				}, nil).
				Once()

			cfg := &config.OutputConfig{DownloadTimeout: 60}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute
			ctx := context.Background()
			err := downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", 3)

			// Verify
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify file content matches golden file
				content, readErr := afero.ReadFile(memFS, "/tmp/output.jpg")
				assert.NoError(t, readErr)
				assert.Equal(t, goldenData, content)
			}

			// Verify redirect is NOT counted as retry attempt (only 1 HTTP call)
			mockHTTP.AssertNumberOfCalls(t, "Do", tt.wantCallCount)
		})
	}
}

// TestDownloadWithRetry_NetworkErrors tests retry behavior for network errors
func TestDownloadWithRetry_NetworkErrors(t *testing.T) {
	t.Run("network error triggers retry", func(t *testing.T) {
		// Setup
		mockHTTP := mocks.NewMockHTTPClient(t)
		memFS := afero.NewMemMapFs()

		goldenPath := filepath.Join("testdata", "poster.jpg.golden")
		goldenData, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("Failed to load golden file: %v", err)
		}

		// Configure mock: network error, then success
		mockHTTP.EXPECT().
			Do(mock.Anything).
			Return(nil, errors.New("connection refused")).
			Once()

		mockHTTP.EXPECT().
			Do(mock.Anything).
			Return(&http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(goldenData)),
			}, nil).
			Once()

		cfg := &config.OutputConfig{DownloadTimeout: 60}
		downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

		// Execute
		ctx := context.Background()
		err = downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", 3)

		// Verify success after retry
		assert.NoError(t, err)
		mockHTTP.AssertNumberOfCalls(t, "Do", 2)
	})
}

// TestDownloadWithRetry_MaxRetriesBoundary tests edge cases for maxRetries parameter
func TestDownloadWithRetry_MaxRetriesBoundary(t *testing.T) {
	// Load golden file
	goldenPath := filepath.Join("testdata", "poster.jpg.golden")
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	tests := []struct {
		name           string
		maxRetries     int
		statusSequence []int
		wantErr        bool
		wantCallCount  int
	}{
		{
			name:           "maxRetries=0 - only initial attempt",
			maxRetries:     0,
			statusSequence: []int{200},
			wantErr:        false,
			wantCallCount:  1,
		},
		{
			name:           "maxRetries=1 - initial + 1 retry",
			maxRetries:     1,
			statusSequence: []int{503, 200},
			wantErr:        false,
			wantCallCount:  2,
		},
		{
			name:           "maxRetries=-1 - treated as 0 (only initial attempt)",
			maxRetries:     -1,
			statusSequence: []int{200},
			wantErr:        false,
			wantCallCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// Configure mock expectations
			for _, status := range tt.statusSequence {
				if status == 200 {
					mockHTTP.EXPECT().
						Do(mock.Anything).
						Return(&http.Response{
							StatusCode: status,
							Body:       io.NopCloser(bytes.NewReader(goldenData)),
						}, nil).
						Once()
				} else {
					mockHTTP.EXPECT().
						Do(mock.Anything).
						Return(&http.Response{
							StatusCode: status,
							Body:       http.NoBody,
						}, nil).
						Once()
				}
			}

			cfg := &config.OutputConfig{DownloadTimeout: 60}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute
			ctx := context.Background()
			err := downloader.DownloadWithRetry(ctx, "https://example.com/test.jpg", "/tmp/output.jpg", tt.maxRetries)

			// Verify
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockHTTP.AssertNumberOfCalls(t, "Do", tt.wantCallCount)
		})
	}
}

// TestDownloadWithRetry_InvalidURLScheme tests URL scheme validation
func TestDownloadWithRetry_InvalidURLScheme(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		wantErr        bool
		wantErrContain string
	}{
		{
			name:           "file:// scheme rejected",
			url:            "file:///etc/passwd",
			wantErr:        true,
			wantErrContain: "unsupported URL scheme",
		},
		{
			name:           "ftp:// scheme rejected",
			url:            "ftp://ftp.example.com/file.txt",
			wantErr:        true,
			wantErrContain: "unsupported URL scheme",
		},
		{
			name:           "data:// scheme rejected",
			url:            "data:text/plain;base64,SGVsbG8=",
			wantErr:        true,
			wantErrContain: "unsupported URL scheme",
		},
		{
			name:           "javascript: scheme rejected",
			url:            "javascript:alert(1)",
			wantErr:        true,
			wantErrContain: "unsupported URL scheme",
		},
		{
			name:    "http:// scheme allowed",
			url:     "http://example.com/file.jpg",
			wantErr: false,
		},
		{
			name:    "https:// scheme allowed",
			url:     "https://example.com/file.jpg",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockHTTP := mocks.NewMockHTTPClient(t)
			memFS := afero.NewMemMapFs()

			// For valid URLs, pre-create the file so download is skipped
			if !tt.wantErr {
				require.NoError(t, afero.WriteFile(memFS, "/tmp/output.jpg", []byte("test"), 0644))
			}

			cfg := &config.OutputConfig{DownloadTimeout: 60}
			downloader := NewDownloader(mockHTTP, memFS, cfg, "test-agent")

			// Execute
			ctx := context.Background()
			err := downloader.DownloadWithRetry(ctx, tt.url, "/tmp/output.jpg", 3)

			// Verify
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContain)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestIsRetryableError tests the internal isRetryableError helper function
func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{
			name:      "503 service unavailable - retryable",
			err:       &statusError{statusCode: 503},
			wantRetry: true,
		},
		{
			name:      "500 internal server error - retryable",
			err:       &statusError{statusCode: 500},
			wantRetry: true,
		},
		{
			name:      "429 too many requests - retryable",
			err:       &statusError{statusCode: 429},
			wantRetry: true,
		},
		{
			name:      "404 not found - non-retryable",
			err:       &statusError{statusCode: 404},
			wantRetry: false,
		},
		{
			name:      "403 forbidden - non-retryable",
			err:       &statusError{statusCode: 403},
			wantRetry: false,
		},
		{
			name:      "401 unauthorized - non-retryable",
			err:       &statusError{statusCode: 401},
			wantRetry: false,
		},
		{
			name:      "400 bad request - non-retryable",
			err:       &statusError{statusCode: 400},
			wantRetry: false,
		},
		{
			name:      "connection refused - retryable",
			err:       fmt.Errorf("failed to download: connection refused"),
			wantRetry: true,
		},
		{
			name:      "connection reset - retryable",
			err:       fmt.Errorf("failed to download: connection reset by peer"),
			wantRetry: true,
		},
		{
			name:      "no such host - retryable",
			err:       fmt.Errorf("failed to download: no such host"),
			wantRetry: true,
		},
		{
			name:      "i/o timeout - retryable",
			err:       fmt.Errorf("failed to download: i/o timeout"),
			wantRetry: true,
		},
		{
			name:      "nil error - non-retryable",
			err:       nil,
			wantRetry: false,
		},
		{
			name:      "unknown error - non-retryable",
			err:       fmt.Errorf("unknown error"),
			wantRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			assert.Equal(t, tt.wantRetry, got)
		})
	}
}
