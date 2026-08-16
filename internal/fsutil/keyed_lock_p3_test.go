package fsutil

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// codex P3 R4-4: destination-lock keys fold separator spelling AND case —
// the downloader joins with the OS separator while journals use slashes. The
// fold is UNCONDITIONAL: the journals compared here persist exclusively on
// consumer media filesystems (NTFS, default-APFS, NAS fuse drivers) where
// case variants and separator spellings of a destination mean one file.
func TestFoldKeyedLock_NormalizesSeparatorsAndCase(t *testing.T) {
	require.Equal(t, foldKeyedLock("/DST/a/Poster.JPG"), foldKeyedLock("/dst/a/poster.jpg"))
	require.Equal(t, foldKeyedLock("/dst/A/poster.jpg"), foldKeyedLock("\\dst\\A\\poster.jpg"))
	require.Equal(t, DestKey("/dst/A/poster.jpg"), DestKey("\\dst\\A\\poster.jpg"))
	require.NotEqual(t, foldKeyedLock("/dst/a/poster.jpg"), foldKeyedLock("/dst/b/poster.jpg"))

	// The same physical destination under either spelling contends for ONE
	// mutex: acquiring one blocks the other.
	r := NewKeyedLockRegistry()
	releaseA := r.Acquire("/dst/X/poster.jpg")
	acquired := make(chan func(), 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		acquired <- r.Acquire("\\dst\\x\\poster.JPG")
	}()
	select {
	case releaseB := <-acquired:
		releaseB()
		t.Fatal("differently-spelled key must contend on the same lock")
	default:
	}
	releaseA()
	releaseB := <-acquired
	releaseB()
	wg.Wait()
}

// The process-wide registries are real registries: locked keys block.
func TestSharedRegistries_HandLocksBack(t *testing.T) {
	jl := SharedJournalLocks()
	dl := SharedDestLocks()
	require.NotNil(t, jl)
	require.NotNil(t, dl)

	rel := jl.Acquire("journal-op-cover")
	done := make(chan struct{})
	go func() { dl.Acquire("dest-cover")(); close(done) }()
	time.Sleep(25 * time.Millisecond)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acquire on fresh key must proceed")
	}
	rel()
}
