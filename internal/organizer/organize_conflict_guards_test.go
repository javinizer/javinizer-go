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

func TestLockRegistry_BoundedUnderChurn(t *testing.T) {
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
	operationLocks.mu.Lock()
	size := len(operationLocks.items)
	operationLocks.mu.Unlock()
	assert.Equal(t, 0, size, "unused entries must be evicted when last waiter releases")
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
