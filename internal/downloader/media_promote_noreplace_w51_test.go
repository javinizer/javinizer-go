package downloader

// POSTER-WRITE-HARDENING wave-51 (codex P2 parity for the legacy promote) —
// the non-overwrite poster promote rides the shared no-replace primitive:
// a racer claiming the destination inside the download→promote window keeps
// its bytes (POSIX previously CLOBBERED them with the plain Rename's replace
// semantics — no backup, no ledger, and a Windows/POSIX parity break), an
// ErrPublishCompleted-carrying publish error is honored like the wave-42
// install path (the candidate name is retained, never unverified-unlinked),
// and a plain publish failure keeps the established error leg.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// w51PromoteCollisionFs plants racer bytes at the destination inside the
// publish's classification lookup — the deterministic replay of a foreign
// writer claiming destPath between the pre-download absence check and the
// no-replace promote.
type w51PromoteCollisionFs struct {
	afero.Fs
	dest  string
	racer []byte
	fired atomic.Bool
	seen  atomic.Int32
}

func (f *w51PromoteCollisionFs) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(f.dest) {
		// The first lookup is the pre-download classification (destination
		// absent); the SECOND is the publish's own classify — plant there.
		if f.seen.Add(1) >= 2 && f.fired.CompareAndSwap(false, true) {
			if err := afero.WriteFile(f.Fs, f.dest, f.racer, 0o644); err != nil {
				return nil, err
			}
		}
	}
	return f.Fs.Stat(name)
}

// w51PromotePublishErrorFs returns a crafted error from the promote rename
// (the no-replace publish's terminal rename on virtual legs).
type w51PromotePublishErrorFs struct {
	afero.Fs
	dest  string
	err   error
	fired atomic.Bool
}

func (f *w51PromotePublishErrorFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.Contains(oldname, ".crop.tmp") {
		f.fired.Store(true)
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

func w51PosterMovie(id, url string) *models.Movie {
	// ShouldCropPoster=true drives the crop-capable flow whose terminal leg
	// is the !overwriteExisting no-replace promote under test.
	return &models.Movie{ID: id, Poster: models.PosterState{PosterURL: url, ShouldCropPoster: true}}
}

func w51PosterConfig() *Config {
	return &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
}

func w51PosterDest(t *testing.T, d *Downloader, movie *models.Movie, destDir string) string {
	t.Helper()
	return d.pathResolver.ResolvePosterPath(movie, nil, true, d.buildTemplateContext(movie, nil), destDir)
}

func TestDownloadPosterW51_PromoteCollisionKeepsRacerBytes(t *testing.T) {
	server := covW1BPosterServer()
	defer server.Close()

	base := afero.NewOsFs()
	destDir := t.TempDir()
	racer := []byte("the racer's own destination bytes")
	fs := &w51PromoteCollisionFs{Fs: base, racer: racer}
	movie := w51PosterMovie("W51-COLLIDE", server.URL+"/poster.jpg")
	dest := w51PosterDest(t, NewDownloader(nil, base, w51PosterConfig(), nil), movie, destDir)
	fs.dest = dest
	d := NewDownloader(server.Client(), fs, w51PosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil)
	require.NoError(t, err, "the racer's win is the existing-artwork outcome, not a failure")
	require.True(t, fs.fired.Load(), "the collision wedge must have fired")
	require.False(t, result.Downloaded, "nothing was downloaded INTO dest by this pass")
	require.Equal(t, dest, result.LocalPath, "the racer-occupied destination is the resolved artwork")
	require.Equal(t, int64(len(racer)), result.Size)

	got, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	require.Equal(t, racer, got, "the mid-window racer's bytes are preserved — the pre-shape POSIX rename destroyed them")
	require.Contains(t, logs.String(), "keeping the existing artwork")

	// Both download temps are reaped by the deferred cleanups.
	entries, rerr := os.ReadDir(destDir)
	require.NoError(t, rerr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".full.tmp", "the candidate temp is reaped")
		require.NotContains(t, e.Name(), ".crop.tmp", "the crop temp is reaped")
	}
}

func TestDownloadPosterW51_PromotePublishCompletedRetainsCandidate(t *testing.T) {
	server := covW1BPosterServer()
	defer server.Close()

	base := afero.NewOsFs()
	destDir := t.TempDir()
	joined := errors.Join(fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	fs := &w51PromotePublishErrorFs{Fs: base, err: joined}
	movie := w51PosterMovie("W51-COMPLETED", server.URL+"/poster.jpg")
	dest := w51PosterDest(t, NewDownloader(nil, base, w51PosterConfig(), nil), movie, destDir)
	fs.dest = dest
	d := NewDownloader(server.Client(), fs, w51PosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil)
	require.NoError(t, err, "a completed-despite-error publish is a completed download (wave-42 discipline)")
	require.True(t, fs.fired.Load(), "the completed wedge must have fired")
	require.True(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.NoError(t, result.Error)
	require.Contains(t, logs.String(), "completed despite")

	// The candidate (crop.tmp) was RETAINED for manual cleanup — the wave-42
	// gate, never unverified-unlinked (it may address a foreign occupant) —
	// while the full-download scratch was reaped.
	entries, rerr := os.ReadDir(destDir)
	require.NoError(t, rerr)
	retained := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".crop.tmp") {
			retained = true
		}
		require.NotContains(t, e.Name(), ".full.tmp", "the full-download scratch was reaped")
	}
	require.True(t, retained, "the candidate temp survives for manual cleanup")
}

func TestDownloadPosterW51_PromotePlainFailureKeepsErrorLeg(t *testing.T) {
	server := covW1BPosterServer()
	defer server.Close()

	base := afero.NewOsFs()
	destDir := t.TempDir()
	publishErr := errors.New("promote publish wedged")
	fs := &w51PromotePublishErrorFs{Fs: base, err: publishErr}
	movie := w51PosterMovie("W51-FAIL", server.URL+"/poster.jpg")
	dest := w51PosterDest(t, NewDownloader(nil, base, w51PosterConfig(), nil), movie, destDir)
	fs.dest = dest
	d := NewDownloader(server.Client(), fs, w51PosterConfig(), nil)

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to finalize poster")
	require.ErrorIs(t, err, publishErr)
	require.NotNil(t, result.Error)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.Empty(t, result.LocalPath)

	// Every temp is reaped on the plain-failure leg.
	entries, rerr := os.ReadDir(destDir)
	require.NoError(t, rerr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".tmp", "no temp survives the plain publish failure")
	}
}
