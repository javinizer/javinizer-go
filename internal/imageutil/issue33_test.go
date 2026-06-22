package imageutil

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/spf13/afero"
)

// TestIssue33_MaxPosterHeight verifies issue #33 is fixed: a high-res cover
// (2184x1468, matching the issue's report) is no longer unconditionally
// downscaled to 500px. With maxPosterHeight=0 (the new default) the cropped
// poster preserves the source resolution.
func TestIssue33_MaxPosterHeight(t *testing.T) {
	srcW, srcH := 2184, 1468
	fs := afero.NewMemMapFs()
	coverPath := "/cover.jpg"
	coverFile, err := fs.Create(coverPath)
	if err != nil {
		t.Fatalf("create cover: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			img.Set(x, y, color.RGBA{R: 60, G: 90, B: 140, A: 255})
		}
	}
	if err := jpeg.Encode(coverFile, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode cover: %v", err)
	}
	coverFile.Close()

	cases := []struct {
		name            string
		maxPosterHeight int
		wantHeight      int
		wantWidth       int
	}{
		// OLD behavior — cap=500 produces the 351x500 the user complained about.
		// Crop: right 47.2% of 2184 = 1032 wide, 1468 tall. Then resize to 500
		// height → 1032 * 500/1468 = 351.46 → int truncates to 351.
		{"old hardcoded 500 cap", 500, 500, 351},
		// NEW default — no cap, source crop preserved (1032x1468).
		{"new default 0 no cap", 0, 1468, 1032},
		// NEW custom cap 1000 → downscaled to 1000 height, preserving aspect.
		// width = 1032 * 1000/1468 = 702.99 → int truncates to 702.
		{"custom cap 1000", 1000, 1000, 702},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			posterPath := fmt.Sprintf("/poster_%d.jpg", tc.maxPosterHeight)
			if err := CropPosterFromCover(fs, coverPath, posterPath, tc.maxPosterHeight); err != nil {
				t.Fatalf("CropPosterFromCover: %v", err)
			}
			posterFile, err := fs.Open(posterPath)
			if err != nil {
				t.Fatalf("open poster: %v", err)
			}
			decoded, _, err := image.Decode(posterFile)
			posterFile.Close()
			if err != nil {
				t.Fatalf("decode poster: %v", err)
			}
			b := decoded.Bounds()
			if b.Dx() != tc.wantWidth {
				t.Errorf("width = %d, want %d", b.Dx(), tc.wantWidth)
			}
			if b.Dy() != tc.wantHeight {
				t.Errorf("height = %d, want %d", b.Dy(), tc.wantHeight)
			}
		})
	}
}
