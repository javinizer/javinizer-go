package actresscache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Saturated decode slots must backpressure instead of OOMing the builder; a
// freed slot lets the pending decode through.
func TestThumbnailDecodeBackpressure(t *testing.T) {
	for i := 0; i < cap(thumbnailDecodeSlots); i++ {
		thumbnailDecodeSlots <- struct{}{}
	}
	image := makeJPEG(t, 32, 32)
	done := make(chan error, 1)
	go func() {
		_, err := ValidateThumbnail(context.Background(), testFetcher(200, "image/jpeg", image, nil), "https://cdn.test/ok.jpg", 1, 1<<20)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("decode completed despite saturated slots: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	<-thumbnailDecodeSlots // free exactly one slot
	require.NoError(t, <-done)
	// Restore the empty pool for other tests.
	for i := 0; i < cap(thumbnailDecodeSlots)-1; i++ {
		<-thumbnailDecodeSlots
	}

	// Cancellation while waiting for a slot surfaces the context error.
	for i := 0; i < cap(thumbnailDecodeSlots); i++ {
		thumbnailDecodeSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(thumbnailDecodeSlots); i++ {
			<-thumbnailDecodeSlots
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ValidateThumbnail(ctx, testFetcher(200, "image/jpeg", image, nil), "https://cdn.test/ok.jpg", 1, 1<<20)
	require.ErrorIs(t, err, context.Canceled)
}
