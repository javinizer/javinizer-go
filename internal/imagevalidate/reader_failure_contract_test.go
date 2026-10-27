package imagevalidate

import (
	"context"
	"errors"
	"image"
	"testing"
)

func TestValidateCancellationBetweenReadsStopsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads := 0
	input := readerFunc(func(p []byte) (int, error) {
		reads++
		cancel()
		// A successful partial read forces ReadAll to ask for more, unlike an
		// EOF return that would only test cancellation after all reads finish.
		return copy(p, []byte{0x89}), nil
	})
	config, format, err := Validate(ctx, input, Limits{1000, 3, 6})
	if !errors.Is(err, context.Canceled) || reads != 1 || config != (image.Config{}) || format != "" {
		t.Fatalf("config=%+v format=%q err=%v reads=%d", config, format, err, reads)
	}
}

func TestValidateReadFailurePreservesCauseWithoutPartialConfiguration(t *testing.T) {
	cause := errors.New("input reader failed")
	reads := 0
	input := readerFunc(func(p []byte) (int, error) {
		reads++
		return copy(p, []byte("\x89PNG\r\n\x1a\n")), cause
	})
	config, format, err := Validate(t.Context(), input, Limits{1000, 3, 6})
	if !errors.Is(err, cause) || reads != 1 || config != (image.Config{}) || format != "" {
		t.Fatalf("config=%+v format=%q err=%v reads=%d", config, format, err, reads)
	}
}
