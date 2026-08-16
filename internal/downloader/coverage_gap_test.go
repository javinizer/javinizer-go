package downloader

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
)

func TestSetDownloadResponseHeaderTimeout_NilDialContext(t *testing.T) {
	transport := &http.Transport{}
	client := &http.Client{Transport: transport}
	setDownloadResponseHeaderTimeout(client, 10*time.Second)
	if transport.DialContext == nil {
		t.Error("DialContext should be set when originally nil")
	}
	if transport.ResponseHeaderTimeout != 10*time.Second {
		t.Errorf("expected ResponseHeaderTimeout=10s, got %v", transport.ResponseHeaderTimeout)
	}
}

func TestNewFallbackDownloadClient_DialContext(t *testing.T) {
	client := newFallbackDownloadClient(10 * time.Second)
	t2, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if t2.DialContext == nil {
		t.Error("DialContext should not be nil")
	}
	if t2.ResponseHeaderTimeout != 10*time.Second {
		t.Errorf("expected ResponseHeaderTimeout=10s, got %v", t2.ResponseHeaderTimeout)
	}
}

func TestStallReader_DisarmAlreadyClosed(t *testing.T) {
	body := newMockReadCloser([]byte("x"), 10*time.Millisecond)
	r := NewStallReader(body, 100*time.Millisecond, context.Background())
	r.Disarm()
	r.Disarm()
	r.Close()
}

func TestStallReader_WatchdogTimerStop(t *testing.T) {
	body := newMockReadCloser([]byte("data"), 50*time.Millisecond)
	r := NewStallReader(body, 100*time.Millisecond, context.Background())
	buf := make([]byte, 10)
	_, _ = r.Read(buf)
	r.Disarm()
	time.Sleep(150 * time.Millisecond)
	if body.isClosed() {
		t.Error("body should not be closed after Disarm")
	}
	r.Close()
}

func TestAdaptiveDownloaderHTTPClient_DoFallback(t *testing.T) {
	adaptive := &adaptiveDownloaderHTTPClient{
		directClient: &http.Client{},
		clients:      make(map[string]httpclient.HTTPClient),
	}
	req, _ := http.NewRequest("GET", "http://localhost:1", nil)
	_, _ = adaptive.Do(req)
}
