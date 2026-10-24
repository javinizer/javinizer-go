package imagevalidate

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	"golang.org/x/image/webp"
)

// Limits bounds encoded bytes, each image dimension, and total decoded pixels.
// All budgets must be positive; MaxEncodedBytes must be less than math.MaxInt64
// so validation can read one extra byte to detect oversized input.
type Limits struct {
	MaxEncodedBytes int64
	MaxDimension    int
	MaxPixels       uint64
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// Validate probes and fully decodes JPEG, PNG, or WebP within the supplied limits,
// returning the header configuration and format after checking decoded dimensions.
// It checks cancellation during reads and before and after decoding; it cannot
// interrupt codec CPU work or a blocked input.Read call.
func Validate(ctx context.Context, input io.Reader, limits Limits) (image.Config, string, error) {
	if err := ctx.Err(); err != nil {
		return image.Config{}, "", err
	}
	if limits.MaxEncodedBytes < 1 || limits.MaxEncodedBytes == math.MaxInt64 || limits.MaxDimension < 1 || limits.MaxPixels < 1 {
		return image.Config{}, "", fmt.Errorf("image limits must be positive and encoded limit must permit overflow detection")
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, input}, limits.MaxEncodedBytes+1))
	if err != nil {
		return image.Config{}, "", fmt.Errorf("read image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return image.Config{}, "", err
	}
	if len(data) == 0 || int64(len(data)) > limits.MaxEncodedBytes {
		return image.Config{}, "", fmt.Errorf("image exceeds encoded size budget")
	}
	var probe func(io.Reader) (image.Config, error)
	var decode func(io.Reader) (image.Image, error)
	var format string
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		format = "png"
		probe = png.DecodeConfig
		decode = png.Decode
	case bytes.HasPrefix(data, []byte{0xff, 0xd8}):
		format = "jpeg"
		probe = jpeg.DecodeConfig
		decode = jpeg.Decode
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		format = "webp"
		probe = webp.DecodeConfig
		decode = webp.Decode
	default:
		return image.Config{}, "", fmt.Errorf("unsupported image format")
	}
	config, err := probe(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, "", fmt.Errorf("read image configuration: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > limits.MaxDimension || config.Height > limits.MaxDimension || uint64(config.Width) > limits.MaxPixels/uint64(config.Height) {
		return image.Config{}, "", fmt.Errorf("image dimensions exceed limits: %dx%d", config.Width, config.Height)
	}
	if err := ctx.Err(); err != nil {
		return image.Config{}, "", err
	}
	decoded, err := decode(bytes.NewReader(data))
	if err := ctx.Err(); err != nil {
		return image.Config{}, "", err
	}
	if err != nil {
		return image.Config{}, "", fmt.Errorf("decode image: %w", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return image.Config{}, "", fmt.Errorf("decoded image differs from configuration")
	}
	return config, format, nil
}
