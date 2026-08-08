package temp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestRandFailure_FetchAndCache_ReturnsError(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes("rand-fail-test"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)

	orig := randRead
	randRead = func(b []byte) (int, error) { return 0, errors.New("rng exhausted") }
	t.Cleanup(func() { randRead = orig })

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	assert.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "rand")
}

func TestDiskWriteFailure_FetchAndCache_PersistFailedTrue(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(make([]byte, 1024))
	}))
	t.Cleanup(upstream.Close)

	// Make every Write fail after the first 10 bytes
	fs := &writeFailFs{Fs: afero.NewMemMapFs(), failAfterBytes: 0}
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	assert.Error(t, result.err)
	assert.True(t, result.persistFailed)
	assert.Contains(t, result.err.Error(), "write temp")
}

type writeFailFile struct {
	afero.File
	written int
	failAt  int
}

func (w *writeFailFile) Write(p []byte) (int, error) {
	if w.written >= w.failAt {
		return 0, errors.New("disk full")
	}
	n, err := w.File.Write(p)
	w.written += n
	return n, err
}

type writeFailFs struct {
	afero.Fs
	failAfterBytes int
}

func (f *writeFailFs) Create(name string) (afero.File, error) {
	file, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &writeFailFile{File: file, failAt: f.failAfterBytes}, nil
}
