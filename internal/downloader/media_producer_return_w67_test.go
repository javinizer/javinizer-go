package downloader

// POSTER-WRITE-HARDENING wave-67 (codex P2, PR#215 — producer-RETURNED
// identity records). Pre-wave-67 the candidate's producer record was a
// caller-side Lstat of the mutable scratch name taken AFTER the producer had
// already returned (downloadPoster's post-download capture of fullPath,
// post-crop capture of cropPath): a foreign swap landing between
// producer-return and capture had the entire provenance chain authenticate
// the substitute. The producers now carry the identity OUT with their
// results — http.download files installOverwritingIdentity's
// post-publish-VERIFIED destination identity on DownloadResult
// (copyBackupToDestPublish's facts.restored shape), and the crop writers
// (imageutil.CropPosterFromCover / CropPosterWithBounds through
// cropDownloadedPoster) hand back their own post-write FileInfo. The legs
// below: a foreign substitute rotated onto the full-download candidate name
// between the producer record and the install-time bind is refused typed
// (substitute preserved byte-intact, destination untouched, scratch
// retained); the normal fallback path (manual crop geometry unusable → the
// full download IS the candidate) installs with both scratch names reaped;
// and installOverwritingIdentity hands back a verified record that provably
// names the landed destination on the real OsFs legs AND the virtual
// (MemMapFs) leg, plus d.download filing the record end to end.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w67FullSwapFs replays a foreign substitution of the full-download scratch
// name (".full.tmp") at the fireOn-th no-follow lookup of that name. The
// overwrite-mode full-download leg looks the name up in this order: #1 the
// create-install's destination classification, #2 publishNoReplaceVirtual's
// pre-publish probe, #3 the wave-67 producer record (the install's
// post-publish verification capture), #4 the install-time bind's Lstat.
// Firing at #4 lands the substitute INSIDE the producer-record→bind window
// the pre-wave-67 caller-side capture used to authenticate.
type w67FullSwapFs struct {
	afero.Fs
	fireOn    int // which no-follow lookup of the full-download name replays the swap
	plant     []byte
	mu        sync.Mutex
	calls     int
	candidate string
	swapped   bool
	swapErr   error
}

