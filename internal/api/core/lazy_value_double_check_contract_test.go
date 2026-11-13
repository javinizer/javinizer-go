package core

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLazyValueConcurrentBuildersReturnPublishedWinner(t *testing.T) {
	type value struct{ id int }
	values := [2]*value{{id: 1}, {id: 2}}
	started := make(chan int, 2)
	results := make(chan any, 2)
	release := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	var released [2]sync.Once
	releaseBuild := func(index int) {
		released[index].Do(func() { close(release[index]) })
	}
	t.Cleanup(func() {
		releaseBuild(0)
		releaseBuild(1)
	})
	var builds atomic.Int32
	lazy := newLazyValue(func() any {
		index := int(builds.Add(1)) - 1
		if index >= len(values) {
			return &value{id: index + 1}
		}
		started <- index
		<-release[index]
		return values[index]
	}, func(v any) bool { return v == nil })

	for index := range values {
		go func() { results <- lazy.Get() }()
		select {
		case actual := <-started:
			require.Equal(t, index, actual)
		case <-time.After(5 * time.Second):
			t.Fatal("both callers must observe an empty cache and enter their builders")
		}
	}

	for index := range values {
		releaseBuild(index)
		select {
		case actual := <-results:
			require.Same(t, values[0], actual, "the later builder must not replace the published winner")
		case <-time.After(5 * time.Second):
			t.Fatal("released builder did not return")
		}
	}
	require.EqualValues(t, 2, builds.Load())
	require.Same(t, values[0], lazy.Get())
	require.EqualValues(t, 2, builds.Load(), "cached access must not construct another value")
}
