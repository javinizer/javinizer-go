package downloader

// POSTER-WRITE-HARDENING wave-51 (codex P2, PR#215) — the download temp name
// is CLAIMED (O_CREATE|O_EXCL), never truncate-opened (the pre-shape
// d.fs.Create): a pre-placed occupant at a drawn temp name keeps its bytes
// and the claim climbs to a fresh draw, an exhausted draw loop fails the
// download without touching any occupant, and a non-collision open failure
// surfaces verbatim.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w47TempExistOnceFs forces os.IsExist for the FIRST O_EXCL claim of a ".tmp"
// name (planting an occupant there first so the preserved-bytes assertion is
// meaningful); every later draw passes through.
type w47TempExistOnceFs struct {
	afero.Fs
	plant  []byte
	once   bool
	fired  atomic.Bool
	claims atomic.Int32
}

func (f *w47TempExistOnceFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.HasSuffix(name, ".tmp") {
		f.claims.Add(1)
		if !f.once || f.fired.CompareAndSwap(false, true) {
			if f.plant != nil {
				if err := afero.WriteFile(f.Fs, name, f.plant, 0o600); err != nil {
					return nil, err
				}
			}
			return nil, os.ErrExist
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// w47TempFailFs fails every ".tmp" claim with a non-collision error.
type w47TempFailFs struct {
	afero.Fs
	err error
}

func (f *w47TempFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.HasSuffix(name, ".tmp") {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func w47MediaServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("genuine downloaded bytes"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadTempClaimW47_ClaimClimbsPastPlantAndPreservesIt(t *testing.T) {
	srv := w47MediaServer(t)
	base := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "cover.jpg")

	plant := []byte("pre-placed occupant at the first drawn temp name")
	fs := &w47TempExistOnceFs{Fs: base, once: true, plant: plant}
	d := NewDownloader(srv.Client(), fs, &Config{}, nil)

	result, err := d.download(context.Background(), srv.URL+"/cover.jpg", dest, MediaTypeCover)
	require.NoError(t, err)
	require.True(t, result.Downloaded)
	require.Equal(t, int32(2), fs.claims.Load(), "the claim climbed past the planted draw")

	content, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	require.Equal(t, "genuine downloaded bytes", string(content))

	// The planted temp name was never truncated or claimed.
	plantSeen := false
	entries, rerr := os.ReadDir(dir)
	require.NoError(t, rerr)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			plantSeen = true
			got, gerr := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(t, gerr)
			require.Equal(t, plant, got, "the pre-placed occupant keeps its bytes byte-intact")
		}
	}
	require.True(t, plantSeen, "the planted temp occupant is retained")
}

func TestDownloadTempClaimW47_ExhaustionFailsWithoutTouchingOccupants(t *testing.T) {
	srv := w47MediaServer(t)
	base := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "cover.jpg")

	plant := []byte("occupant at every drawn temp name")
	fs := &w47TempExistOnceFs{Fs: base, once: false, plant: plant} // always EXIST
	d := NewDownloader(srv.Client(), fs, &Config{}, nil)

	result, err := d.download(context.Background(), srv.URL+"/cover.jpg", dest, MediaTypeCover)
	require.Error(t, err)
	require.ErrorContains(t, err, "download temp names exhausted")
	require.NotNil(t, result.Error)
	require.False(t, result.Downloaded)

	// Every planted occupant survived — the claim loop never truncates.
	entries, rerr := os.ReadDir(dir)
	require.NoError(t, rerr)
	tmpCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			tmpCount++
			got, gerr := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(t, gerr)
			require.Equal(t, plant, got)
		}
	}
	require.Equal(t, downloadTempClaimTries, tmpCount, "one planted occupant per refused draw, all preserved")
}

func TestDownloadTempClaimW47_HardOpenFailureSurfaces(t *testing.T) {
	srv := w47MediaServer(t)
	base := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "cover.jpg")

	openErr := errors.New("temp open wedged")
	fs := &w47TempFailFs{Fs: base, err: openErr}
	d := NewDownloader(srv.Client(), fs, &Config{}, nil)

	result, err := d.download(context.Background(), srv.URL+"/cover.jpg", dest, MediaTypeCover)
	require.Error(t, err)
	require.ErrorIs(t, err, openErr)
	require.NotNil(t, result.Error)
}
