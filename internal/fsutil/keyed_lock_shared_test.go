package fsutil

// #224 phase D: the shared-acquisition mode lifts the organizer's per-directory
// reader/writer discipline onto the process-wide destination registry. These
// tests pin the RW semantics, the refcount/white-box eviction discipline under
// mixed-mode churn (the registry must stay bounded), queued-writer preference
// (a directory rename must eventually win against streaming child writes), and
// key-folding parity with the exclusive path.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKeyedLock_AcquireShared_ConcurrentHoldersOverlap(t *testing.T) {
	r := NewKeyedLockRegistry()
	entered := make(chan struct{}, 2)
	releaseEnter := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel := r.AcquireShared("/shared/dir")
			entered <- struct{}{}
			<-releaseEnter
			rel()
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Until(deadline)):
			t.Fatal("second shared holder must overlap the first")
		}
	}
	close(releaseEnter)
	wg.Wait()
}

func TestKeyedLock_AcquireShared_ExclusiveExcludesBothWays(t *testing.T) {
	t.Run("exclusive waits for shared drain", func(t *testing.T) {
		r := NewKeyedLockRegistry()
		hold := r.AcquireShared("/rw/dir")
		granted := make(chan func(), 1)
		go func() { granted <- r.Acquire("/rw/dir") }()
		select {
		case rel := <-granted:
			rel()
			t.Fatal("exclusive acquisition proceeded while a shared hold was live")
		case <-time.After(50 * time.Millisecond):
		}
		hold()
		select {
		case rel := <-granted:
			rel()
		case <-time.After(2 * time.Second):
			t.Fatal("exclusive acquisition never granted after shared drain")
		}
	})
	t.Run("shared waits behind exclusive", func(t *testing.T) {
		r := NewKeyedLockRegistry()
		hold := r.Acquire("/rw/dir2")
		granted := make(chan func(), 1)
		go func() { granted <- r.AcquireShared("/rw/dir2") }()
		select {
		case rel := <-granted:
			rel()
			t.Fatal("shared acquisition proceeded while an exclusive hold was live")
		case <-time.After(50 * time.Millisecond):
		}
		hold()
		select {
		case rel := <-granted:
			rel()
		case <-time.After(2 * time.Second):
			t.Fatal("shared acquisition never granted after exclusive release")
		}
	})
}

// A queued writer must win over readers that arrive after it queued — a
// directory rename permanently starved by streaming child writes would never
// drain the directory (Go sync.RWMutex writer-preference, pinned).
func TestKeyedLock_AcquireShared_QueuedExclusiveWinsOverLateReaders(t *testing.T) {
	r := NewKeyedLockRegistry()
	hold := r.AcquireShared("/prio")
	excl := make(chan func(), 1)
	go func() { excl <- r.Acquire("/prio") }()
	time.Sleep(50 * time.Millisecond) // let the exclusive acquisition queue
	late := make(chan func(), 1)
	go func() { late <- r.AcquireShared("/prio") }()
	time.Sleep(50 * time.Millisecond)
	select {
	case rel := <-late:
		rel()
		t.Fatal("late shared hold must queue behind the already-waiting exclusive acquisition")
	default:
	}
	hold()
	select {
	case rel := <-excl:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("queued exclusive acquisition never granted")
	}
	select {
	case rel := <-late:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("late shared hold never granted after the exclusive released")
	}
}

func TestKeyedLock_AcquireShared_VacuumOnRelease(t *testing.T) {
	r := NewKeyedLockRegistry()
	r.AcquireShared("gone-shared")()
	r.mu.Lock()
	_, present := r.locks[foldKeyedLock("gone-shared")]
	r.mu.Unlock()
	require.False(t, present, "released shared keys are evicted")
}

// A queued exclusive waiter pins the entry: releasing the last live SHARED
// hold must NOT evict while the waiter is still registered, or the waiter
// would acquire a stale lock object nobody else contends on.
func TestKeyedLock_SharedReleaseKeepsEntryWhileExclusiveWaits(t *testing.T) {
	r := NewKeyedLockRegistry()
	hold := r.AcquireShared("/waiter-pin")
	done := make(chan func(), 1)
	go func() { done <- r.Acquire("/waiter-pin") }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		refs := r.locks[foldKeyedLock("/waiter-pin")].refs
		r.mu.Unlock()
		if refs == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exclusive waiter never registered on the entry")
		}
		time.Sleep(2 * time.Millisecond)
	}
	hold()
	r.mu.Lock()
	_, present := r.locks[foldKeyedLock("/waiter-pin")]
	r.mu.Unlock()
	require.True(t, present, "entry must survive while an exclusive waiter is queued on it")
	rel := <-done
	rel()
	r.mu.Lock()
	_, present = r.locks[foldKeyedLock("/waiter-pin")]
	r.mu.Unlock()
	require.False(t, present, "entry evicted once the final waiter releases")
}

func TestKeyedLock_SharedExclusiveChurn_Bounded(t *testing.T) {
	r := NewKeyedLockRegistry()
	var wg sync.WaitGroup
	const keyCount = 25
	var inFlight [keyCount]atomic.Int32
	var maxExclusive [keyCount]atomic.Int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ki := j % keyCount
				key := fmt.Sprintf("/churn-%d", ki)
				if i%2 == 0 {
					rel := r.Acquire(key)
					cur := inFlight[ki].Add(1)
					for {
						m := maxExclusive[ki].Load()
						if cur <= m || maxExclusive[ki].CompareAndSwap(m, cur) {
							break
						}
					}
					time.Sleep(time.Millisecond)
					inFlight[ki].Add(-1)
					rel()
				} else {
					rel := r.AcquireShared(key)
					rel()
				}
			}
		}(i)
	}
	wg.Wait()
	for ki := 0; ki < keyCount; ki++ {
		require.Equal(t, int32(1), maxExclusive[ki].Load(), "exclusive holders of one key must never overlap, under shared traffic too (key /churn-%d)", ki)
	}
	r.mu.Lock()
	size := len(r.locks)
	r.mu.Unlock()
	require.Equal(t, 0, size, "mixed shared/exclusive churn must leave the registry empty (bounded)")
}

