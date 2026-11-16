package metadatafetch

import (
	"context"
	"crypto/sha256"
	"net/url"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

// This observes the second cancellation check without fabricating Err results.
// The real context is canceled only after hashing has completed, and its actual
// Err and Done methods retain the standard context contract.
type finalizationBarrierContext struct {
	context.Context
	calls    atomic.Int32
	postHash chan struct{}
	resume   chan struct{}
}

func (c *finalizationBarrierContext) Err() error {
	if c.calls.Add(1) == 2 {
		close(c.postHash)
		<-c.resume
	}
	return c.Context.Err()
}

func TestFinalizeResultCancellationAfterHashDiscardsCompletePayload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, cancel := context.WithCancel(t.Context())
		defer cancel()
		ctx := &finalizationBarrierContext{Context: parent, postHash: make(chan struct{}), resume: make(chan struct{})}
		defer close(ctx.resume)
		data := []byte("complete metadata response")
		original := append([]byte(nil), data...)
		target, err := url.Parse("https://example.com/final?token=private")
		require.NoError(t, err)
		type outcome struct {
			result Result
			err    error
		}
		done := make(chan outcome, 1)
		go func() { result, err := finalizeResult(ctx, data, target); done <- outcome{result, err} }()
		synctest.Wait()
		select {
		case <-ctx.postHash:
		default:
			t.Fatal("finalization must observe cancellation after hashing")
		}
		require.NoError(t, parent.Err(), "the pre-hash check saw a live context")
		select {
		case <-done:
			t.Fatal("result escaped before the post-hash check")
		default:
		}
		cancel()
		require.ErrorIs(t, parent.Err(), context.Canceled)
		select {
		case <-parent.Done():
		default:
			t.Fatal("real context Done must be closed")
		}
		ctx.resume <- struct{}{}
		got := <-done
		require.ErrorIs(t, got.err, context.Canceled)
		boundaryZeroResult(t, got.result)
		require.Equal(t, original, data, "cancellation must not modify the owned response bytes")
		require.Equal(t, int32(2), ctx.calls.Load())

		// A later independent request can finalize the same valid payload normally.
		recovered, err := finalizeResult(t.Context(), data, target)
		require.NoError(t, err)
		require.Equal(t, data, recovered.Bytes)
		require.Equal(t, target.String(), recovered.FinalURL)
		require.Equal(t, sha256.Sum256(data), recovered.SHA256)
	})
}

func TestFinalizeResultAlreadyCanceledReturnsNoPartialResult(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := finalizeResult(ctx, []byte("payload"), &url.URL{Scheme: "https", Host: "example.com"})
	require.ErrorIs(t, err, context.Canceled)
	boundaryZeroResult(t, result)
}
