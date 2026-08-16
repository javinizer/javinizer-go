package poster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func TestDownloadFromURL_StallReaderWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-image-data"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", server.Client(), 100*time.Millisecond).
		WithSSRFCheck(func(_ string) error { return nil })

	result, err := pm.DownloadFromURL(context.Background(), "job1", "ST-STALL", server.URL+"/img.jpg", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
