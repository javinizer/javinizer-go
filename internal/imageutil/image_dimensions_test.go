package imageutil

import (
	"image"
	"image/jpeg"
	"testing"

	"github.com/spf13/afero"
)

func TestImageDimensions(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()

	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	f, err := fs.Create("/ok.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	t.Run("reads header-only dimensions", func(t *testing.T) {
		w, h, err := ImageDimensions(fs, "/ok.jpg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w != 640 || h != 480 {
			t.Fatalf("dims = %dx%d, want 640x480", w, h)
		}
	})

	t.Run("missing file returns open error", func(t *testing.T) {
		if _, _, err := ImageDimensions(fs, "/nope.jpg"); err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("undecodable content returns decode error", func(t *testing.T) {
		if err := afero.WriteFile(fs, "/garbage.jpg", []byte("not an image"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ImageDimensions(fs, "/garbage.jpg"); err == nil {
			t.Fatal("expected decode error for garbage bytes")
		}
	})
}
