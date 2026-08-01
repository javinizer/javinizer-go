package downloader

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoToneCoverServer(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			if x < 500 {
				img.Set(x, y, color.RGBA{R: 220, G: 30, B: 30, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 30, G: 30, B: 220, A: 255})
			}
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 95})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func decodePosterImage(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	require.NoError(t, err)
	return img
}

func newPosterTestDownloader(cfg *Config) *Downloader {
	if cfg.MediaFormatConfig.PosterFormat == "" {
		cfg.MediaFormatConfig = organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"}
	}
	return NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, nil)
}

func TestDownloadPoster_AppliesManualCropBounds(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 400, b.Dx(), "poster must be cropped to the user's manual bounds")
	assert.Equal(t, 600, b.Dy())
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	assert.Greater(t, r, bl, "poster pixels must come from the manual (left/red) crop region")
}

// failOverwriteRenameFs reproduces Windows rename semantics: os.Rename refuses
// to replace an existing destination.
type failOverwriteRenameFs struct {
	afero.Fs
}

func (f *failOverwriteRenameFs) Rename(oldPath, newPath string) error {
	if _, err := f.Fs.Stat(newPath); err == nil {
		return fmt.Errorf("rename %s %s: destination exists (windows semantics)", oldPath, newPath)
	}
	return f.Fs.Rename(oldPath, newPath)
}

func TestDownloadPoster_StaleBoundsKeepWholeReplacesExistingPoster(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &failOverwriteRenameFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	// An organize/update run whose resolved poster already exists must still
	// get the keep-whole replacement — not a rename failure.
	existing := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs, existing, []byte("old poster"), 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err, "keep-whole fallback must replace an existing destination portably")
	require.True(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs, existing)
	require.NoError(t, readErr)
	assert.NotEqual(t, "old poster", string(got), "old poster content must be replaced")
}

func TestDownloadPoster_ManualCropConcurrentSameDestination(t *testing.T) {
	// Multipart movies with a part-less poster template resolve one shared
	// destPath; every part enters the crop branch concurrently. Both the
	// shared <dest>.full.tmp staging file and the final write must be
	// serialized — otherwise one worker deletes/renames the temp another is
	// cropping.
	for iter := 0; iter < 5; iter++ {
		srv := twoToneCoverServer(t)
		fs := afero.NewMemMapFs()
		tmpDir := "/out"
		require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

		movie := func() *models.Movie {
			m := createTestMovie()
			m.Poster.PosterURL = srv.URL + "/cover.jpg"
			m.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}
			return m
		}

		d := NewDownloader(http.DefaultClient, fs, &Config{
			DownloadPoster:    true,
			MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
		}, nil)

		const workers = 8
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := d.downloadPoster(context.Background(), movie(), tmpDir, nil)
				if err != nil {
					errs <- err
					return
				}
				if !res.Downloaded {
					errs <- fmt.Errorf("expected Downloaded=true")
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent manual crop failed: %v", err)
		}

		img := decodePosterImageFs(t, fs, filepath.Join(tmpDir, "IPX-535-poster.jpg"))
		b := img.Bounds()
		require.Equal(t, 400, b.Dx())
		require.Equal(t, 600, b.Dy())
		srv.Close()
	}
}

func decodePosterImageFs(t *testing.T, fs afero.Fs, path string) image.Image {
	t.Helper()
	f, err := fs.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	require.NoError(t, err)
	return img
}

func TestDownloadPoster_StaleStagingTempDoesNotShortCircuitCrop(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	// A crashed run can leave <dest>.full.tmp behind; d.download would mistake
	// it for a completed download and the crop would never run.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "IPX-535-poster.jpg.full.tmp"), []byte("stale stage"), 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded, "stale staging file must not short-circuit the manual crop")

	img := decodePosterImage(t, result.LocalPath)
	assert.Equal(t, 400, img.Bounds().Dx())
	assert.Equal(t, 600, img.Bounds().Dy())
}

// ENOSPC fs: Create succeeds (truncating), then writes fail — simulates a disk
// filling up mid-encode.
type enospcFs struct {
	afero.Fs
	after int
}

func (f *enospcFs) Create(name string) (afero.File, error) {
	base, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(name, "-poster.jpg") && !strings.HasSuffix(name, "-poster.jpg.crop.tmp") {
		return base, nil
	}
	return &limitedWriteFile{File: base, remaining: f.after}, nil
}

type limitedWriteFile struct {
	afero.File
	remaining int
}

func (l *limitedWriteFile) Write(p []byte) (int, error) {
	if len(p) > l.remaining {
		var n int
		if l.remaining > 0 {
			n, _ = l.File.Write(p[:l.remaining])
			l.remaining -= n
		}
		return n, fmt.Errorf("simulated ENOSPC")
	}
	n, err := l.File.Write(p)
	l.remaining -= n
	return n, err
}

func TestDownloadPoster_DownloadFailureInsideCropPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	movie := createTestMovie()
	movie.Poster.PosterURL = "http://127.0.0.1:1/never.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "a failed download inside the crop pipeline must fail the poster step")
	require.False(t, result.Downloaded)

	// No stale staging may survive behind.
	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.False(t, strings.Contains(e.Name(), "IPX-535-poster.jpg"), "stale stage left behind: %s", e.Name())
	}
}

