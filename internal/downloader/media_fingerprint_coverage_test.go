package downloader

import (
	"errors"
	"image/color"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type fingerprintMeasureFailFs struct {
	afero.Fs
	opens atomic.Int32
}

func (f *fingerprintMeasureFailFs) Open(name string) (afero.File, error) {
	if f.opens.Add(1) == 2 {
		return nil, errors.New("fingerprint open failed")
	}
	return f.Fs.Open(name)
}

func TestCropDownloadedPosterFingerprintMeasureError(t *testing.T) {
	base := afero.NewMemMapFs()
	full := "/source-full.jpg"
	require.NoError(t, afero.WriteFile(base, full, p4JPEG(color.RGBA{R: 12, G: 34, B: 56, A: 255}), 0o644))
	fs := &fingerprintMeasureFailFs{Fs: base}
	d := NewDownloader(http.DefaultClient, fs, &Config{}, nil)
	bounds := &models.CropBounds{X: 0, Y: 0, Width: 0.5, Height: 1, SourceFingerprint: strings.Repeat("a", 64)}

	ok, _ := d.cropDownloadedPoster(full, "/crop.jpg", bounds)
	require.False(t, ok)
}