func (f *w67FullSwapFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	f.mu.Lock()
	if !f.swapped && f.swapErr == nil && strings.HasSuffix(name, ".full.tmp") {
		f.calls++
		if f.calls == f.fireOn {
			f.candidate = name
			if rerr := f.Fs.Rename(name, name+".hidden"); rerr != nil {
				f.swapErr = rerr
			} else if werr := afero.WriteFile(f.Fs, name, f.plant, 0o600); werr != nil {
				f.swapErr = werr
			} else {
				f.swapped = true
			}
		}
	}
	f.mu.Unlock()
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// w67FallbackMovie carries valid CONTAINMENT geometry whose source aspect
// provably mismatches the two-tone 1000x600 source: the manual crop refuses
// on the aspect guard and downloadPoster falls back to installing the FULL
// download — the leg whose producer record must come from http.download's
// verified publish, never a post-return re-lookup of fullPath.
func w67FallbackMovie(id, url string) *models.Movie {
	return &models.Movie{ID: id, Poster: models.PosterState{
		PosterURL:            url,
		PosterCropBounds:     &models.CropBounds{X: 0, Y: 0, Width: 0.5, Height: 1, SourceAspect: 0.5},
		PosterCropSourceFull: true,
	}}
}

// (a) Substitution inside the producer-record→bind window, end to end: the
// bind's wave-66 producer gate refuses typed (Lstat ≠ the returned record),
// the substitute stays byte-intact at the retained candidate name, the
// destination is never stored, and the refusal is warn-logged.
func TestDownloadPosterW67_FullCandidateSwapBetweenProducerAndBindRefused(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	plant := []byte("w67 foreign substitute — planted post-producer-record, pre-bind")
	fsW := &w67FullSwapFs{Fs: base, fireOn: 4, plant: plant}
	movie := w67FallbackMovie("W67-FULL", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	d := NewDownloader(server.Client(), fsW, w42CropPosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	result, err := d.downloadPoster(context.Background(), movie, "/output", nil, true)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.ErrorIs(t, result.Error, errStagedInputSubstituted)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.Empty(t, result.LocalPath)

	_, statErr := base.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the substitute must never reach the destination")

	require.True(t, fsW.swapped, "the producer-record→bind swap replay fired")
	require.NoError(t, fsW.swapErr)
	require.Equal(t, plant, mustReadDownloaderW7(t, base, fsW.candidate),
		"the refusal kept the surviving name byte-intact for manual cleanup")

	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{filepath.Base(fsW.candidate), filepath.Base(fsW.candidate) + ".hidden"}, names,
		"the retained candidate keeps the substitute; the crop scratch stayed absent")

	// The parked genuine object is the full download the returned producer
	// record named — 1000x600, never the substitute's bytes.
	_, w, h := decodeResultPoster(t, base, fsW.candidate+".hidden")
	require.Equal(t, 1000, w)
	require.Equal(t, 600, h)

	require.Contains(t, logs.String(), "no longer names the crop/write-produced object")
	require.Contains(t, logs.String(), "substitute preserved")
}

// (b) Normal path: the aspect-mismatched manual crop falls back, the FULL
// download installs as the candidate, and the producer-record binding keeps
// the scratch reaping exact (both temp names gone).
func TestDownloadPosterW67_FullCandidateProducerRecordInstallsNormal(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	movie := w67FallbackMovie("W67-NORMAL", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	d := NewDownloader(server.Client(), base, w42CropPosterConfig(), nil)

	result, err := d.downloadPoster(context.Background(), movie, "/output", nil, true)
	require.NoError(t, err)
	require.True(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.True(t, result.producerIdentity.known,
		"the full download filed the verified publish's producer record on its result")

	// The manual-crop aspect guard fell back: the published bytes are the
	// FULL two-tone source (1000x600), not a crop.
	_, w, h := decodeResultPoster(t, base, dest)
	require.Equal(t, 1000, w)
	require.Equal(t, 600, h)

	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "both scratch names reaped/absent — the producer-record binding keeps cleanup exact")
	require.Equal(t, filepath.Base(dest), entries[0].Name())
}

// (c) The installer's identity hand-back on all three publish flavors: the
// returned record must name the landed destination object itself.
func TestInstallOverwritingIdentityW67_VerifiedIdentityRidesOut(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()

	t.Run("create leg — the bound publish's verified identity", func(t *testing.T) {
		staged, dest, prov := w48Stage(t, fs, filepath.Join(dir, "create"), "w67-genuine-create")
		d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

		var out installedDestIdentity
		skipped, replaced, err := d.installOverwritingIdentity(context.Background(), staged, dest, downloadLedger{}, &out, prov)
		require.NoError(t, err)
		require.False(t, skipped)
		require.False(t, replaced)
		require.True(t, out.known, "the completed create files the producer record")
		require.EqualValues(t, len("w67-genuine-create"), out.size)
		if runtime.GOOS != "windows" {
			require.True(t, out.hasDevIno, "the OsFs bound publish carries kernel identity")
		}
		require.True(t, destStillHoldsInstalledObject(fs, dest, out),
			"the record names the landed destination object")
		got, rerr := os.ReadFile(dest)
		require.NoError(t, rerr)
		require.Equal(t, "w67-genuine-create", string(got))
	})

	t.Run("replace leg — the wave-31 verified baseline rides out", func(t *testing.T) {
		staged, dest, prov := w48Stage(t, fs, filepath.Join(dir, "replace"), "w67-genuine-replace")
		require.NoError(t, os.WriteFile(dest, []byte("old poster bytes"), 0o644))
		ledger := &w45Ledger{}
		d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

		var out installedDestIdentity
		skipped, replaced, err := d.installOverwritingIdentity(context.Background(), staged, dest,
			downloadLedger{opID: "w67-replace", recorder: ledger}, &out, prov)
		require.NoError(t, err)
		require.False(t, skipped)
		require.True(t, replaced)
		require.True(t, out.known)
		if runtime.GOOS != "windows" {
			require.True(t, out.hasDevIno)
		}
		require.True(t, destStillHoldsInstalledObject(fs, dest, out))
		got, rerr := os.ReadFile(dest)
		require.NoError(t, rerr)
		require.Equal(t, "w67-genuine-replace", string(got))
		require.Equal(t, 1, ledger.confirmed)
	})

	t.Run("virtual leg — the MemMapFs post-publish capture", func(t *testing.T) {
		mfs := afero.NewMemMapFs()
		staged, dest, prov := w48StageMem(t, mfs, "/w67mem", "w67-genuine-mem")
		d := NewDownloader(nil, mfs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

		var out installedDestIdentity
		skipped, replaced, err := d.installOverwritingIdentity(context.Background(), staged, dest, downloadLedger{}, &out, prov)
		require.NoError(t, err)
		require.False(t, skipped)
		require.False(t, replaced)
		require.True(t, out.known)
		require.False(t, out.hasDevIno, "MemMapFs exposes no kernel identity")
		require.True(t, destStillHoldsInstalledObject(mfs, dest, out))
	})
}

// (d) d.download files the record on its result end to end — the record must
// provably name the landed destination on both filesystem flavors.
func TestDownloadW67_ProducerIdentityFiledOnResult(t *testing.T) {
	server := serveTwoToneSource(t)

	t.Run("MemMapFs — virtual-leg verified capture", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		d := NewDownloader(server.Client(), fs, w42CropPosterConfig(), nil)

		result, err := d.download(context.Background(), server.URL+"/cover.jpg", "/out/poster.jpg", MediaTypePoster, true)
		require.NoError(t, err)
		require.True(t, result.Downloaded)
		require.True(t, result.producerIdentity.known,
			"the verified publish's identity rides the result")
		require.False(t, result.producerIdentity.hasDevIno)
		require.True(t, destStillHoldsInstalledObject(fs, "/out/poster.jpg", result.producerIdentity))
	})

	t.Run("OsFs — bound publish's SameFile-proven identity", func(t *testing.T) {
		fs := afero.NewOsFs()
		dest := filepath.Join(t.TempDir(), "poster.jpg")
		d := NewDownloader(server.Client(), fs, w42CropPosterConfig(), nil)

		result, err := d.download(context.Background(), server.URL+"/cover.jpg", dest, MediaTypePoster, true)
		require.NoError(t, err)
		require.True(t, result.Downloaded)
		require.True(t, result.producerIdentity.known)
		if runtime.GOOS != "windows" {
			require.True(t, result.producerIdentity.hasDevIno)
		}
		require.True(t, destStillHoldsInstalledObject(fs, dest, result.producerIdentity))
	})
}
