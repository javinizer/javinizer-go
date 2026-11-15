package actressmetadata

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/require"
)

// metadataBoundaryContext retains a real context's values, deadlines and
// cancellation semantics. A test may pause the final snapshot Err observation
// or the first Done observation while the registry links its bounded request.
// The test cancels the underlying context before releasing that observation.
type metadataBoundaryContext struct {
	context.Context
	pauseOnLink bool
	errCalls    atomic.Int32
	doneCalls   atomic.Int32
	reached     chan struct{}
	release     chan struct{}
}

func (ctx *metadataBoundaryContext) Err() error {
	if !ctx.pauseOnLink && ctx.errCalls.Add(1) == 2 {
		close(ctx.reached)
		<-ctx.release
	}
	return ctx.Context.Err()
}

func (ctx *metadataBoundaryContext) Done() <-chan struct{} {
	if ctx.pauseOnLink && ctx.doneCalls.Add(1) == 1 {
		close(ctx.reached)
		<-ctx.release
	}
	return ctx.Context.Done()
}

func cancelAtMetadataBoundary(t *testing.T, pauseOnLink bool, call func(context.Context)) {
	t.Helper()
	parent, cancel := context.WithCancel(t.Context())
	ctx := &metadataBoundaryContext{Context: parent, pauseOnLink: pauseOnLink, reached: make(chan struct{}), release: make(chan struct{})}
	finished := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(ctx.release) }) }
	go func() {
		defer close(finished)
		call(ctx)
	}()
	defer func() {
		cancel()
		release()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("metadata call did not join after cleanup")
		}
	}()
	select {
	case <-ctx.reached:
	case <-finished:
		t.Fatal("metadata call returned before the requested cancellation boundary")
	case <-time.After(5 * time.Second):
		t.Fatal("metadata call did not reach the cancellation boundary")
	}
	cancel()
	release()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("metadata call did not return after cancellation")
	}
	require.ErrorIs(t, parent.Err(), context.Canceled)
}

func TestSnapshotCancellationAfterLookupDiscardsEvidence(t *testing.T) {
	// Keep one real, validated evidence record so both reference and claim
	// lookups have an unambiguous positive control.
	snapshot, err := actresscache.Build(t.Context(), fixture(t).Records()[:1], limits())
	require.NoError(t, err)
	p := provider(t, snapshot, "fixture_a")
	before := snapshot.Bytes()
	for _, tc := range []struct {
		name string
		q    Query
		want Outcome
	}{
		{"matching_reference", ref("fixture_a", "actor-1"), Available},
		{"missing_reference", ref("fixture_a", "missing"), Unmatched},
		{"matching_claim", claim("7"), Available},
		{"missing_claim", claim("99"), Unmatched},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalQuery := tc.q.clone()
			baseline, baselineErr := p.LookupEvidence(t.Context(), tc.q)
			require.NoError(t, baselineErr)
			require.Equal(t, tc.want, baseline.Outcome)
			require.Equal(t, snapshot.Digest(), baseline.SnapshotDigest)
			if tc.want == Available {
				require.NotNil(t, baseline.Evidence)
			}

			var got Result
			var gotErr error
			cancelAtMetadataBoundary(t, false, func(ctx context.Context) {
				got, gotErr = p.LookupEvidence(ctx, tc.q)
			})
			require.ErrorIs(t, gotErr, context.Canceled)
			require.Equal(t, Result{}, got, "no outcome, digest or evidence may escape a cancelled lookup")
			require.Equal(t, originalQuery, tc.q)
			require.Equal(t, before, snapshot.Bytes())

			retried, retryErr := p.LookupEvidence(t.Context(), tc.q)
			require.NoError(t, retryErr)
			require.Equal(t, baseline, retried, "cancellation must not poison the pinned snapshot")
		})
	}
}

type metadataAdmissionClock struct {
	fakeClock
	nowCalls int
}

func (c *metadataAdmissionClock) Now() time.Time {
	c.nowCalls++
	return c.fakeClock.Now()
}

func (c *metadataAdmissionClock) Wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.fakeClock.Wait(ctx, delay)
}

func TestRegistryCancellationDuringLinkingPreservesAdmissionAndSchedule(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		name := "unused_schedule"
		if scheduled {
			name = "existing_schedule"
		}
		t.Run(name, func(t *testing.T) {
			clock := &metadataAdmissionClock{fakeClock: fakeClock{now: time.Unix(1, 0)}}
			disabled, first, second := &stub{}, &stub{}, &stub{}
			r, err := NewRegistry([]Registration{
				{Name: "disabled", Provider: disabled},
				{Name: "first", Provider: first, Enabled: true, MinInterval: time.Second},
				{Name: "second", Provider: second, Enabled: true, MinInterval: time.Second},
			}, clock, time.Minute)
			require.NoError(t, err)
			priorities := []string{"disabled", "first", "second"}
			q := ref("fixture_a", "actor-1")
			if scheduled {
				_, err = r.Resolve(t.Context(), q, priorities)
				require.NoError(t, err)
			}
			firstBefore, secondBefore := r.entries["first"].next, r.entries["second"].next
			first.calls, second.calls, clock.nowCalls = 0, 0, 0
			clock.waits = nil

			var got *Resolution
			var gotErr error
			cancelAtMetadataBoundary(t, true, func(ctx context.Context) {
				got, gotErr = r.Resolve(ctx, q, priorities)
			})
			require.ErrorIs(t, gotErr, context.Canceled)
			require.Nil(t, got)
			require.Zero(t, disabled.calls)
			require.Zero(t, first.calls)
			require.Zero(t, second.calls)
			require.Zero(t, clock.nowCalls, "a cancelled linked request must stop before consulting the throttle clock")
			require.Empty(t, clock.waits)
			require.Equal(t, firstBefore, r.entries["first"].next)
			require.Equal(t, secondBefore, r.entries["second"].next)

			// A subsequent valid caller proves that cancellation released the
			// registry's bounded admission and retained normal priority order.
			retried, retryErr := r.Resolve(t.Context(), q, priorities)
			require.NoError(t, retryErr)
			require.Equal(t, Unmatched, retried.Outcome)
			require.Len(t, retried.Observations, 2)
			require.Equal(t, "first", retried.Observations[0].Provider)
			require.Equal(t, "second", retried.Observations[1].Provider)
			require.Zero(t, disabled.calls)
			require.Equal(t, 1, first.calls)
			require.Equal(t, 1, second.calls)
		})
	}
}
