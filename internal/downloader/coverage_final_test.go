package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
)

func TestNewFallbackDownloadClient_WrapsDialContext(t *testing.T) {
	client := newFallbackDownloadClient(15 * time.Second)
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if tr.DialContext == nil {
		t.Error("DialContext should be wrapped")
	}
	if tr.ResponseHeaderTimeout != 15*time.Second {
		t.Errorf("expected 15s, got %v", tr.ResponseHeaderTimeout)
	}
	if tr.TLSHandshakeTimeout != 15*time.Second {
		t.Errorf("expected 15s, got %v", tr.TLSHandshakeTimeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = tr.DialContext(ctx, "tcp", "localhost:1")
}

func TestStallReader_WatchdogTimerStopFires(t *testing.T) {
	body := newMockReadCloser(nil, 10*time.Millisecond)
	r := NewStallReader(body, 50*time.Millisecond, context.Background())
	buf := make([]byte, 10)
	_, _ = r.Read(buf)
	r.Disarm()
	time.Sleep(100 * time.Millisecond)
	if body.isClosed() {
		t.Error("body should not be closed after Disarm")
	}
	r.Close()
}

func TestDownloadTrailer_RetryFailureResult(t *testing.T) {
	server := new404Server()
	defer server.Close()
	d := NewDownloader(server.Client(), newMemFS(), &Config{
		DownloadTrailer:   true,
		MediaFormatConfig: organizerMediaFormatConfig(),
	}, nil)
	movie := &models.Movie{ID: "TEST-404", TrailerURL: server.URL + "/trailer.mp4"}
	result, err := d.downloadTrailer(context.Background(), movie, "/tmp", nil)
	if err == nil {
		t.Error("expected error from 404")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Type != MediaTypeTrailer {
		t.Errorf("expected MediaTypeTrailer, got %v", result.Type)
	}
	if result.Error == nil {
		t.Error("expected result.Error to be set")
	}
}

func new404Server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
}

func newMemFS() afero.Fs {
	return afero.NewMemMapFs()
}

func organizerMediaFormatConfig() organizer.MediaFormatConfig {
	return organizer.MediaFormatConfig{
		TrailerFormat: "<ID>-trailer.mp4",
	}
}
