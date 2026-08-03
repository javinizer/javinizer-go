package downloader

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceReplaceFixture spins up an image server and a downloader whose Poster
// template resolves to <tmpDir>/IPX-535-poster.jpg, with a STALE poster file
// pre-installed at that destination. It returns the downloader, the stale
// bytes, the destination, and a hit counter — so tests can prove whether the
// existing file was re-downloaded or kept.
func forceReplaceFixture(t *testing.T, width, height int) (d *Downloader, destPath, serverURL string, hits *int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	hitCount := 0
	hits = &hitCount
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		require.NoError(t, jpeg.Encode(w, img, &jpeg.Options{Quality: 85}))
	}))
	t.Cleanup(server.Close)

	tmpDir := t.TempDir()
	destPath = filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, os.WriteFile(destPath, []byte("stale-poster-from-pass-1"), 0o644))

	cfg := &Config{
		DownloadPoster:  true,
		MaxPosterHeight: 475,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
	return NewDownloader(server.Client(), afero.NewOsFs(), cfg, nil), destPath, server.URL, hits
}

// TestDownloadPoster_ForceReplaceReplacesExistingPoster pins Codex P2-A: the
// apply-phase drift repair re-runs the poster write after a mid-apply edit
// changed the effective poster source WITHOUT setting CropBounds — the
// exists-skip would otherwise keep the poster the first pass installed,
// reporting success while the organized file stays on the OLD image the
// envelope no longer references. With ForceReplacePoster the high-quality
// (no-crop) flow must re-download and REPLACE the destination.
func TestDownloadPoster_ForceReplaceReplacesExistingPoster(t *testing.T) {
	d, destPath, serverURL, hits := forceReplaceFixture(t, 200, 300)

	movie := createTestMovie()
	movie.Poster.PosterURL = serverURL + "/poster.jpg"
	movie.Poster.CoverURL = ""
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = nil

	result, err := d.Download(context.Background(), DownloadCmd{
		Movie:              movie,
		DestDir:            filepath.Dir(destPath),
		ForceReplacePoster: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.NotEqual(t, "stale-poster-from-pass-1", string(got),
		"the forced repair flow must REPLACE the stale poster the first pass wrote")
	assert.Equal(t, 1, *hits, "the forced flow must re-download from the live source")
	assert.Contains(t, result.DownloadedPaths, destPath, "the replaced poster counts as downloaded")
}

// TestDownloadPoster_ForceReplaceSkipsAutoCropRecheck covers the auto-crop
// twin of the exists-skip: ShouldCropPoster=true with nil bounds re-checks
// the destination UNDER the crop lock and would KEEP the stale poster a peer
// (the first pass) installed. ForceReplace must bypass that re-check and
// re-crop from the live source.
func TestDownloadPoster_ForceReplaceSkipsAutoCropRecheck(t *testing.T) {
	d, destPath, serverURL, hits := forceReplaceFixture(t, 800, 1600)

	movie := createTestMovie()
	movie.Poster.PosterURL = serverURL + "/cover.jpg"
	movie.Poster.CoverURL = ""
	movie.Poster.ShouldCropPoster = true
	movie.Poster.CropBounds = nil

	_, err := d.Download(context.Background(), DownloadCmd{
		Movie:              movie,
		DestDir:            filepath.Dir(destPath),
		ForceReplacePoster: true,
	})
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.NotEqual(t, "stale-poster-from-pass-1", string(got),
		"the forced repair flow must replace the poster even on the auto-crop branch")
	assert.Equal(t, 1, *hits, "the re-check-under-lock keep must not fire when replacement is forced")
	imgCfg, dimErr := jpeg.DecodeConfig(bytes.NewReader(got))
	require.NoError(t, dimErr)
	assert.LessOrEqual(t, imgCfg.Height, 475, "the replacement ran the auto-crop, not a straight copy")
}

// TestDownloadPoster_NoForceKeepsExistingPoster is the negative control: with
// no CropBounds and no force flag the downloader MUST keep the existing
// poster — re-running a job never re-downloads already-installed artwork.
func TestDownloadPoster_NoForceKeepsExistingPoster(t *testing.T) {
	d, destPath, serverURL, hits := forceReplaceFixture(t, 200, 300)

	movie := createTestMovie()
	movie.Poster.PosterURL = serverURL + "/poster.jpg"
	movie.Poster.CoverURL = ""
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = nil

	_, err := d.Download(context.Background(), DownloadCmd{
		Movie:   movie,
		DestDir: filepath.Dir(destPath),
	})
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, "stale-poster-from-pass-1", string(got), "without the force flag the exists-skip stands")
	assert.Equal(t, 0, *hits, "without the force flag nothing is re-downloaded")
}
