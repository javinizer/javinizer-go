package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownload_DedupNonOwnerRetriesAfterOwnerFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("retry success"))
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
	assert.False(t, second.Skipped)
	assert.True(t, second.Downloaded)
	assert.Equal(t, int32(2), requests.Load())
	contents, readErr := afero.ReadFile(fs, path)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("retry success"), contents)
}

func TestAcquireDownloadReservationRetriesFailedReservation(t *testing.T) {
	dedup := &sync.Map{}
	path := "/output/shared.jpg"
	failed := &downloadReservation{done: make(chan struct{})}
	dedup.Store(path, failed)
	close(failed.done)
	go func() {
		time.Sleep(time.Millisecond)
		dedup.Delete(path)
	}()

	reservation, skipped, err := acquireDownloadReservation(context.Background(), dedup, path)
	require.NoError(t, err)
	assert.False(t, skipped)
	require.NotNil(t, reservation)
	finishDownloadReservation(dedup, path, reservation, true)
}

func TestAcquireDownloadReservationHonorsCancellation(t *testing.T) {
	dedup := &sync.Map{}
	path := "/output/shared.jpg"
	owner, skipped, err := acquireDownloadReservation(context.Background(), dedup, path)
	require.NoError(t, err)
	assert.False(t, skipped)
	require.NotNil(t, owner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reservation, skipped, err := acquireDownloadReservation(ctx, dedup, path)
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, skipped)
	assert.Nil(t, reservation)
	finishDownloadReservation(dedup, path, owner, false)
}

func TestDownload_ReturnsReservationWaitCancellation(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := NewDownloader(http.DefaultClient, fs, &Config{}, nil)
	dedup := &sync.Map{}
	path := "/output/shared.jpg"
	owner, skipped, err := acquireDownloadReservation(context.Background(), dedup, path)
	require.NoError(t, err)
	assert.False(t, skipped)
	require.NotNil(t, owner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := d.download(ctx, "https://example.com/shared.jpg", path, MediaTypeCover, true, dedup)
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, result.Error, context.Canceled)
	finishDownloadReservation(dedup, path, owner, false)
}
