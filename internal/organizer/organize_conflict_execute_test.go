package organizer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Concurrent workers racing to one destination: exactly one wins, losers get conflict
// errors, and the existing content is never silently replaced.
func TestOrganizeStrategy_Execute_ConcurrentSameDestination_ExactlyOneWins(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()

	const workers = 10
	var okCount, failCount int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start // release all workers at once for the same destination
			src := fmt.Sprintf("/incoming/IPX-123-%02d.mp4", i)
			if err := afero.WriteFile(fs, src, []byte(fmt.Sprintf("part-%d", i)), 0644); err != nil {
				t.Errorf("write: %v", err)
				return
			}
			match := models.FileMatchInfo{Path: src, Name: filepath.Base(src), Extension: ".mp4", MovieID: "IPX-123"}
			_, err := org.Organize(context.Background(), OrganizeCmd{
				Match: match, Movie: movie, DestDir: "/sorted", MoveFiles: true,
			})
			if err != nil {
				atomic.AddInt64(&failCount, 1)
			} else {
				atomic.AddInt64(&okCount, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int64(1), atomic.LoadInt64(&okCount))
	assert.Equal(t, int64(workers-1), atomic.LoadInt64(&failCount))

	entries, err := afero.ReadDir(fs, "/sorted/IPX-123")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	data, err := afero.ReadFile(fs, "/sorted/IPX-123/IPX-123.mp4")
	require.NoError(t, err)
	assert.Contains(t, string(data), "part-")
}

// Same-inode aliasing must keep working: a destination that is a hardlink alias of the
// source is an idempotent no-op, not a conflict.
func TestOrganizeStrategy_Execute_SameInodeDestination_NoConflict(t *testing.T) {
	if _, err := os.Stat("/tmp"); err != nil {
		t.Skip("os fs unavailable")
	}
	// MemMapFs cannot create hardlinks; use the real filesystem.
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "IPX-999-a.mp4")
	aliasDir := filepath.Join(dir, "out", "IPX-999")
	require.NoError(t, os.MkdirAll(aliasDir, 0755))
	require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0644))
	require.NoError(t, os.Link(src, filepath.Join(aliasDir, "IPX-999.mp4")))

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-999").Build()
	match := models.FileMatchInfo{Path: src, Name: "IPX-999-a.mp4", Extension: ".mp4", MovieID: "IPX-999"}

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, DestDir: filepath.Join(dir, "out"), MoveFiles: true,
	})
	require.NoError(t, err)
	assert.True(t, result.Moved)

	data, err := os.ReadFile(filepath.Join(aliasDir, "IPX-999.mp4"))
	require.NoError(t, err)
	assert.Equal(t, []byte("shared-bytes"), data)

	// Idempotent same-inode move must NOT consume the source path entry.
	srcData, err := os.ReadFile(src)
	require.NoError(t, err, "source alias must be preserved on same-inode no-op")
	assert.Equal(t, []byte("shared-bytes"), srcData)
}