// failRenameFs forces failure when installing the crop stage to the final
// destination (e.g. Windows rename-to-existing or cross-device moves).
type failRenameFs struct {
	afero.Fs
}

func (f *failRenameFs) Rename(oldPath, newPath string) error {
	if strings.HasSuffix(oldPath, ".crop.tmp") {
		return fmt.Errorf("forced install rename failure")
	}
	return f.Fs.Rename(oldPath, newPath)
}

// permFailRemoveFs refuses to delete poster staging files (permissions /
// Windows sharing lock).
type permFailRemoveFs struct {
	afero.Fs
}

func (f *permFailRemoveFs) Remove(name string) error {
	if strings.HasSuffix(name, ".full.tmp") || strings.HasSuffix(name, ".crop.tmp") {
		return fmt.Errorf("forced removal failure (locked file)")
	}
	return f.Fs.Remove(name)
}

func TestDownloadPoster_StaleStageRemovalFailureFailsLoudly(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &permFailRemoveFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))
	require.NoError(t, afero.WriteFile(fs.Fs, filepath.Join(tmpDir, "IPX-535-poster.jpg.full.tmp"), []byte("stale"), 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "unremovable stale stage must fail the step, not skip the crop silently")
	require.False(t, result.Downloaded)
}

func TestDownloadPoster_FailedInstallRestoresExistingPoster(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &failRenameFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	oldBytes := []byte("pre-existing valid poster")
	dest := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs.Fs, dest, oldBytes, 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err)
	require.False(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs.Fs, dest)
	require.NoError(t, readErr)
	assert.Equal(t, oldBytes, got, "the old poster must be rolled back when the install rename fails")
}

// failBackupRenameFs refuses to stage the existing destination aside.
type failBackupRenameFs struct {
	afero.Fs
}

func (f *failBackupRenameFs) Rename(oldPath, newPath string) error {
	if strings.HasSuffix(newPath, ".bak") {
		return fmt.Errorf("forced backup rename failure")
	}
	return f.Fs.Rename(oldPath, newPath)
}

func TestDownloadPoster_BackupRenameFailureLeavesOldPosterIntact(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &failBackupRenameFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	oldBytes := []byte("pre-existing valid poster")
	dest := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs.Fs, dest, oldBytes, 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err)
	require.False(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs.Fs, dest)
	require.NoError(t, readErr)
	assert.Equal(t, oldBytes, got, "failed backup must leave the old poster untouched")
	exists, statErr := afero.Exists(fs, dest+".crop.tmp")
	require.NoError(t, statErr)
	assert.False(t, exists, "crop stage must be cleaned up")
}

func TestDownloadPoster_InterruptedBackupRecoveredBeforeReplace(t *testing.T) {
	srv := twoToneCoverServer(t)

	// Previous run died after dest→.bak but before installing the crop: the
	// only copy of the old poster lives in the backup. A failed install must
	// roll back to it, never leave the slot empty.
	fs := &failRenameFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	oldBytes := []byte("old poster survives only as backup")
	dest := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs.Fs, dest+".bak", oldBytes, 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "install rename is forced to fail")
	require.False(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs.Fs, dest)
	require.NoError(t, readErr, "the backup must be recovered/rolled back — the slot must not end up empty")
	assert.Equal(t, oldBytes, got)
}

// failRestoreRenameFs refuses the crash-recovery rename (.bak -> dest).
type failRestoreRenameFs struct {
	afero.Fs
}

func (f *failRestoreRenameFs) Rename(oldPath, newPath string) error {
	if strings.HasSuffix(oldPath, ".bak") {
		return fmt.Errorf("forced restore failure")
	}
	return f.Fs.Rename(oldPath, newPath)
}

func TestDownloadPoster_CrashedBackupRestoredBeforeFailedDownload(t *testing.T) {
	fs := afero.NewMemMapFs()
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	oldBytes := []byte("only surviving poster bytes")
	dest := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs, dest+".bak", oldBytes, 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = "http://127.0.0.1:1/never.jpg" // download itself fails
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err)
	require.False(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs, dest)
	require.NoError(t, readErr, "the crashed backup must be restored even when the retry download fails")
	assert.Equal(t, oldBytes, got)
}

func TestDownloadPoster_BackupRecoveryFailureFailsLoudly(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &failRestoreRenameFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	oldBytes := []byte("only surviving poster bytes")
	dest := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs.Fs, dest+".bak", oldBytes, 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "a failed backup recovery must surface, not be overwritten silently")
	require.False(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs.Fs, dest+".bak")
	require.NoError(t, readErr, "the only surviving copy must not be destroyed")
	assert.Equal(t, oldBytes, got)
}

