package downloader

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestDownloadTruncated_Temporary(t *testing.T) {
	var e downloadTruncatedError
	if !e.Temporary() {
		t.Error("downloadTruncatedError.Temporary() should return true")
	}
}

func TestDownloadEmpty_Temporary(t *testing.T) {
	var e downloadEmptyError
	if !e.Temporary() {
		t.Error("downloadEmptyError.Temporary() should return true")
	}
}

func TestIsDownloadTruncated_Wrapped(t *testing.T) {
	wrapped := fmt.Errorf("%w: downloaded 5 of 10 bytes", errDownloadTruncated)
	if !IsDownloadTruncated(wrapped) {
		t.Error("wrapped truncation should be detected")
	}
	if IsDownloadTruncated(errDownloadStalled) {
		t.Error("stalled error should not be detected as truncated")
	}
}

func TestIsDownloadEmpty_Wrapped(t *testing.T) {
	wrapped := fmt.Errorf("%w: downloaded 0 bytes", errDownloadEmpty)
	if !IsDownloadEmpty(wrapped) {
		t.Error("wrapped empty should be detected")
	}
	if IsDownloadEmpty(errDownloadStalled) {
		t.Error("stalled error should not be detected as empty")
	}
}

func TestRedactURL_WithQueryParams(t *testing.T) {
	result := redactURL("https://example.com/path?token=secret&key=abc")
	if result != "https://example.com/path" {
		t.Errorf("expected redacted URL, got: %s", result)
	}
}

func TestRedactURL_WithUserInfo(t *testing.T) {
	result := redactURL("https://user:pass@example.com/path")
	if result != "https://example.com/path" {
		t.Errorf("expected redacted URL, got: %s", result)
	}
}

func TestRedactURL_InvalidURL(t *testing.T) {
	result := redactURL("://invalid")
	if result != "://invalid" {
		t.Errorf("expected original URL for invalid input, got: %s", result)
	}
}

func TestNewFallbackDownloadClient_WithTimeout(t *testing.T) {
	client := newFallbackDownloadClient(60 * time.Second)
	if client.Timeout != 0 {
		t.Errorf("expected Timeout=0, got %v", client.Timeout)
	}
	if client.Transport == nil {
		t.Error("expected non-nil transport")
	}
}

func TestNewFallbackDownloadClient_ZeroTimeout(t *testing.T) {
	client := newFallbackDownloadClient(0)
	if client.Timeout != 0 {
		t.Errorf("expected Timeout=0, got %v", client.Timeout)
	}
}

func TestUnwrapClient_DirectClient(t *testing.T) {
	client := &http.Client{}
	result, ok := unwrapClient(client)
	if !ok || result != client {
		t.Error("expected direct *http.Client to be unwrapped")
	}
}

func TestUnwrapClient_NonHTTPClient(t *testing.T) {
	client := &adaptiveDownloaderHTTPClient{}
	_, ok := unwrapClient(client)
	if ok {
		t.Error("expected non-*http.Client to return false")
	}
}

func TestSetDownloadResponseHeaderTimeout_NilTransport(t *testing.T) {
	client := &http.Client{Transport: nil}
	setDownloadResponseHeaderTimeout(client, 30*time.Second)
}

func TestIsRetryable_UnexpectedEOF(t *testing.T) {
	wrapped := fmt.Errorf("failed to write file: %w", io.ErrUnexpectedEOF)
	if !isRetryableError(wrapped) {
		t.Error("io.ErrUnexpectedEOF should be retryable")
	}
}
