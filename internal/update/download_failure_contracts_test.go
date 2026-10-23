package update

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type rejectedDownloadTransport struct{ calls atomic.Int32 }

func (r *rejectedDownloadTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return nil, os.ErrPermission
}

func TestDownloadMalformedURLDoesNotRequestOrWrite(t *testing.T) {
	transport := &rejectedDownloadTransport{}
	var output bytes.Buffer
	err := DownloadTo(context.Background(), &http.Client{Transport: transport}, "://invalid", &output, 1024)
	require.Error(t, err)
	require.Zero(t, transport.calls.Load())
	require.Empty(t, output.String())
}

func TestDownloadTruncatedResponsePreservesFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "partial")
	}))
	defer server.Close()
	var output bytes.Buffer
	err := DownloadTo(context.Background(), server.Client(), server.URL, &output, 1024)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.ErrorContains(t, err, "read body")
	require.Equal(t, "partial", output.String())
}

func TestDownloadClosedDestinationRejectsWrite(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "new binary")
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "destination")
	require.NoError(t, os.WriteFile(path, []byte("existing bytes"), 0600))
	destination, err := os.OpenFile(path, os.O_WRONLY, 0600)
	require.NoError(t, err)
	require.NoError(t, destination.Close())
	err = DownloadTo(context.Background(), server.Client(), server.URL, destination, 1024)
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorContains(t, err, "read body")
	retained, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "existing bytes", string(retained))
}

func TestVerifyDownloadedDirectoryReturnsHashReadError(t *testing.T) {
	path := t.TempDir()
	err := VerifyFileSHA256(path, "not-a-file-checksum")
	require.Error(t, err)
	require.ErrorContains(t, err, "hash asset")
	entries, readErr := os.ReadDir(path)
	require.NoError(t, readErr)
	require.Empty(t, entries)
}
