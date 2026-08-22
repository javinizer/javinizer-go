package downloader

// POSTER-WRITE-HARDENING wave-47 (codex P2, PR#215 finding F1-media) —
// wave-45's handle-derived install provenance mirrored onto the
// cropped-poster install path (downloadPoster). The crop writers
// (imageutil.CropPosterFromCover / CropPosterWithBounds) hand back no open
// handle, so downloadPoster freezes the candidate's identity from the
// post-write no-follow lstat immediately after the crop/write completes
// (captureInstalledDestIdentity) and hands it to installOverwriting through
// the variadic provenance — the wave-45 gate's stagedInputUnrecorded
// classification no longer covers the cropped candidate. A substitute
// rotated onto the candidate name between crop/write and install now trips
// the errStagedInputSubstituted refusal on BOTH the create and replace legs:
// the destination is preserved (never published / set-aside restored +
// journal retracted), the substitute is preserved byte-intact
// (downloadPoster's stagedRetained gate keeps the deferred scratch cleanup
// off the possibly-foreign name, the wave-42 retention discipline), and the
// refusal is warn-logged. The plain path — no window rotation — installs
// exactly the cropped candidate as before.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// w47CropSwapFs interposes the crop/write→install window deterministically:
// the cropped candidate name is lstat'd by downloadPoster's PRODUCER record
// capture (wave-66: at the crop, in both modes), then by the install-time
// provenance bind (wave-48), and again by installOverwriting's re-proof. The
// fireOn-th no-follow lookup of the candidate name parks the genuine object
// aside ("*.hidden") and plants foreign bytes at the name — exactly the
// directory-writer rotation finding F1-media closes.
type w47CropSwapFs struct {
	afero.Fs
	fireOn    int // which no-follow lookup of the candidate name replays the swap
	plant     []byte
	mu        sync.Mutex
	calls     int
	candidate string
	swapped   bool
	swapErr   error
}

