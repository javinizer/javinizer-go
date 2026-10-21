package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func fallbackSource(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
			if x < w/3 {
				img.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
			if x >= 2*w/3 {
				img.Set(x, y, color.RGBA{0, 0, 255, 255})
			}
		}
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func TestPosterFallbackDimensions(t *testing.T) {
	for _, mode := range []string{"center", "fit"} {
		for _, size := range []struct {
			name string
			w, h int
		}{{"landscape", 192, 108}, {"square", 100, 100}, {"portrait", 60, 90}, {"tall", 20, 100}, {"odd", 101, 55}, {"tiny", 1, 1}, {"narrow", 1, 99}, {"short", 99, 1}} {
			for _, cap := range []int{0, 3, 31, 1000} {
				t.Run(mode+"/"+size.name+"/"+strconv.Itoa(cap), func(t *testing.T) {
					fs := afero.NewMemMapFs()
					raw := fallbackSource(t, size.w, size.h)
					require.NoError(t, afero.WriteFile(fs, "/source.png", raw, 0600))
					info, err := RenderPosterFromCover(fs, "/source.png", "/path.jpg", cap, mode)
					require.NoError(t, err)
					require.NotNil(t, info)
					info, err = RenderPosterFromCoverReader(fs, bytes.NewReader(raw), "/reader.jpg", cap, mode)
					require.NoError(t, err)
					require.NotNil(t, info)
					w, h := decodeTestImageDimensions(t, fs, "/path.jpg")
					require.Equal(t, 3*w, 2*h)
					require.GreaterOrEqual(t, w, 2)
					if cap > 0 {
						require.LessOrEqual(t, h, cap)
					}
					a, err := afero.ReadFile(fs, "/path.jpg")
					require.NoError(t, err)
					b, err := afero.ReadFile(fs, "/reader.jpg")
					require.NoError(t, err)
					require.Equal(t, a, b)
				})
			}
		}
	}
}

func TestPosterFallbackPixels(t *testing.T) {
	fs := afero.NewMemMapFs()
	raw := fallbackSource(t, 300, 150)
	for _, mode := range []string{"center", "fit"} {
		_, err := RenderPosterFromCoverReader(fs, bytes.NewReader(raw), "/out.jpg", 0, mode)
		require.NoError(t, err)
		f, err := fs.Open("/out.jpg")
		require.NoError(t, err)
		img, _, err := image.Decode(f)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		r, g, b, _ := img.At(w/2, h/2).RGBA()
		require.Greater(t, int(r>>8), 240)
		require.Greater(t, int(g>>8), 240)
		require.Greater(t, int(b>>8), 240)
		if mode == "fit" {
			require.Equal(t, 300, w)
			require.Equal(t, 450, h)
			r, g, b, _ = img.At(w/2, 10).RGBA()
			require.InDelta(t, 32, r>>8, 2)
			require.InDelta(t, 32, g>>8, 2)
			require.InDelta(t, 32, b>>8, 2)
			r, g, b, _ = img.At(10, h/2).RGBA()
			require.Greater(t, int(r>>8), 230)
			require.Less(t, int(g>>8), 20)
			require.Less(t, int(b>>8), 20)
			r, g, b, _ = img.At(w-10, h/2).RGBA()
			require.Less(t, int(r>>8), 20)
			require.Less(t, int(g>>8), 20)
			require.Greater(t, int(b>>8), 230)
		} else {
			require.Equal(t, 100, w)
			require.Equal(t, 150, h)
		}
	}
}

func TestPosterFallbackLegacyUnchanged(t *testing.T) {
	fs := afero.NewMemMapFs()
	raw := fallbackSource(t, 300, 150)
	require.NoError(t, afero.WriteFile(fs, "/source.png", raw, 0600))
	_, err := CropPosterFromCover(fs, "/source.png", "/expected.jpg", 0)
	require.NoError(t, err)
	expected, err := afero.ReadFile(fs, "/expected.jpg")
	require.NoError(t, err)
	for _, mode := range []string{"", "legacy"} {
		_, err = RenderPosterFromCover(fs, "/source.png", "/actual.jpg", 0, mode)
		require.NoError(t, err)
		actual, err := afero.ReadFile(fs, "/actual.jpg")
		require.NoError(t, err)
		require.Equal(t, expected, actual)
		_, err = RenderPosterFromCoverReader(fs, bytes.NewReader(raw), "/reader.jpg", 0, mode)
		require.NoError(t, err)
		actual, err = afero.ReadFile(fs, "/reader.jpg")
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

func TestPosterFallbackErrors(t *testing.T) {
	fs := afero.NewMemMapFs()
	_, err := RenderPosterFromCover(fs, "/missing", "/out.jpg", 0, "fit")
	require.Error(t, err)
	_, err = RenderPosterFromCoverReader(fs, bytes.NewReader([]byte("bad")), "/out.jpg", 0, "fit")
	require.Error(t, err)
	for _, tc := range []struct {
		mode string
		cap  int
	}{{"invalid", 0}, {"center", -1}, {"center", 1}, {"fit", 2}} {
		_, err = RenderPosterFromCoverReader(fs, bytes.NewReader(fallbackSource(t, 10, 10)), "/out.jpg", tc.cap, tc.mode)
		require.Error(t, err)
	}
	_, err = renderPosterFallback(fs, image.NewRGBA(image.Rect(0, 0, 0, 0)), "/out.jpg", 0, "fit")
	require.Error(t, err)
	_, err = RenderPosterFromCoverReader(afero.NewReadOnlyFs(fs), bytes.NewReader(fallbackSource(t, 10, 10)), "/out.jpg", 0, "fit")
	require.Error(t, err)
	exists, err := afero.Exists(fs, "/out.jpg")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestPosterFallbackNonzeroOrigin(t *testing.T) {
	fs := afero.NewMemMapFs()
	img := image.NewRGBA(image.Rect(10, 20, 110, 170))
	for y := 20; y < 170; y++ {
		for x := 10; x < 110; x++ {
			img.Set(x, y, color.White)
		}
	}
	for _, mode := range []string{"center", "fit"} {
		_, err := renderPosterFallback(fs, img, "/out.jpg", 0, mode)
		require.NoError(t, err)
		w, h := decodeTestImageDimensions(t, fs, "/out.jpg")
		require.Equal(t, 100, w)
		require.Equal(t, 150, h)
	}
}

func TestPosterFallbackAllocationBound(t *testing.T) {
	fs := afero.NewMemMapFs()
	raw := fallbackSource(t, 20000, 1)
	_, err := RenderPosterFromCoverReader(fs, bytes.NewReader(raw), "/out.jpg", 0, "fit")
	require.ErrorContains(t, err, "exceeds 16 Mi pixels")
	exists, err := afero.Exists(fs, "/out.jpg")
	require.NoError(t, err)
	require.False(t, exists)
	_, err = RenderPosterFromCoverReader(fs, bytes.NewReader(raw), "/out.jpg", 300, "fit")
	require.NoError(t, err)
	w, h := decodeTestImageDimensions(t, fs, "/out.jpg")
	require.Equal(t, 200, w)
	require.Equal(t, 300, h)
}
