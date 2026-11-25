package imagevalidate

import (
	"image"
	"image/color"
	"testing"
)

func TestConfirmDecodedDimensions(t *testing.T) {
	config := image.Config{ColorModel: color.RGBAModel, Width: 4, Height: 3}
	const format = "png"
	for _, tt := range []struct {
		name          string
		width, height int
		wantError     bool
	}{
		{"matches at non-zero origin", 4, 3, false},
		{"width mismatch", 5, 3, true},
		{"height mismatch", 4, 2, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bounds := image.NewRGBA(image.Rect(7, 11, 7+tt.width, 11+tt.height)).Bounds()
			gotConfig, gotFormat, err := confirmDecodedDimensions(config, format, bounds)
			if tt.wantError {
				if err == nil || err.Error() != "decoded image differs from configuration" {
					t.Fatalf("mismatch error = %v", err)
				}
				if gotConfig != (image.Config{}) || gotFormat != "" {
					t.Fatalf("mismatch exposed metadata: %+v, %q", gotConfig, gotFormat)
				}
				return
			}
			if err != nil || gotConfig.Width != config.Width || gotConfig.Height != config.Height ||
				gotConfig.ColorModel != config.ColorModel || gotFormat != format {
				t.Fatalf("match returned %+v, %q, %v", gotConfig, gotFormat, err)
			}
		})
	}
}
