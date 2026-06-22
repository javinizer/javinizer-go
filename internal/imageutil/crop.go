package imageutil

import (
	"fmt"
	"image"
	"image/jpeg"

	_ "golang.org/x/image/webp" // Register WebP decoder
	_ "image/png"               // Register PNG decoder

	"github.com/spf13/afero"
	"golang.org/x/image/draw"
)

const (
	// DefaultMaxPosterHeight is the maximum height for poster images when no
	// explicit value is provided. Set to 0 (no cap) to preserve source
	// resolution — previous versions hard-capped at 500px, which silently
	// degraded high-resolution posters (see issue #33).
	DefaultMaxPosterHeight = 0

	// LandscapeAspectRatioThreshold determines if an image is landscape-oriented
	// Images with width/height > this value are considered landscape (typical JAV covers are ~1.5)
	LandscapeAspectRatioThreshold = 1.2
)

// CropPosterFromCover intelligently crops a cover image to create a poster.
//
// Strategy:
// - Landscape images (aspect ratio > 1.2): Crop right 47.2% (original Javinizer behavior)
// - Square/Portrait images (aspect ratio <= 1.2): Center crop to 2:3 aspect ratio
// - If maxPosterHeight > 0 and the result exceeds it, resize maintaining aspect ratio
//
// Pass maxPosterHeight=0 to preserve the cropped source resolution (no resize).
//
// This ensures good results for both wide JAV covers and square promotional images
func CropPosterFromCover(fs afero.Fs, coverPath, posterPath string, maxPosterHeight int) error {
	img, width, height, err := decodePosterSource(fs, coverPath)
	if err != nil {
		return err
	}

	// Calculate aspect ratio to determine crop strategy
	aspectRatio := float64(width) / float64(height)

	var left, top, right, bottom int

	if aspectRatio > LandscapeAspectRatioThreshold {
		// Landscape image: Use right-side crop (original Javinizer method)
		// left = width / 1.895734597 keeps the right 47.2% of the image
		left = int(float64(width) / 1.895734597)
		top = 0
		right = width
		bottom = height
	} else {
		// Square or portrait image: Use center crop with 2:3 aspect ratio
		// Target aspect ratio for posters is 2:3 (width:height)
		targetAspectRatio := 2.0 / 3.0

		// Calculate crop dimensions to achieve 2:3 aspect ratio
		var cropWidth, cropHeight int
		if float64(width)/float64(height) > targetAspectRatio {
			// Image is wider than 2:3, crop width
			cropHeight = height
			cropWidth = int(float64(cropHeight) * targetAspectRatio)
		} else {
			// Image is taller than 2:3, crop height
			cropWidth = width
			cropHeight = int(float64(cropWidth) / targetAspectRatio)
		}

		// Center the crop
		left = (width - cropWidth) / 2
		top = (height - cropHeight) / 2
		right = left + cropWidth
		bottom = top + cropHeight
	}

	return cropAndWritePoster(fs, img, posterPath, left, top, right, bottom, maxPosterHeight)
}

// CropPosterWithBounds crops a cover image using explicit pixel bounds.
// Bounds are in source-image pixels and must be within the image dimensions.
// If maxPosterHeight > 0 and the cropped result exceeds it, the output is
// downscaled preserving aspect ratio. Pass 0 to preserve the source resolution.
func CropPosterWithBounds(fs afero.Fs, coverPath, posterPath string, left, top, right, bottom, maxPosterHeight int) error {
	img, width, height, err := decodePosterSource(fs, coverPath)
	if err != nil {
		return err
	}

	if left < 0 || top < 0 || right > width || bottom > height {
		return fmt.Errorf("crop bounds out of range: left=%d top=%d right=%d bottom=%d image=%dx%d",
			left, top, right, bottom, width, height)
	}
	if left >= right || top >= bottom {
		return fmt.Errorf("invalid crop bounds: left=%d top=%d right=%d bottom=%d",
			left, top, right, bottom)
	}

	return cropAndWritePoster(fs, img, posterPath, left, top, right, bottom, maxPosterHeight)
}

func decodePosterSource(fs afero.Fs, coverPath string) (image.Image, int, int, error) {
	coverFile, err := fs.Open(coverPath)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to open cover image: %w", err)
	}
	defer func() { _ = coverFile.Close() }()

	img, _, err := image.Decode(coverFile)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to decode cover image: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}

	return img, width, height, nil
}

func cropAndWritePoster(fs afero.Fs, img image.Image, posterPath string, left, top, right, bottom, maxPosterHeight int) error {
	cropRect := image.Rect(left, top, right, bottom)
	croppedWidth := right - left
	croppedHeight := bottom - top

	var cropped image.Image
	if sub, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	}); ok {
		cropped = sub.SubImage(cropRect)
	} else {
		rgba := image.NewRGBA(image.Rect(0, 0, croppedWidth, croppedHeight))
		draw.Draw(rgba, rgba.Bounds(), img, image.Pt(left, top), draw.Src)
		cropped = rgba
	}

	var finalImage = cropped
	if maxPosterHeight > 0 && croppedHeight > maxPosterHeight {
		scale := float64(maxPosterHeight) / float64(croppedHeight)
		newWidth := int(float64(croppedWidth) * scale)
		newHeight := maxPosterHeight

		resizedBounds := image.Rect(0, 0, newWidth, newHeight)
		resized := image.NewRGBA(resizedBounds)
		draw.BiLinear.Scale(resized, resizedBounds, cropped, cropped.Bounds(), draw.Over, nil)
		finalImage = resized
	}

	posterFile, err := fs.Create(posterPath)
	if err != nil {
		return fmt.Errorf("failed to create poster file: %w", err)
	}
	defer func() { _ = posterFile.Close() }()

	opts := &jpeg.Options{Quality: 95}
	if err := jpeg.Encode(posterFile, finalImage, opts); err != nil {
		return fmt.Errorf("failed to encode poster image: %w", err)
	}

	return nil
}
