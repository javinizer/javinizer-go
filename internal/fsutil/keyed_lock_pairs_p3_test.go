package fsutil

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKeyedLock_AcquirePair_DedupAndOrder(t *testing.T) {
	r := NewKeyedLockRegistry()
	rel := r.AcquirePair("/b", "/a")
	// second acquisition of either key blocks
	done := make(chan struct{})
	go func() { r.Acquire("/a")(); close(done) }()
	select {
	case <-done:
		t.Fatal("key /a must be held")
	case <-time.After(30 * time.Millisecond):
	}
	rel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked acquire never released")
	}

	// same key twice = one held entry
	rel2 := r.AcquirePair("/x", "x")
	rel2()

	// empty + whitespace keys → no-op release
	rel3 := r.AcquirePair("", " ")
	rel3()

	var hits atomic.Int32
	many := r.AcquireMany([]string{"/c", "/b", "/a", "/a"})
	go func() {
		r.Acquire("/b")()
		hits.Add(1)
	}()
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, int32(0), hits.Load(), "held by Many")
	many()
	deadline := time.Now().Add(time.Second)
	for hits.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	require.Equal(t, int32(1), hits.Load())
}

func TestKeyedLock_VacuumOnRelease(t *testing.T) {
	r := NewKeyedLockRegistry()
	r.Acquire("gone")()
	r.mu.Lock()
	_, present := r.locks[foldKeyedLock("gone")]
	r.mu.Unlock()
	require.False(t, present, "released keys are evicted")
}
