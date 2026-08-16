package fsutil

import (
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// codex P3 R4-4: destination-lock keys must fold separator spelling AND case
// — the downloader joins with the OS separator while journals use slashes.
func TestFoldKeyedLock_NormalizesSeparatorsAndCase(t *testing.T) {
	// Lock keys follow DestKey: case folds everywhere (over-locking is safe),
	// separators fold only where they CAN'T be filename characters (Windows).
	require.Equal(t, foldKeyedLock("/DST/a/Poster.JPG"), foldKeyedLock("/dst/a/poster.jpg"))
	if runtime.GOOS == "windows" {
		require.Equal(t, foldKeyedLock("/dst/A/poster.jpg"), foldKeyedLock("\\dst\\A\\poster.jpg"))
	} else {
		require.NotEqual(t, foldKeyedLock("/dst/A/poster.jpg"), foldKeyedLock("\\dst\\A\\poster.jpg"))
	}
	if runtime.GOOS == "windows" {
		require.Equal(t, DestKey("/dst/A/poster.jpg"), DestKey("\\dst\\A\\poster.jpg"))
	} else {
		// R17-1: POSIX/macOS keep distinct paths distinct.
		require.NotEqual(t, DestKey("/dst/A/poster.jpg"), DestKey("\\dst\\A\\poster.jpg"))
		require.NotEqual(t, DestKey("/DST/a/Poster.JPG"), DestKey("/dst/a/poster.jpg"))
	}
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
