package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownload_DedupNonOwnerSkipsAfterOwnerFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "failed", http.StatusBadGateway)
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	d := NewDownloader(server.Client(), fs, &Config{}, nil)
	dedup := &sync.Map{}
	path := "/output/shared.jpg"

	first, firstErr := d.download(context.Background(), server.URL+"/shared.jpg", path, MediaTypeCover, true, dedup)
	require.Error(t, firstErr)
	assert.False(t, first.Downloaded)

	second, secondErr := d.download(context.Background(), server.URL+"/shared.jpg", path, MediaTypeCover, true, dedup)
	require.NoError(t, secondErr)
	assert.True(t, second.Skipped)
	assert.False(t, second.Downloaded)
	assert.Equal(t, int32(1), requests.Load())
	_, statErr := fs.Stat(path)
	assert.Error(t, statErr)
}
