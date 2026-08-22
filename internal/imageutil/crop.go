package imageutil

import (
	"fmt"
	"image"
	"image/jpeg"
	"os"

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

// ImageDimensions reads pixel dimensions from an image header without
// decoding the pixel data.
func ImageDimensions(fs afero.Fs, path string) (int, int, error) {
	f, err := fs.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// CropPosterFromCover intelligently crops a cover image to create a poster.
//
// Strategy:
// - Landscape images (aspect ratio > 1.2): Crop right 47.2% (original Javinizer behavior)
// - Square/Portrait images (aspect ratio <= 1.2): Center crop to 2:3 aspect ratio
// - If maxPosterHeight > 0 and the result exceeds it, resize maintaining aspect ratio
//
// Pass maxPosterHeight=0 to preserve the cropped source resolution (no resize).
//
// # This ensures good results for both wide JAV covers and square promotional images
//
// On success the written poster's own FileInfo rides back with the result
// (wave-67, codex P2, PR#215 — see cropAndWritePoster): the caller freezes
// THAT producer record as the candidate's bind identity instead of
// re-deriving the mutable name after the producer returned.
func CropPosterFromCover(fs afero.Fs, coverPath, posterPath string, maxPosterHeight int) (os.FileInfo, error) {
	img, width, height, err := decodePosterSource(fs, coverPath)
	if err != nil {
		return nil, err
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
func CropPosterWithBounds(fs afero.Fs, coverPath, posterPath string, left, top, right, bottom, maxPosterHeight int) (os.FileInfo, error) {
	img, width, height, err := decodePosterSource(fs, coverPath)
	if err != nil {
		return nil, err
	}

	if left < 0 || top < 0 || right > width || bottom > height {
		return nil, fmt.Errorf("crop bounds out of range: left=%d top=%d right=%d bottom=%d image=%dx%d",
			left, top, right, bottom, width, height)
	}
	if left >= right || top >= bottom {
		return nil, fmt.Errorf("invalid crop bounds: left=%d top=%d right=%d bottom=%d",
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

// cropAndWritePoster writes the cropped JPEG and hands back the WRITTEN
// object's identity with its result (wave-67, codex P2, PR#215 — the
// downloader's producer-side provenance bind): the identity is captured
// FROM THE OPEN WRITE HANDLE before close (wave-68, codex P2, PR#215 F1
// — the fstat names exactly the object we wrote, so a substitute rotated
// onto the name inside the close→identity-capture window cannot
// authenticate against itself downstream), from INSIDE the producer, so
// no caller-side re-lookup of the mutable name after the crop returned can
// authenticate a substitute rotated onto it in between. On a filesystem
// whose FileInfo.Sys() carries kernel identity (the real OsFs, whose
// close does not re-stamp ModTime) the pre-close fstat IS the binding
// record; a legacy/virtual fs whose Sys() is nil (afero MemMapFs
// re-stamps ModTime at close, so the pre-close ModTime would name a record
// no later lookup can match) falls back to the post-close no-follow
// lookup for the durable ModTime — the wave-67 posture preserved for the
// no-identity leg only. A failed capture fails the write leg closed — a
// producer that cannot prove its own record hands nothing down (callers
// keep their refuse-closed posture).
func cropAndWritePoster(fs afero.Fs, img image.Image, posterPath string, left, top, right, bottom, maxPosterHeight int) (os.FileInfo, error) {
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
		return nil, fmt.Errorf("failed to create poster file: %w", err)
	}

	opts := &jpeg.Options{Quality: 95}
	if err := jpeg.Encode(posterFile, finalImage, opts); err != nil {
		_ = posterFile.Close()
		return nil, fmt.Errorf("failed to encode poster image: %w", err)
	}

	// Capture the written object's identity FROM THE OPEN WRITE HANDLE before
	// close (wave-68, codex P2, PR#215 F1): the fstat names exactly the object
	// we wrote — a substitute rotated onto the name after close cannot
	// authenticate against it downstream. See writtenPosterIdentity for the
	// legacy/virtual-fs fallback (Sys() carries no identity).
	preCloseInfo, statErr := posterFile.Stat()
	_ = posterFile.Close()
	if statErr != nil {
		return nil, fmt.Errorf("failed to stat written poster file: %w", statErr)
	}
	return writtenPosterIdentity(fs, posterPath, preCloseInfo)
}

// writtenPosterIdentity selects the crop producer's write-leg identity record
// (wave-68, codex P2, PR#215 F1): when the open handle's pre-close fstat
// carries filesystem identity (FileInfo.Sys() non-nil — the real OsFs, whose
// close does not re-stamp ModTime), it IS the binding record — a substitute
// rotated onto the name after close cannot authenticate against it. A
// legacy/virtual fs whose Sys() is nil (afero MemMapFs re-stamps ModTime at
// close) falls back to the post-close no-follow lookup for the durable
// ModTime (the wave-67 posture preserved for the no-identity leg only).
func writtenPosterIdentity(fs afero.Fs, posterPath string, preCloseInfo os.FileInfo) (os.FileInfo, error) {
	if preCloseInfo.Sys() != nil {
		return preCloseInfo, nil
	}
	return lstatWrittenPoster(fs, posterPath)
}

// lstatWrittenPoster is the crop producers' write-leg identity seam for
// legacy/virtual filesystems whose FileInfo.Sys() carries no kernel identity
// (wave-67; wave-68, codex P2, PR#215 F1 narrowed it to this fallback): a
// no-follow post-write lookup of the just-written poster name, folded
// through the producer so the record rides out with the result. The real
// OsFs leg captures the identity from the OPEN handle before close instead
// (see writtenPosterIdentity) — its close does not re-stamp ModTime, so the
// pre-close fstat names a record a later lookup can match and a substitute
// rotated onto the name after close cannot authenticate against itself.
func lstatWrittenPoster(fs afero.Fs, posterPath string) (os.FileInfo, error) {
	var info os.FileInfo
	var err error
	if ls, ok := fs.(afero.Lstater); ok {
		info, _, err = ls.LstatIfPossible(posterPath)
	} else {
		info, err = fs.Stat(posterPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat written poster file: %w", err)
	}
	return info, nil
}
