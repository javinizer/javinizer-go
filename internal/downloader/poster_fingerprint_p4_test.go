package downloader

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
)

func p4JPEG(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func TestDownloadPosterFingerprintMismatchFallsBack(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{DownloadPoster: true, MaxPosterHeight: 0, MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"}}
	d := NewDownloader(http.DefaultClient, fs, cfg, nil)
	original := p4JPEG(color.RGBA{R: 20, G: 20, B: 20, A: 255})
	replacement := p4JPEG(color.RGBA{R: 220, G: 220, B: 220, A: 255})
	full := "/tmp/source-full.jpg"
	crop := "/tmp/cropped.jpg"
	if err := afero.WriteFile(fs, full, original, 0o644); err != nil {
		t.Fatal(err)
	}
	bounds := &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1, SourceAspect: 1000.0 / 600.0,
		SourceFingerprint: assetidentity.FromBytes(original).Fingerprint,
	}
	ok, _ := d.cropDownloadedPoster(full, crop, bounds)
	if !ok {
		t.Fatal("same-content source should accept manual geometry")
	}
	if err := afero.WriteFile(fs, full, replacement, 0o644); err != nil {
		t.Fatal(err)
	}
	ok, _ = d.cropDownloadedPoster(full, crop, bounds)
	if ok {
		t.Fatal("same-aspect different-content source must reject stale manual geometry")
	}
}