// Shared acquisition folds exactly like the exclusive path: differently-spelled
// keys of one destination contend in either mode combination.
func TestKeyedLock_AcquireShared_FoldsLikeExclusive(t *testing.T) {
	previous := PathBackslashesAreSeparators
	PathBackslashesAreSeparators = true
	t.Cleanup(func() { PathBackslashesAreSeparators = previous })

	r := NewKeyedLockRegistry()
	hold := r.AcquireShared("/DST/Sub")
	granted := make(chan func(), 1)
	go func() { granted <- r.Acquire("\\dst\\sub") }()
	select {
	case rel := <-granted:
		rel()
		t.Fatal("differently-spelled key must contend with the shared hold")
	case <-time.After(50 * time.Millisecond):
	}
	hold()
	select {
	case rel := <-granted:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("folded exclusive acquisition never granted after shared release")
	}
}

// Directory tier: writer/reader discipline identical to the file tier, on
// the same registry, under namespaced keys.
func TestKeyedLock_DirTier_RWDiscipline(t *testing.T) {
	r := NewKeyedLockRegistry()
	a := r.AcquireDirShared("/tier/dir")
	b := r.AcquireDirShared("/tier/dir") // overlapping readers proceed
	excl := make(chan func(), 1)
	go func() { excl <- r.AcquireDirExclusive("/tier/dir") }()
	select {
	case rel := <-excl:
		rel()
		t.Fatal("exclusive dir hold must wait for shared dir holds to drain")
	case <-time.After(50 * time.Millisecond):
	}
	a()
	b()
	select {
	case rel := <-excl:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive dir hold never granted after shared drain")
	}

	hold := r.AcquireDirExclusive("/tier/dir2")
	sh := make(chan func(), 1)
	go func() { sh <- r.AcquireDirShared("/tier/dir2") }()
	select {
	case rel := <-sh:
		rel()
		t.Fatal("shared dir hold entered under a live exclusive dir hold")
	case <-time.After(50 * time.Millisecond):
	}
	hold()
	select {
	case rel := <-sh:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("shared dir hold never granted after exclusive release")
	}
}

// The tiers must never alias: a file-tier hold of one spelling and a
// directory-tier hold of the SAME spelling proceed concurrently — that is the
// property keeping nested ancestor-dir/descendant-file (and degenerate
// same-name) acquisitions deadlock-free without re-entrant mutexes.
func TestKeyedLock_DirTier_NeverAliasesFileTier(t *testing.T) {
	r := NewKeyedLockRegistry()
	dirHold := r.AcquireDirExclusive("/same/name")
	fileGranted := make(chan func(), 1)
	go func() { fileGranted <- r.Acquire("/same/name") }()
	select {
	case rel := <-fileGranted:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("file-tier acquisition must not contend with the directory tier")
	}
	dirHold()

	fileHold := r.Acquire("/same/name2")
	dirGranted := make(chan func(), 1)
	go func() { dirGranted <- r.AcquireDirExclusive("/same/name2") }()
	select {
	case rel := <-dirGranted:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("directory-tier acquisition must not contend with the file tier")
	}
	fileHold()
}

// ".." components can cancel LEADING segments under filepath.Clean; the tier
// marker is a suffix, so cleaning never strips it: dotdot spellings of one
// directory still contend.
func TestKeyedLock_DirTier_DotDotSpellingContends(t *testing.T) {
	r := NewKeyedLockRegistry()
	hold := r.AcquireDirExclusive("/x/../tiered")
	blocked := make(chan func(), 1)
	go func() { blocked <- r.AcquireDirShared("/tiered") }()
	select {
	case rel := <-blocked:
		rel()
		t.Fatal("dotdot-cleaned spelling of one directory must contend on one tier key")
	case <-time.After(50 * time.Millisecond):
	}
	hold()
	select {
	case rel := <-blocked:
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("tier acquisition never granted after release")
	}
}

// The organizer's universal holding pattern — ancestor directory (shared or
// exclusive) then descendant file (exclusive) on DISTINCT keys — must never
// deadlock under mixed concurrency, including alongside exclusive directory
// acquisitions (the in-place rename shape).
func TestKeyedLock_NestedDirThenFileDistinctKeys_NoDeadlock(t *testing.T) {
	r := NewKeyedLockRegistry()
	const dir = "/nest"
	file := dir + "/movie.mkv"
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			relDir := r.AcquireDirShared(dir)
			relFile := r.Acquire(file)
			relFile()
			relDir()
		}()
		go func() {
			defer wg.Done()
			<-start
			relDir := r.AcquireDirExclusive(dir)
			relFile := r.Acquire(file)
			relFile()
			relDir()
		}()
	}
	close(start)
	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(10 * time.Second):
		t.Fatal("nested dir-then-file acquisitions deadlocked")
	}
	r.mu.Lock()
	size := len(r.locks)
	r.mu.Unlock()
	require.Equal(t, 0, size)
}
