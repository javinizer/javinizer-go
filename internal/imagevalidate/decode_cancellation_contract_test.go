package imagevalidate

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// decodeBoundaryContext delegates all cancellation semantics to a real context.
// It pauses one Err observation after input EOF so the test can cancel at a
// decoding boundary without sleeps, large images, or replacing a codec.
// Observations after EOF are: after reading, before decoding, after decoding.
type decodeBoundaryContext struct {
	context.Context
	inputComplete atomic.Bool
	observations  atomic.Int32
	pauseAt       int32
	reached       chan struct{}
	release       chan struct{}
}

func (ctx *decodeBoundaryContext) Err() error {
	if ctx.inputComplete.Load() && ctx.observations.Add(1) == ctx.pauseAt {
		close(ctx.reached)
		<-ctx.release
	}
	return ctx.Context.Err()
}

func TestValidateCancellationAtDecodeBoundaries(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	var pngData, jpegData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegData, img, nil); err != nil {
		t.Fatal(err)
	}
	webpData, err := os.ReadFile("testdata/gopher.webp")
	if err != nil {
		t.Fatal(err)
	}

	fixtures := []struct {
		name      string
		data      []byte
		decodeErr bool
	}{
		{name: "png", data: pngData.Bytes()},
		{name: "jpeg", data: jpegData.Bytes()},
		{name: "webp", data: webpData},
		// The complete header remains valid, but removing IEND makes full
		// decoding fail. Cancellation must take precedence over that error.
		{name: "truncated_png", data: pngData.Bytes()[:pngData.Len()-12], decodeErr: true},
	}
	limits := Limits{MaxEncodedBytes: MaxEncodedBytes, MaxDimension: 8192, MaxPixels: 32_000_000}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			original := bytes.Clone(fixture.data)
			baseline, format, baselineErr := Validate(t.Context(), bytes.NewReader(fixture.data), limits)
			if fixture.decodeErr {
				if baselineErr == nil || !strings.Contains(baselineErr.Error(), "decode image:") {
					t.Fatalf("fixture must pass probing and fail full decoding: %v", baselineErr)
				}
			} else if baselineErr != nil || baseline.Width < 1 || baseline.Height < 1 || format != fixture.name {
				t.Fatalf("valid fixture: configuration=%v format=%q error=%v", baseline, format, baselineErr)
			}

			for _, boundary := range []struct {
				name    string
				pauseAt int32
			}{{"before_decode", 2}, {"after_decode", 3}} {
				t.Run(boundary.name, func(t *testing.T) {
					parent, cancel := context.WithCancel(t.Context())
					defer cancel()
					ctx := &decodeBoundaryContext{
						Context: parent, pauseAt: boundary.pauseAt,
						reached: make(chan struct{}), release: make(chan struct{}),
					}
					reader := bytes.NewReader(fixture.data)
					input := readerFunc(func(p []byte) (int, error) {
						n, readErr := reader.Read(p)
						if readErr == io.EOF {
							ctx.inputComplete.Store(true)
						}
						return n, readErr
					})
					type outcome struct {
						config image.Config
						format string
						err    error
					}
					result := make(chan outcome, 1)
					go func() {
						cfg, gotFormat, gotErr := Validate(ctx, input, limits)
						result <- outcome{cfg, gotFormat, gotErr}
					}()

					// Always release the worker, including when a regression makes
					// Validate return before reaching the requested observation.
					var releaseOnce sync.Once
					release := func() { releaseOnce.Do(func() { close(ctx.release) }) }
					defer release()
					select {
					case <-ctx.reached:
					case got := <-result:
						t.Fatalf("validation returned before %s: %+v", boundary.name, got)
					case <-time.After(5 * time.Second):
						t.Fatalf("validation did not reach %s", boundary.name)
					}
					cancel()
					release()
					select {
					case got := <-result:
						if !errors.Is(got.err, context.Canceled) {
							t.Fatalf("cancellation must win over a decoded result or codec error: %v", got.err)
						}
						if got.config != (image.Config{}) || got.format != "" {
							t.Fatalf("cancelled validation leaked partial metadata: %+v", got)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("cancelled validation did not return")
					}

					// A cancelled attempt must not mutate the encoded input or
					// poison a later independent validation of the same bytes.
					retried, retriedFormat, retriedErr := Validate(t.Context(), bytes.NewReader(fixture.data), limits)
					if !bytes.Equal(fixture.data, original) {
						t.Fatal("validation mutated the encoded input")
					}
					if retried.Width != baseline.Width || retried.Height != baseline.Height || retriedFormat != format || (retriedErr == nil) != (baselineErr == nil) {
						t.Fatalf("retry changed result: config=%v format=%q error=%v", retried, retriedFormat, retriedErr)
					}
				})
			}
		})
	}
}
