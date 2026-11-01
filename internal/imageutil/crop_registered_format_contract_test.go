package imageutil_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/draw"
)

const cropFixtureMagic = "JV-CROP-RGB-V1"

// A valid image.Image is not required to implement the optional SubImage
// method. Registered decoders may therefore return this deliberately narrow
// wrapper. The codec is pure and uniquely named: no global callback captures
// test state or changes the behavior of a production image format.
type cropFixtureImage struct{ image.Image }

func cropFixtureConfig(r io.Reader) (image.Config, error) {
	header := make([]byte, len(cropFixtureMagic)+2)
	if _, err := io.ReadFull(r, header); err != nil {
		return image.Config{}, err
	}
	if string(header[:len(cropFixtureMagic)]) != cropFixtureMagic {
		return image.Config{}, fmt.Errorf("invalid crop fixture signature")
	}
	return image.Config{ColorModel: color.RGBAModel, Width: int(header[len(cropFixtureMagic)]), Height: int(header[len(cropFixtureMagic)+1])}, nil
}

func decodeCropFixture(r io.Reader) (image.Image, error) {
	cfg, err := cropFixtureConfig(r)
	if err != nil {
		return nil, err
	}
	pixels := make([]byte, cfg.Width*cfg.Height*3)
	if _, err := io.ReadFull(r, pixels); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			i := (y*cfg.Width + x) * 3
			img.SetRGBA(x, y, color.RGBA{R: pixels[i], G: pixels[i+1], B: pixels[i+2], A: 255})
		}
	}
	return cropFixtureImage{Image: img}, nil
}

func init() {
	image.RegisterFormat("javinizer-crop-rgb-v1", cropFixtureMagic, decodeCropFixture, cropFixtureConfig)
}

func cropFixtureBytes(width, height int) []byte {
	data := append([]byte(cropFixtureMagic), byte(width), byte(height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Independent horizontal and vertical ramps make either a wrong
			// source column or row observable after cropping.
			data = append(data, byte(12*x), byte(18*y), 90)
		}
	}
	return data
}

func TestCropRegisteredImageWithoutSubImagePreservesPixelsAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		top           int
		crop          func(afero.Fs, string, string, []byte) (os.FileInfo, error)
	}{
		{"path automatic", 8, 12, 0, func(fs afero.Fs, source, target string, _ []byte) (os.FileInfo, error) {
			return imageutil.CropPosterFromCover(fs, source, target, 0)
		}},
		{"bound reader resized", 4, 6, 0, func(fs afero.Fs, _, target string, data []byte) (os.FileInfo, error) {
			return imageutil.CropPosterFromCoverReader(fs, bytes.NewReader(data), target, 6)
		}},
		{"bound reader explicit rectangle", 8, 8, 2, func(fs afero.Fs, _, target string, data []byte) (os.FileInfo, error) {
			return imageutil.CropPosterWithBoundsReader(fs, bytes.NewReader(data), target, 8, 2, 16, 10, 0)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "source.rgb"), filepath.Join(root, "poster.jpg")
			encoded := cropFixtureBytes(16, 12)
			require.NoError(t, os.WriteFile(source, encoded, 0600))
			written, err := tc.crop(afero.NewOsFs(), source, target, encoded)
			require.NoError(t, err)
			require.NotNil(t, written)
			actual, err := os.Stat(target)
			require.NoError(t, err)
			require.True(t, os.SameFile(written, actual), "returned producer identity must name the written output")
			output, err := os.ReadFile(target)
			require.NoError(t, err)
			decoded, err := jpeg.Decode(bytes.NewReader(output))
			require.NoError(t, err)
			require.Equal(t, image.Rect(0, 0, tc.width, tc.height), decoded.Bounds())
			// Build an independent expected raster from known source coordinates,
			// not by cropping/decoding the fixture or calling production helpers.
			// Encode with the same public JPEG quality contract so lossy chroma
			// edges do not require a tolerance broad enough to hide offset bugs.
			referenceHeight := 12
			if tc.top != 0 {
				referenceHeight = 8
			}
			reference := image.NewRGBA(image.Rect(0, 0, 8, referenceHeight))
			for y := 0; y < referenceHeight; y++ {
				for x := 0; x < 8; x++ {
					reference.SetRGBA(x, y, color.RGBA{R: uint8((x + 8) * 12), G: uint8((y + tc.top) * 18), B: 90, A: 255})
				}
			}
			if tc.width == 4 {
				resized := image.NewRGBA(image.Rect(0, 0, 4, 6))
				draw.BiLinear.Scale(resized, resized.Bounds(), reference, reference.Bounds(), draw.Over, nil)
				reference = resized
			}
			var expectedJPEG bytes.Buffer
			require.NoError(t, jpeg.Encode(&expectedJPEG, reference, &jpeg.Options{Quality: 95}))
			require.Equal(t, expectedJPEG.Bytes(), output, "crop must preserve exact horizontal/vertical source mapping before JPEG encoding")
			retained, err := os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, encoded, retained)
		})
	}
}

func TestCropRegisteredEmptyRasterRejectsBeforeOutputMutation(t *testing.T) {
	for _, size := range []image.Point{{X: 0, Y: 3}, {X: 4, Y: 0}} {
		t.Run(fmt.Sprintf("%dx%d", size.X, size.Y), func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "empty.rgb"), filepath.Join(root, "existing.jpg")
			encoded := cropFixtureBytes(size.X, size.Y)
			require.NoError(t, os.WriteFile(source, encoded, 0600))
			sentinel := []byte("preexisting poster must not be truncated")
			require.NoError(t, os.WriteFile(target, sentinel, 0600))
			before, err := os.Stat(target)
			require.NoError(t, err)
			written, err := imageutil.CropPosterFromCover(afero.NewOsFs(), source, target, 0)
			require.ErrorContains(t, err, fmt.Sprintf("invalid image dimensions: %dx%d", size.X, size.Y))
			require.Nil(t, written)
			after, err := os.Stat(target)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			retained, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, sentinel, retained)
			retained, err = os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, encoded, retained)
		})
	}
}