func TestDownloadPoster_BoundsScaleToResizedSource(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	// The user cropped the preview at 2000x1200; Organize downloads the same
	// image at 1000x600 — in-range after scaling, wrong region without it.
	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{
		X: 0, Y: 0, Width: 800, Height: 1200,
		ImageWidth: 2000, ImageHeight: 1200, SourceWasCover: true,
	}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 400, b.Dx(), "bounds must scale 0.5x to the downloaded resolution")
	assert.Equal(t, 600, b.Dy())
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	assert.Greater(t, r, bl, "scaled crop must land in the user's (left/red) region")
}

func twoToneImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.Set(x, y, color.RGBA{R: 220, G: 30, B: 30, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 30, G: 30, B: 220, A: 255})
			}
		}
	}
	return img
}

func imageServer(t *testing.T, img image.Image) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 95})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadPoster_ScaledBoundsTouchingEdgeDoNotOverflow(t *testing.T) {
	srv := imageServer(t, twoToneImage(500, 300))
	tmpDir := t.TempDir()

	// Preview was measured at 1000x600 (x=333..1000 touches the right edge);
	// organize downloads the same image at 500x300. Rounding origin and size
	// independently overflows (167+334=501 > 500) and would fall back, losing
	// the user's crop; edge-based scaling must yield width 333 (500-167).
	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{
		X: 333, Y: 0, Width: 667, Height: 600,
		ImageWidth: 1000, ImageHeight: 600, SourceWasCover: true,
	}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 333, b.Dx(), "edge-scaled crop must fit the resized image exactly")
	assert.Equal(t, 300, b.Dy())
}

func TestDownloadPoster_InstallRenameFailureSurfaces(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &failRenameFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "a failed install rename must surface as a poster failure")
	require.False(t, result.Downloaded)

	exists, statErr := afero.Exists(fs, filepath.Join(tmpDir, "IPX-535-poster.jpg.crop.tmp"))
	require.NoError(t, statErr)
	assert.False(t, exists, "crop stage must be cleaned up after a failed install")
}

func TestDownloadPoster_FailedCropLeavesExistingPosterIntact(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &enospcFs{Fs: afero.NewMemMapFs(), after: 32}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	oldBytes := []byte("pre-existing valid poster content")
	dest := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs.Fs, dest, oldBytes, 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "failed write must surface as a poster failure")
	require.False(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs.Fs, dest)
	require.NoError(t, readErr)
	assert.Equal(t, oldBytes, got, "a failed crop must not destroy the pre-existing poster")
}

func TestDownloadPoster_BoundsCarryMaxPosterHeight(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	// The crop preview honored an explicit 300px max height; Organize must
	// produce the same output height instead of the config default (0 = uncapped).
	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600, MaxPosterHeight: 300}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 300, b.Dy(), "the stored max poster height must be honored at apply")
	assert.Equal(t, 200, b.Dx(), "aspect 400:600 scales to 200:300")
}

func TestDownloadPoster_UndecodableDownloadDoesNotShipAsPoster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>scraper error page</html>"))
	}))
	t.Cleanup(srv.Close)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/oops"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "an undecodable download must fail the poster step, not be renamed into place")
	require.False(t, result.Downloaded)
	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.False(t, strings.HasSuffix(e.Name(), "-poster.jpg"), "no poster file should be written: %s", e.Name())
	}
}

func TestDownloadPoster_TruncatedDownloadDoesNotShipAsPoster(t *testing.T) {
	coverBytes := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 1000; x++ {
				img.Set(x, y, color.RGBA{R: 220, G: 30, B: 30, A: 255})
			}
		}
		var buf bytes.Buffer
		require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}))
		return buf.Bytes()
	}()
	truncated := coverBytes[:len(coverBytes)/2]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(truncated)
	}))
	t.Cleanup(srv.Close)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "a header-valid but body-truncated image must not be renamed into place")
	require.False(t, result.Downloaded)
}

func TestDownloadPoster_ManualCropOverwritesExistingPoster(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	// A poster already on disk from a previous organize/update must be replaced
	// by the user's explicit crop, not silently kept.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "IPX-535-poster.jpg"), []byte("old poster"), 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 400, b.Dx(), "existing poster must be replaced by the manual crop")
	assert.Equal(t, 600, b.Dy())
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	assert.Greater(t, r, bl)
}

func TestDownloadPoster_ExistingPosterWithoutBoundsStillSkipped(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "IPX-535-poster.jpg"), []byte("old poster"), 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.False(t, result.Downloaded, "existing poster without manual bounds keeps the skip-existing behavior")

	content, err := os.ReadFile(result.LocalPath)
	require.NoError(t, err)
	assert.Equal(t, "old poster", string(content))
}

func TestDownloadPoster_StaleCropBoundsOnPosterGradeSourceSaveWhole(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false // poster-grade scraper source, then user-cropped
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 1000, b.Dx(), "stale bounds on a poster-grade source must not butcher the image with an auto-crop")
	assert.Equal(t, 600, b.Dy())
}

func TestDownloadPoster_StaleCropBoundsFallBackToDefaultCrop(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false // set by the manual crop itself

	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600, SourceWasCover: true}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err, "stale bounds must not fail the poster download")
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Less(t, b.Dx(), b.Dy(), "cover-shaped source with stale bounds degrades to the default portrait crop")
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	assert.Greater(t, bl, r, "fallback crop is the default right-side auto-crop")
}
