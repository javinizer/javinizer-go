package organizer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Subtitle destinations are race-safe: a subtitle created between check and move is
// preserved and the incoming one is skipped.
func TestOrganize_Subtitle_LateExistingSkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:       "<ID>",
		FileFormat:         "<ID>",
		RenameFile:         true,
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
		OperationMode:      operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-535").Build()

	require.NoError(t, org.fs.MkdirAll("/src", 0755))
	require.NoError(t, afero.WriteFile(fs, "/src/IPX-535.mp4", []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/src/IPX-535.srt", []byte("subtitle-mine"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/dest/IPX-535/IPX-535.srt", []byte("subtitle-existing"), 0644))

	match := models.FileMatchInfo{Path: "/src/IPX-535.mp4", Name: "IPX-535.mp4", Extension: ".mp4", MovieID: "IPX-535"}
	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, DestDir: "/dest", MoveFiles: true,
	})
	require.NoError(t, err)
	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Skipped, "existing subtitle must be skipped")
	data, _ := afero.ReadFile(fs, "/dest/IPX-535/IPX-535.srt")
	assert.Equal(t, []byte("subtitle-existing"), data, "existing subtitle preserved")
}

// Boundedness/GC of the underlying registry is pinned white-box in
// internal/fsutil (keyed_lock_shared_test.go); these churn cases pin that the
// organizer wrappers ride it without wedging.
func TestLockRegistry_ChurnThroughSharedRegistry(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				dst := fmt.Sprintf("/dst-%d", j%25)
				_ = withDestFileLock(dst, func() error { return nil })
			}
		}(i)
	}
	wg.Wait()
	done := make(chan struct{})
	go func() {
		_ = withDestFileLock("/dst-1", func() error { return nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a churned destination key must be handed out again once traffic drains")
	}
}

// Verifies a lock mutually excludes simultaneous waiters: the in-flight counter must
// never exceed 1 across all queued critical sections — an unlocked or mis-keyed
// implementation fails this even while still running every callback.
func TestLockRegistry_WaiterQueuedSerialized(t *testing.T) {
	var inFlight atomic.Int32
	var maxSeen atomic.Int32
	executed := make(chan int, 4)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 1; i <= 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_ = withDestFileLock("/x-child", func() error {
				cur := inFlight.Add(1)
				for {
					m := maxSeen.Load()
					if cur <= m || maxSeen.CompareAndSwap(m, cur) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				inFlight.Add(-1)
				executed <- n
				return nil
			})
		}(i)
	}
	close(start)
	wg.Wait()
	close(executed)
	assert.Equal(t, int32(1), maxSeen.Load(), "critical sections under one key must never overlap")
	count := 0
	for range executed {
		count++
	}
	assert.Equal(t, 4, count, "every queued waiter must be served exactly once")
}

// Exclusive directory locks exclude concurrent shared child writes and vice versa:
// a rename-in-progress must never drag a freshly landed file away (and a landed file
// must never be followed by a rollback into the wrong directory).
func TestDirOperationLocks_ExclusiveExcludesShared(t *testing.T) {
	held := make(chan struct{})
	release := make(chan struct{})
	exclusiveDone := make(chan struct{})
	go func() {
		defer close(exclusiveDone)
		_ = withDestDirExclusiveLock("/target-dir", func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	sharedEntered := make(chan struct{})
	go func() {
		defer close(sharedEntered)
		_ = withDestDirSharedLock("/target-dir", func() error { return nil })
	}()

	select {
	case <-sharedEntered:
		t.Fatal("shared child lock entered while the exclusive directory rename was held")
	case <-time.After(100 * time.Millisecond): // scheduling grace; blocking is the assertion
	}
	close(release)
	<-exclusiveDone
	select {
	case <-sharedEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("shared child lock never entered after the exclusive holder released")
	}
}

func TestDirOperationLocks_SharedChildrenRunInParallel(t *testing.T) {
	var inFlight atomic.Int32
	var maxSeen atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = withDestDirSharedLock("/target-dir", func() error {
				cur := inFlight.Add(1)
				for {
					m := maxSeen.Load()
					if cur <= m || maxSeen.CompareAndSwap(m, cur) {
						break
					}
				}
				time.Sleep(2 * time.Millisecond)
				inFlight.Add(-1)
				return nil
			})
		}()
	}
	close(start)
	wg.Wait()
	assert.Greater(t, maxSeen.Load(), int32(1), "shared directory locks must permit concurrent child writes (batch copy throughput)")
}

func TestDirOperationLocks_DrainedSharedChurnAdmitsExclusive(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = withDestDirSharedLock(fmt.Sprintf("/evictable-%d", n), func() error { return nil })
		}(i)
	}
	wg.Wait()
	done := make(chan struct{})
	go func() {
		_ = withDestDirExclusiveLock("/evictable-1", func() error { return nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive directory lock starved after shared child traffic drained")
	}
}
