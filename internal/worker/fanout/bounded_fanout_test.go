package fanout

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedFanOut_DispatchesAllItems(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	results := BoundedFanOut(context.Background(), 3, items, func(ctx context.Context, item int) int {
		return item * 2
	})

	assert.Len(t, results, 5)
	// Results may be in any order due to concurrency
	total := 0
	for _, r := range results {
		total += r
	}
	assert.Equal(t, 30, total, "sum of doubled items should be 30")
}

func TestBoundedFanOut_RespectsMaxWorkers(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32
	items := make([]int, 20)

	BoundedFanOut(context.Background(), 3, items, func(ctx context.Context, item int) int {
		cur := atomic.AddInt32(&concurrent, 1)
		for {
			old := atomic.LoadInt32(&maxConcurrent)
			if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, cur) {
				break
			}
		}
		// Do some work to increase chance of concurrency
		for i := 0; i < 1000; i++ {
		}
		atomic.AddInt32(&concurrent, -1)
		return 0
	})

	assert.LessOrEqual(t, atomic.LoadInt32(&maxConcurrent), int32(3), "should not exceed maxWorkers")
}

func TestBoundedFanOut_PreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before dispatching

	items := []int{1, 2, 3, 4, 5}

	// Even with a pre-cancelled context, BoundedFanOut still dispatches and
	// collects all outcomes — cancellation does not stop in-flight work.
	results := BoundedFanOut(ctx, 2, items, func(ctx context.Context, item int) int {
		return item
	})

	assert.Len(t, results, 5, "all items should still be processed with pre-cancelled context")
}

func TestBoundedFanOut_EmptyItems(t *testing.T) {
	results := BoundedFanOut(context.Background(), 3, []int{}, func(ctx context.Context, item int) int {
		return item
	})

	assert.Empty(t, results)
}

func TestBoundedFanOut_WorkFuncReturnsAnyValue(t *testing.T) {
	items := []int{1, 2, 3}

	results := BoundedFanOut(context.Background(), 2, items, func(ctx context.Context, item int) int {
		return item * 10
	})

	require.Len(t, results, 3)
	total := 0
	for _, r := range results {
		total += r
	}
	assert.Equal(t, 60, total, "sum of 10+20+30 should be 60")
}
