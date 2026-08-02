package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAcquireJobEnvelopeLock_SerializesPerJobAndEvicts pins the three
// contract points of the job-scope envelope lock (the bulk-rescrape
// commit→persist→rollback serializer): the same jobID blocks, a different
// jobID does not, and the map entry is evicted once the last holder releases
// (no unbounded growth).
func TestAcquireJobEnvelopeLock_SerializesPerJobAndEvicts(t *testing.T) {
	const jobID = "job-envelope-lock"

	first := AcquireJobEnvelopeLock(jobID)

	second := make(chan func(), 1)
	go func() { second <- AcquireJobEnvelopeLock(jobID) }()

	select {
	case release := <-second:
		release()
		t.Fatal("second acquire on the same jobID must block while the first holder holds")
	case <-time.After(50 * time.Millisecond):
	}

	// A different job never contends: per-job granularity.
	otherJob := AcquireJobEnvelopeLock("job-envelope-lock-other")
	otherJob()

	first()

	select {
	case release := <-second:
		release()
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire must complete once the first holder releases")
	}

	// The entry is evicted when the last holder releases.
	deadline := time.Now().Add(2 * time.Second)
	for {
		jobEnvelopeLockGuard.Lock()
		_, exists := jobEnvelopeLockEntries.Load(jobID)
		jobEnvelopeLockGuard.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job envelope lock entry was not evicted after the last release")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestAcquireJobEnvelopeLock_ReacquireAfterEviction keeps the post-eviction
// re-registration path at 100%: releasing the last holder drops the entry,
// and a fresh acquire builds a new one that still serializes.
func TestAcquireJobEnvelopeLock_ReacquireAfterEviction(t *testing.T) {
	const jobID = "job-envelope-relock"

	AcquireJobEnvelopeLock(jobID)()

	again := AcquireJobEnvelopeLock(jobID)
	blocked := make(chan func(), 1)
	go func() { blocked <- AcquireJobEnvelopeLock(jobID) }()

	select {
	case release := <-blocked:
		release()
		t.Fatal("a re-registered entry must still serialize per jobID")
	case <-time.After(50 * time.Millisecond):
	}
	again()

	select {
	case release := <-blocked:
		release()
	case <-time.After(2 * time.Second):
		t.Fatal("the contender must acquire once the holder releases")
	}

	assert.NotPanics(t, func() {
		AcquireJobEnvelopeLock("job-envelope-relock-final")()
	}, "an unrelated jobID is independent")
}
