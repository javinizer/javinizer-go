package actressmetadata

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRegistryCancellationAfterSuccessfulWaitDoesNotCallProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p := &stub{}
	clock := &fakeClock{now: time.Unix(1, 0), wait: func(context.Context) error { cancel(); return nil }}
	r, err := NewRegistry([]Registration{{Name: "fixture", Provider: p, Enabled: true, MinInterval: time.Second}}, clock, time.Minute)
	require.NoError(t, err)
	_, err = r.Resolve(t.Context(), ref("fixture_a", "actor-1"), []string{"fixture"})
	require.NoError(t, err)
	result, err := r.Resolve(ctx, ref("fixture_a", "actor-1"), []string{"fixture"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
	require.Equal(t, 1, p.calls)
	require.Equal(t, []time.Duration{time.Second}, clock.waits)
	// Cancellation releases admission; a later caller can still resolve.
	clock.wait = nil
	result, err = r.Resolve(t.Context(), ref("fixture_a", "actor-1"), []string{"fixture"})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, p.calls)
}
func TestRegistryCancellationAfterProviderDiscardsSuccessAndStopsPriorityChain(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := &stub{fn: func(context.Context, Query) (Result, error) { cancel(); return Result{Outcome: NoSnapshot}, nil }}
	second := &stub{}
	r := registry(t, Registration{Name: "first", Provider: first, Enabled: true}, Registration{Name: "second", Provider: second, Enabled: true})
	result, err := r.Resolve(ctx, ref("fixture_a", "actor-1"), []string{"first", "second"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
	require.Equal(t, 1, first.calls)
	require.Zero(t, second.calls)
	first.fn = nil
	result, err = r.Resolve(t.Context(), ref("fixture_a", "actor-1"), []string{"first", "second"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
}
