package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// stagingOrderFs records the event order on ONE shared staging file
// (<dest>.tmp) and stalls the first writer's first Write so a second,
// unsynchronized stager has a deterministic window to interleave.
type stagingOrderFs struct {
	afero.Fs
	tmpPath         string
	mu              sync.Mutex
	events          []string
	secondCreate    chan struct{}
	allowFirstWrite chan struct{}
	wrappedFirst    bool
	renameSeen      bool
}

func (f *stagingOrderFs) record(ev string) {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
}

func (f *stagingOrderFs) Create(name string) (afero.File, error) {
	file, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	if name != f.tmpPath {
		return file, nil
	}
	if !f.wrappedFirst {
		f.wrappedFirst = true
		f.record("create1")
		return &stallFirstWriteFile{File: file, gate: f.allowFirstWrite}, nil
	}
	f.record("create2")
	select {
	case <-f.secondCreate:
	default:
		close(f.secondCreate)
	}
	return file, nil
}

func (f *stagingOrderFs) Rename(oldname, newname string) error {
	if oldname == f.tmpPath {
		f.mu.Lock()
		f.renameSeen = true
		f.mu.Unlock()
		f.record("rename")
	}
	return f.Fs.Rename(oldname, newname)
}

type stallFirstWriteFile struct {
	afero.File
	once sync.Once
	gate <-chan struct{}
}

func (w *stallFirstWriteFile) Write(p []byte) (int, error) {
	w.once.Do(func() { <-w.gate })
	return w.File.Write(p)
}

// TestDownload_ConcurrentSameDestSerialized pins L3: a cover download and a
// high-quality poster download resolving to the SAME destPath (multipart
// siblings can share a part-less template path) must not stage through
// <dest>.tmp concurrently — the second writer may only touch the staging
// file after the first writer's rename (or skip entirely via the Stat
// short-circuit). The final content must be exactly one coherent body.
func TestDownload_ConcurrentSameDestSerialized(t *testing.T) {
	bodyCover := make([]byte, 64*1024)
	for i := range bodyCover {
		bodyCover[i] = 'A'
	}
	bodyPoster := make([]byte, 64*1024)
	for i := range bodyPoster {
		bodyPoster[i] = 'B'
	}
	coverSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bodyCover)
	}))
	defer coverSrv.Close()
	posterSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bodyPoster)
	}))
	defer posterSrv.Close()

	tmpDir := t.TempDir()
	// Force cover and HQ poster onto the SAME destination path — the shared
	// destPath the multipart workers race on.
	cfg := &Config{
		DownloadCover:  true,
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat: "<ID>-same.jpg",
			PosterFormat: "<ID>-same.jpg",
		},
	}
	movie := &models.Movie{ID: "LCK-001", Poster: models.PosterState{
		CoverURL:  coverSrv.URL + "/cover.jpg",
		PosterURL: posterSrv.URL + "/poster.jpg",
		// High-quality poster: straight download, no crop staging lock of its own.
		ShouldCropPoster: false,
	}}

	destPath := filepath.Join(tmpDir, "LCK-001-same.jpg")
	fs := &stagingOrderFs{
		Fs:              afero.NewOsFs(),
		tmpPath:         destPath + ".tmp",
		secondCreate:    make(chan struct{}),
		allowFirstWrite: make(chan struct{}),
	}
	d := NewDownloader(http.DefaultClient, fs, cfg, nil)

	var wg sync.WaitGroup
	results := make([]*DownloadResult, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = d.downloadCover(context.Background(), movie, tmpDir, nil)
	}()
	go func() {
		defer wg.Done()
		results[1], errs[1] = d.downloadPoster(context.Background(), movie, tmpDir, nil, false)
	}()

	// Give a (broken) concurrent second stager a wide deterministic window to
	// Create the shared tmp while writer 1 is stalled mid-write; then release.
	select {
	case <-fs.secondCreate:
		t.Log("second staging Create interleaved — expected only pre-fix")
	case <-time.After(750 * time.Millisecond):
	}
	close(fs.allowFirstWrite)
	wg.Wait()

	for i := range results {
		require.NotNil(t, results[i])
		require.NoError(t, errs[i], "worker %d: %v", i, errs[i])
		require.NoError(t, results[i].Error, "worker %d: %v", i, results[i].Error)
	}

	fs.mu.Lock()
	events := append([]string(nil), fs.events...)
	renameSeen := fs.renameSeen
	fs.mu.Unlock()

	secondCreateIdx, firstRenameIdx := -1, -1
	for i, ev := range events {
		if ev == "create2" && secondCreateIdx == -1 {
			secondCreateIdx = i
		}
		if ev == "rename" && firstRenameIdx == -1 {
			firstRenameIdx = i
		}
	}
	if secondCreateIdx != -1 {
		require.True(t, renameSeen && firstRenameIdx != -1 && firstRenameIdx < secondCreateIdx,
			"a second stager touched the shared %s before the first rename completed — per-dest serialization missing (events: %v)", fs.tmpPath, events)
	}

	got, err := afero.ReadFile(afero.NewOsFs(), destPath)
	require.NoError(t, err)
	if string(got) != string(bodyCover) && string(got) != string(bodyPoster) {
		t.Fatalf("final destination is a MIX of the two downloads (%d bytes) — concurrent staging corrupted it", len(got))
	}
	// Exactly one downloads; the winner installs the file, the loser Stat-skips.
	downloaded := 0
	for _, r := range results {
		if r.Downloaded {
			downloaded++
		}
	}
	assert.Equal(t, 1, downloaded, "exactly one worker must install the shared destination")
}