func (f *w47CropSwapFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	f.mu.Lock()
	if !f.swapped && f.swapErr == nil && strings.HasSuffix(name, ".crop.tmp") {
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

// (a) Create-mode substitution, end-to-end through Download: the refusal is
// the typed sentinel, the destination is never stored and stays OUT of the
// CreatedPaths accounting, the foreign substitute survives byte-intact at
// the retained candidate name (our own full-source scratch still reaps), and
// the posture is warn-logged.
func TestDownloadPosterW47_CandidateSwapBetweenCropAndInstallRefused(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	// fireOn 2 (wave-66 numbering: #1 = the producer-record capture at the
	// crop, #2 = the install-time bind's Lstat) replays a substitution
	// INSIDE the producer-write→bind window: the bind's Lstat reads the
	// substitute, the wave-66 producer-record gate refuses typed before any
	// handle is opened — pre-wave-66 the substitute authenticated against
	// itself on this very leg.
	fsW := &w47CropSwapFs{Fs: base, fireOn: 2, plant: []byte("w47 foreign candidate substitute — planted post-crop")}
	movie := w42CropMovie("W47-CREATE", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	d := NewDownloader(server.Client(), fsW, w42CropPosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	outcome, err := d.Download(context.Background(), DownloadCmd{Movie: movie, DestDir: "/output", OverwriteExistingMedia: true})
	require.Error(t, err)
	var partial *DownloadPartialError
	require.ErrorAs(t, err, &partial, "the refused poster is a failed critical download")
	require.NotNil(t, outcome)
	require.NotContains(t, outcome.CreatedPaths, dest, "nothing was published — the refusal must not enter CreatedPaths")
	require.NotContains(t, outcome.DownloadedPaths, dest)

	var result DownloadResult
	found := false
	for _, r := range outcome.Results {
		if r.Type == MediaTypePoster {
			result, found = r, true
		}
	}
	require.True(t, found, "a poster result is produced")
	require.ErrorIs(t, result.Error, errStagedInputSubstituted)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.False(t, result.Skipped)
	require.Empty(t, result.LocalPath)

	_, statErr := base.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the substitute must never reach the destination")

	require.True(t, fsW.swapped, "the window swap replay fired")
	require.NoError(t, fsW.swapErr)
	require.NotEmpty(t, fsW.candidate)
	require.Equal(t, fsW.plant, mustReadDownloaderW7(t, base, fsW.candidate),
		"the stagedRetained gate kept the deferred cleanup off the candidate name — foreign bytes preserved")

	// WHICH deferred cleanup runs: our own full-source scratch still reaps;
	// only the retained candidate name (substitute) and the hook's parked
	// genuine object remain.
	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{filepath.Base(fsW.candidate), filepath.Base(fsW.candidate) + ".hidden"}, names,
		"full.tmp scratch reaped; the retained candidate name carries the preserved substitute")

	// The parked genuine object is the CROPPED candidate we produced.
	_, w, h := decodeResultPoster(t, base, fsW.candidate+".hidden")
	require.InDelta(t, 472, w, 3)
	require.Equal(t, 600, h)

	require.Contains(t, logs.String(), "no longer names the crop/write-produced object")
	require.Contains(t, logs.String(), "substitute preserved")
}

// (b) Plain path unchanged: with provenance armed and NO window rotation the
// cropped candidate installs normally and both scratch names reap.
func TestDownloadPosterW47_ProvenanceArmedCroppedInstallUnchanged(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	movie := w42CropMovie("W47-PLAIN", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	d := NewDownloader(server.Client(), base, w42CropPosterConfig(), nil)

	outcome, err := d.Download(context.Background(), DownloadCmd{Movie: movie, DestDir: "/output", OverwriteExistingMedia: true})
	require.NoError(t, err)
	require.Contains(t, outcome.CreatedPaths, dest)

	_, w, h := decodeResultPoster(t, base, dest)
	require.InDelta(t, 472, w, 3, "the provenance-armed install publishes the cropped candidate exactly as before")
	require.Equal(t, 600, h)

	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "both scratch names reaped on the plain path")
	require.Equal(t, filepath.Base(dest), entries[0].Name())
}

// (c) Replace-mode substitution: the refusal fires BEFORE the publish (the
// wave-26 baseline capture is bound to the VALIDATED crop object), so the
// set-aside restore returns the pre-existing destination bytes, the armed
// journal entry is retracted with the consumed backup, and the foreign
// substitute at the candidate name is preserved byte-intact.
func TestDownloadPosterW47_ReplacePathRefusesSubstitutedCandidate(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	preExisting := []byte("pre-existing poster bytes — never published-over")
	// fireOn 3: wave-66 added the producer-record capture at the crop (#1),
	// so the publish-adjacent lookup the replace-path compensation is asserted
	// against is #2 = the bind's Lstat, #3 = installOverwriting's re-proof.
	fsW := &w47CropSwapFs{Fs: base, fireOn: 3, plant: []byte("w47 replace-window foreign substitute")}
	movie := w42CropMovie("W47-REPLACE", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, preExisting, 0o644))

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	recorder := &armedTestLedger{}
	d := NewDownloader(server.Client(), fsW, w42CropPosterConfig(), nil)
	result, err := d.downloadPoster(context.Background(), movie, "/output", nil, true, nil,
		downloadLedger{opID: "w47-replace", recorder: recorder})
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.ErrorIs(t, result.Error, errStagedInputSubstituted)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.False(t, result.Skipped)
	require.Empty(t, result.LocalPath)

	require.Equal(t, preExisting, mustReadDownloaderW7(t, base, dest),
		"the publish-failure compensation restored the pre-existing destination bytes")

	require.True(t, fsW.swapped, "the window swap replay fired")
	require.Equal(t, fsW.plant, mustReadDownloaderW7(t, base, fsW.candidate),
		"the substitute is preserved byte-intact at the retained candidate name")

	require.Empty(t, recorder.get(), "the armed journal entry was retracted with the rollback")
	require.Len(t, recorder.released, 1)
	_, berr := base.Stat(recorder.released[0].backupPath)
	require.ErrorIs(t, berr, os.ErrNotExist, "the set-aside was consumed by the no-replace restore")

	// The parked genuine object is the cropped candidate we produced.
	_, w, _ := decodeResultPoster(t, base, fsW.candidate+".hidden")
	require.InDelta(t, 472, w, 3)

	require.Contains(t, logs.String(), "no longer names the crop/write-produced object")
}
