package imagevalidate

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

const MaxEncodedBytes = 10 << 20

func validate(data []byte) (image.Config, string, error) {
	return Validate(context.Background(), bytes.NewReader(data), Limits{MaxEncodedBytes, 8192, 32_000_000})
}

func TestValidate(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.White)
	for _, format := range []string{"png", "jpeg"} {
		var buf bytes.Buffer
		var err error
		if format == "png" {
			err = png.Encode(&buf, img)
		} else {
			err = jpeg.Encode(&buf, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		config, got, err := validate(buf.Bytes())
		if err != nil || got != format || config.Width != 2 || config.Height != 3 {
			t.Fatalf("%s: %v %s %v", format, config, got, err)
		}
		if _, _, err := validate(buf.Bytes()[:len(buf.Bytes())/2]); err == nil {
			t.Fatalf("accepted truncated %s", format)
		}
	}
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validate(buf.Bytes()); err == nil {
		t.Fatal("accepted GIF")
	}
	for _, data := range [][]byte{nil, []byte("not an image"), make([]byte, MaxEncodedBytes+1)} {
		if _, _, err := validate(data); err == nil {
			t.Fatalf("accepted invalid image of %d bytes", len(data))
		}
	}
}

func TestRejectDimensionsBeforeDecode(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, dims := range [][2]uint32{{8193, 1}, {1, 8193}, {8192, 8192}, {4000, 8001}, {0xffffffff, 0xffffffff}} {
		data := bytes.Clone(buf.Bytes())
		binary.BigEndian.PutUint32(data[16:20], dims[0])
		binary.BigEndian.PutUint32(data[20:24], dims[1])
		binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
		_, _, err := validate(data)
		if err == nil || !strings.Contains(err.Error(), "dimensions exceed limits") && !strings.Contains(err.Error(), "configuration") {
			t.Fatalf("%v: %v", dims, err)
		}
	}
}

func TestExplicitLimitsAndCancellation(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	for _, tc := range []struct {
		limits Limits
		ok     bool
	}{
		{Limits{int64(len(data)), 3, 6}, true},
		{Limits{int64(len(data) - 1), 3, 6}, false},
		{Limits{int64(len(data)), 2, 6}, false},
		{Limits{int64(len(data)), 3, 5}, false},
		{Limits{0, 3, 6}, false}, {Limits{100, 0, 6}, false}, {Limits{100, 3, 0}, false}, {Limits{math.MaxInt64, 3, 6}, false},
	} {
		_, _, err := Validate(t.Context(), bytes.NewReader(data), tc.limits)
		if (err == nil) != tc.ok {
			t.Fatalf("limits=%+v err=%v", tc.limits, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := Validate(ctx, bytes.NewReader(data), Limits{1000, 3, 6}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	reader := readerFunc(func(p []byte) (int, error) { cancel(); return copy(p, data), io.EOF })
	if _, _, err := Validate(ctx, reader, Limits{1000, 3, 6}); !errors.Is(err, context.Canceled) {
		t.Fatalf("read cancel: %v", err)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestWebP(t *testing.T) {
	data, err := os.ReadFile("testdata/gopher.webp")
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := validate(data)
	if err != nil || format != "webp" || config.Width < 1 || config.Height < 1 {
		t.Fatalf("WebP: %v %s %v", config, format, err)
	}
	if _, _, err := validate(data[:len(data)/2]); err == nil {
		t.Fatal("accepted truncated WebP")
	}
}
