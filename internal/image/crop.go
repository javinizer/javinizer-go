package image

import (
	"fmt"
	"image"
	"image/jpeg"

	_ "image/png" // Register PNG decoder

	"github.com/spf13/afero"
	"golang.org/x/image/draw"
)

const (
	// MaxPosterHeight is the maximum height for poster images (optimized for Jellyfin/Plex/Emby)
	// Reduced from 1000px to 500px for better file sizes and performance
	MaxPosterHeight = 500

	// LandscapeAspectRatioThreshold determines if an image is landscape-oriented
	// Images with width/height > this value are considered landscape (typical JAV covers are ~1.5)
	LandscapeAspectRatioThreshold = 1.2
)

// CropPosterFromCover intelligently crops a cover image to create a poster
//
// Strategy:
// - Landscape images (aspect ratio > 1.2): Crop right 47.2% (original Javinizer behavior)
// - Square/Portrait images (aspect ratio <= 1.2): Center crop to 2:3 aspect ratio
// - If result exceeds MaxPosterHeight, resize maintaining aspect ratio
//
// This ensures good results for both wide JAV covers and square promotional images
func CropPosterFromCover(fs afero.Fs, coverPath, posterPath string) error {
	// Open and decode the cover image
	coverFile, err := fs.Open(coverPath)
	if err != nil {
		return fmt.Errorf("failed to open cover image: %w", err)
	}
	defer coverFile.Close()

	img, format, err := image.Decode(coverFile)
	if err != nil {
		return fmt.Errorf("failed to decode cover image: %w", err)
	}

	// Get image dimensions
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Validate dimensions to prevent division by zero
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", width, height)
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

	// Create cropped image
	croppedBounds := image.Rect(0, 0, right-left, bottom-top)
	cropped := image.NewRGBA(croppedBounds)

	// Copy the cropped portion
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			cropped.Set(x-left, y-top, img.At(x, y))
		}
	}

	// Resize if the cropped poster height exceeds MaxPosterHeight
	var finalImage image.Image = cropped
	croppedHeight := bottom - top
	if croppedHeight > MaxPosterHeight {
		// Calculate new dimensions maintaining aspect ratio
		croppedWidth := right - left
		scale := float64(MaxPosterHeight) / float64(croppedHeight)
		newWidth := int(float64(croppedWidth) * scale)
		newHeight := MaxPosterHeight

		// Create resized image using high-quality bilinear interpolation
		resizedBounds := image.Rect(0, 0, newWidth, newHeight)
		resized := image.NewRGBA(resizedBounds)
		draw.BiLinear.Scale(resized, resizedBounds, cropped, cropped.Bounds(), draw.Over, nil)
		finalImage = resized
	}

	// Create output file
	posterFile, err := fs.Create(posterPath)
	if err != nil {
		return fmt.Errorf("failed to create poster file: %w", err)
	}
	defer posterFile.Close()

	// Encode as JPEG with high quality
	opts := &jpeg.Options{Quality: 95}
	if err := jpeg.Encode(posterFile, finalImage, opts); err != nil {
		return fmt.Errorf("failed to encode poster image: %w", err)
	}

	// Log format for debugging
	_ = format

	return nil
}
