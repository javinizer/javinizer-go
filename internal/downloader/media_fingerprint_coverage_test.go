package downloader

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCropDownloadedPosterUndecodable(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := t.TempDir()
	d := newGeometryDownloader(fs)
	bounds := &models.CropBounds{Width: .5, Height: 1}
	ok, _ := d.cropDownloadedPoster([]byte("not an image"), filepath.Join(root, "crop.jpg"), bounds)
	require.False(t, ok)
	entries, _ := afero.ReadDir(fs, root)
	require.Empty(t, entries)
}
