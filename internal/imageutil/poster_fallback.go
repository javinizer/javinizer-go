package imageutil

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"os"

	"github.com/spf13/afero"
	"golang.org/x/image/draw"
)

// RenderPosterFromCover generates an automatic fallback, leaving the historical
// crop unchanged when mode is empty or legacy. Explicit manual crops do not use
// this entry point.
func RenderPosterFromCover(fs afero.Fs, sourcePath, posterPath string, maxHeight int, mode string) (os.FileInfo, error) {
	if mode == "" || mode == "legacy" {
		return CropPosterFromCover(fs, sourcePath, posterPath, maxHeight)
	}
	img, _, _, err := decodePosterSource(fs, sourcePath)
	if err != nil {
		return nil, err
	}
	return renderPosterFallback(fs, img, posterPath, maxHeight, mode)
}

// RenderPosterFromCoverReader consumes the caller's verified source snapshot;
// it never reopens a mutable source path after manual-source verification.
func RenderPosterFromCoverReader(fs afero.Fs, source io.Reader, posterPath string, maxHeight int, mode string) (os.FileInfo, error) {
	if mode == "" || mode == "legacy" {
		return CropPosterFromCoverReader(fs, source, posterPath, maxHeight)
	}
	img, _, err := image.Decode(source)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cover image: %w", err)
	}
	return renderPosterFallback(fs, img, posterPath, maxHeight, mode)
}

func renderPosterFallback(fs afero.Fs, img image.Image, posterPath string, maxHeight int, mode string) (os.FileInfo, error) {
	if mode != "center" && mode != "fit" {
		return nil, fmt.Errorf("invalid poster fallback mode: %q", mode)
	}
	// Exact integer 2:3 output needs at least 2x3 pixels. Never silently exceed
	// an explicit height cap or emit a non-portrait image to satisfy a tiny cap.
	if maxHeight < 0 || maxHeight == 1 || maxHeight == 2 {
		return nil, fmt.Errorf("poster fallback height must be 0 or at least 3")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid image dimensions: %dx%d", w, h)
	}
	sourceRect := b
	var units int
	if mode == "center" {
		// Use the largest exact 2:3 source rectangle whenever it fits. Tiny
		// sources are the only case requiring enlargement to the minimum 2x3.
		units = min(w/2, h/3)
		if units > 0 {
			cw, ch := 2*units, 3*units
			sourceRect = image.Rect(b.Min.X+(w-cw)/2, b.Min.Y+(h-ch)/2, b.Min.X+(w-cw)/2+cw, b.Min.Y+(h-ch)/2+ch)
		} else {
			cw, ch := min(w, 2), min(h, 3)
			sourceRect = image.Rect(b.Min.X+(w-cw)/2, b.Min.Y+(h-ch)/2, b.Min.X+(w-cw)/2+cw, b.Min.Y+(h-ch)/2+ch)
			units = 1
		}
	} else {
		// Ceiling division retains source resolution unless a cap requests a
		// smaller canvas. Scale below is capped at 1: fit never enlarges content.
		units = max((w+1)/2, (h+2)/3)
	}
	if maxHeight > 0 {
		units = min(units, maxHeight/3)
	}
	// Bound the output before multiplication/allocation. Fit padding can otherwise
	// amplify a small, very skinny source into a multi-gigabyte canvas. 1672
	// is floor(sqrt((16*1024*1024)/6)), the largest 2:3 unit count below 16 Mi pixels.
	if units > 1672 {
		return nil, fmt.Errorf("poster fallback exceeds 16 Mi pixels; set max_poster_height to 5016 or less")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 2*units, 3*units))
	target := canvas.Bounds()
	if mode == "fit" {
		draw.Draw(canvas, target, image.NewUniform(color.RGBA{32, 32, 32, 255}), image.Point{}, draw.Src)
		scale := min(1.0, min(float64(target.Dx())/float64(w), float64(target.Dy())/float64(h)))
		fw, fh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
		left, top := (target.Dx()-fw)/2, (target.Dy()-fh)/2
		target = image.Rect(left, top, left+fw, top+fh)
	}
	draw.BiLinear.Scale(canvas, target, img, sourceRect, draw.Src, nil)
	// Reuse the existing producer-bound writer and its FileInfo identity. The
	// downloader's overwrite, atomic installation, cleanup and revert stay intact.
	return cropAndWritePoster(fs, canvas, posterPath, 0, 0, canvas.Bounds().Dx(), canvas.Bounds().Dy(), 0)
}
