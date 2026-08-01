package imageutil

import (
	"fmt"
	"image"

	"github.com/spf13/afero"
)

// ImageDimensionsFromFile reads only the image header to report the source
// dimensions without decoding pixels.
func ImageDimensionsFromFile(fs afero.Fs, path string) (int, int, error) {
	f, err := fs.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open image: %w", err)
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read image dimensions: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}
