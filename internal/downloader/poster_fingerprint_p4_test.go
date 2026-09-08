package downloader

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func p4JPEG(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func TestDownloadPosterFingerprintMismatchRequiresRecrop(t *testing.T) {
	original := p4JPEG(color.RGBA{R: 20, G: 20, B: 20, A: 255})
	replacement := p4JPEG(color.RGBA{R: 220, G: 220, B: 220, A: 255})
	for _, source := range [][]byte{original, replacement} {
		server, _ := identityServer(t, source)
		bounds := &models.CropBounds{Width: .4, Height: 1, SourceAspect: 1000.0 / 600, SourceFingerprint: assetidentity.FromBytes(original).Fingerprint}
		result, err := newGeometryDownloader(afero.NewMemMapFs()).downloadPoster(context.Background(), geometryMovie(server.URL, bounds, false), t.TempDir(), nil)
		if bytes.Equal(source, original) {
			require.NoError(t, err)
			require.True(t, result.Downloaded)
		} else {
			requireIdentityRefusal(t, result, err, SourceFingerprintMismatch, bounds)
		}
	}
}
