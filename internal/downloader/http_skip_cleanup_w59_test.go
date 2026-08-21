package downloader

// POSTER-WRITE-HARDENING wave-59 (codex P2, PR#215 finding F2) — the
// skipped-download cleanup Removes tempPath by pathname after the provenance
// handle closed (installOverwriting's bound-publish ownership); a foreign
// file swapped onto tempPath inside the validation→cleanup window was
// deleted. The cleanup is now bound to the staged object: remove ONLY when
// tempPath still provably names the validated object (SameFile against the
// validation-time identity snapshot — the handle's never-mutable fstat, the
// handle itself being closed by installOverwriting); a foreign occupant is
// preserved byte-intact for manual cleanup. This test replays that swap on
// the download() skip path (unarmed ledger → installOverwriting publishes
// nothing) through the cleanup's first no-follow Lstat of the staged name.

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// w59SwapOnLstatFs swaps the first .tmp-staged name it sees via Lstat with
// the foreign plant — the deterministic form of a directory writer winning
// the validation→cleanup window. validateDownloadedMedia reads through the
// OPEN handle (fstat, never a path Lstat) and installOverwriting's skip path
// never Lstats the staged name, so the first path Lstat of tempPath is the
// cleanup's SameFile gate.
type w59SwapOnLstatFs struct {
	afero.Fs
	plant []byte
	fired atomic.Bool
}

func (f *w59SwapOnLstatFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if strings.HasSuffix(name, ".tmp") && f.fired.CompareAndSwap(false, true) {
		if err := afero.WriteFile(f.Fs, name, f.plant, 0o600); err != nil {
			return nil, false, err
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// A swap after skip preserves the foreign occupant at tempPath byte-intact
// and the download outcome's accounting stays the skip shape — pre-wave-59
// the pathname Remove deleted it.
func TestDownloadW59_SkipCleanupPreservesForeignOccupantAtTempPath(t *testing.T) {
	payload := []byte("w59-genuine-media-bytes")
	server := w41MediaServer(t, payload)

	base := afero.NewMemMapFs()
	destDir := "/output/W59-SKIP"
	dest := destDir + "/poster.jpg"
	require.NoError(t, base.MkdirAll(destDir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("existing artwork"), 0o644))

	plant := []byte("foreign-plant-occupant")
	fs := &w59SwapOnLstatFs{Fs: base, plant: plant}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	d := NewDownloader(server.Client(), fs, &Config{}, nil)
	result, err := d.download(context.Background(), server.URL+"/poster.jpg", dest, MediaTypePoster, true)
	require.NoError(t, err)
	require.True(t, result.Skipped, "the unarmed-ledger install skipped")
	require.False(t, result.Downloaded)
	require.Equal(t, dest, result.LocalPath)
	require.True(t, fs.fired.Load(), "the skip-cleanup SameFile gate must have Lstat'd the staged name")

	// The foreign occupant at the staged tempPath survived byte-intact —
	// pre-wave-59 the pathname Remove would have deleted it.
	entries, rerr := afero.ReadDir(base, destDir)
	require.NoError(t, rerr)
	foundPlant := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			foundPlant = true
			got, gerr := afero.ReadFile(base, destDir+"/"+e.Name())
			require.NoError(t, gerr)
			require.Equal(t, plant, got, "the foreign plant at the staged tempPath was preserved byte-intact")
		}
	}
	require.True(t, foundPlant, "the staged tempPath (with the foreign plant) was NOT removed")
	require.Contains(t, logs.String(), "no longer provably names the validated download")
}
