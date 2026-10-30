package fscase_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/worker/fscase"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// delayedProbeFS delegates genuine filesystem operations, delaying only the
// first OpenFile. Its bookkeeping lock is never held during the delay or I/O:
// the adapter must not itself serialize competing probes.
type delayedProbeFS struct {
	afero.Fs
	mu      sync.Mutex
	writes  []string
	reads   []string
	removes []string
	entered chan struct{}
	release <-chan struct{}
}

func (f *delayedProbeFS) OpenFile(name string, flag int, mode os.FileMode) (afero.File, error) {
	f.mu.Lock()
	first := len(f.writes) == 0
	f.writes = append(f.writes, name)
	f.mu.Unlock()
	if first {
		close(f.entered)
		<-f.release
	}
	return f.Fs.OpenFile(name, flag, mode)
}

func (f *delayedProbeFS) Open(name string) (afero.File, error) {
	f.mu.Lock()
	f.reads = append(f.reads, name)
	f.mu.Unlock()
	return f.Fs.Open(name)
}

func (f *delayedProbeFS) Remove(name string) error {
	f.mu.Lock()
	f.removes = append(f.removes, name)
	f.mu.Unlock()
	return f.Fs.Remove(name)
}

func (f *delayedProbeFS) snapshot() (writes, reads, removes []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...), append([]string(nil), f.reads...), append([]string(nil), f.removes...)
}

func TestFSCaseCacheConcurrentCallersShareProbeAndPreserveFiles(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "owned")
	require.NoError(t, base.MkdirAll(dir, 0755))
	sentinel := filepath.Join(dir, ".javinizer_case_test_1")
	payload := []byte("preexisting user file")
	require.NoError(t, afero.WriteFile(base, sentinel, payload, 0600))
	before, err := base.Stat(sentinel)
	require.NoError(t, err)

	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	fs := &delayedProbeFS{Fs: base, entered: make(chan struct{}), release: release}
	cache := fscase.NewFSCaseCache(fs)
	const followers = 8
	results := make(chan bool, followers+1)
	started := make(chan struct{}, followers)
	var workers sync.WaitGroup
	// Register cleanup before starting any worker; failed assertions must not
	// leave a filesystem operation blocked. Timers are deadlock watchdogs,
	// not evidence that callers have reached any internal synchronization.
	t.Cleanup(func() {
		unblock()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("case-cache callers did not finish after filesystem release")
		}
	})
	launch := func(follower bool) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if follower {
				started <- struct{}{}
			}
			results <- cache.IsCaseInsensitive(dir)
		}()
	}
	launch(false)
	select {
	case <-fs.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first public lookup did not start its filesystem probe")
	}
	for i := 0; i < followers; i++ {
		launch(true)
	}
	for i := 0; i < followers; i++ {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("follower did not start")
		}
	}
	// These acknowledgments establish launched callers while the first I/O
	// is pending, NOT that every follower crossed the initial cache lookup
	// or reached the private probe mutex. No particular branch hit is promised.
	unblock()
	for i := 0; i <= followers; i++ {
		select {
		case result := <-results:
			require.False(t, result, "the owned MemMapFs is case-sensitive")
		case <-time.After(10 * time.Second):
			t.Fatal("public lookup failed to complete")
		}
	}
	writes, reads, removes := fs.snapshot()
	require.Len(t, writes, 2, "all callers must share one two-file probe")
	require.Equal(t, []string{writes[0]}, reads)
	require.ElementsMatch(t, writes, removes)
	require.Equal(t, dir, filepath.Dir(writes[0]))
	require.True(t, strings.HasPrefix(filepath.Base(writes[0]), ".javinizer_case_test_"))
	require.Equal(t, filepath.Join(dir, ".JAVINIZER_CASE_TEST_"+strings.TrimPrefix(filepath.Base(writes[0]), ".javinizer_case_test_")), writes[1])
	require.NotEqual(t, sentinel, writes[0])
	got, err := afero.ReadFile(base, sentinel)
	require.NoError(t, err)
	require.Equal(t, payload, got)
	after, err := base.Stat(sentinel)
	require.NoError(t, err)
	require.Equal(t, before.Mode(), after.Mode())
	require.Equal(t, before.ModTime(), after.ModTime())
	entries, err := afero.ReadDir(base, dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "both randomized probe files must be cleaned up")
	require.Equal(t, filepath.Base(sentinel), entries[0].Name())

	require.False(t, cache.IsCaseInsensitive(dir))
	cachedWrites, cachedReads, cachedRemoves := fs.snapshot()
	require.Equal(t, writes, cachedWrites)
	require.Equal(t, reads, cachedReads)
	require.Equal(t, removes, cachedRemoves, "cached lookup must perform no filesystem I/O")
}
