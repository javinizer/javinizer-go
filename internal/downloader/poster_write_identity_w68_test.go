package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-68 (P2) — the two codex findings
// this change closes:
//
//   - F1 (imageutil/crop.go): the crop producer's write-leg identity used to
//     be a post-close no-follow lookup of the just-written name, so a
//     substitute rotated onto the name inside the close→identity-capture
//     window authenticated against ITSELF downstream. The producer now
//     captures the identity FROM THE OPEN WRITE HANDLE before close (f.Stat)
//     — on a filesystem whose FileInfo.Sys() carries kernel identity (the
//     real OsFs, whose close does not re-stamp ModTime) the pre-close fstat
//     IS the binding record; a legacy/virtual fs whose Sys() is nil (afero
//     MemMapFs re-stamps ModTime at close) keeps the post-close lookup for
//     the durable ModTime (the wave-67 posture preserved for the no-identity
//     leg only).
//   - F2 (downloader/http.go): the ErrPublishCompleted leg returned NO
//     identity record, so downstream treated poster provenance as unknown
//     (skipped the producer gates) — a foreign temp replacement could then
//     ride publish-as-poster. The completed-WITH-identity leg (waves 61/62)
//     now hands the verified published identity into producerIdentity; when
//     that identity is genuinely unavailable (virtual-fs posture, or
//     ErrPublishCompleted without a dest info binding) the leg refuses to
//     certify an unproven publish instead of continuing (wave-53's
//     fail-closed posture).

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// w68WriteJPEG writes a flat-color JPEG of the given dimensions onto fs.
func w68WriteJPEG(t *testing.T, fs afero.Fs, path string, width, height int, col color.Color) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, col)
		}
	}
	f, err := fs.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, jpeg.Encode(f, img, &jpeg.Options{Quality: 95}))
}

// w68CropPostCloseSwapFs wraps an OsFs and swaps a substitute onto the poster
// name on the FIRST no-follow lookup — replaying a directory writer rotating
// a foreign object onto the name inside the close→identity-capture window.
// With the F1 fix the crop producer (OsFs, Sys() non-nil) captures the
// identity from the OPEN handle before close, so no no-follow lookup runs
// inside the producer and the swap does not fire there; the downstream bind's
// own Lstat is the first lookup, reads the substitute, and refuses typed
// against the genuine producer record.
type w68CropPostCloseSwapFs struct {
	afero.Fs
	poster  string
	plant   []byte
	mu      sync.Mutex
	swapped bool
}

func (f *w68CropPostCloseSwapFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	f.mu.Lock()
	if !f.swapped && filepath.Clean(name) == filepath.Clean(f.poster) {
		f.swapped = true
		_ = f.Fs.Rename(name, name+".hidden")
		_ = afero.WriteFile(f.Fs, name, f.plant, 0o644)
	}
	f.mu.Unlock()
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// TestCropProducerW68_PreCloseFstatSurvivesPostCloseSubstitution (F1): the
// crop producer captures the written object's identity FROM THE OPEN HANDLE
// before close, so a substitute rotated onto the name inside the
// close→identity-capture window cannot authenticate against itself
// downstream. w68CropPostCloseSwapFs swaps on the first no-follow lookup;
// with the fix the crop producer never looks the name up (the swap does not
// fire inside the producer), the returned record names the genuine object,
// and bindCandidateProvenance refuses the substitute the bind's own Lstat reads.
func TestCropProducerW68_PreCloseFstatSurvivesPostCloseSubstitution(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	cover := filepath.Join(dir, "cover.jpg")
	poster := filepath.Join(dir, "poster.jpg")
	w68WriteJPEG(t, fs, cover, 1000, 600, color.White) // landscape → right-side crop

	plant := []byte("w68 post-close substitute — foreign bytes")
	swapFs := &w68CropPostCloseSwapFs{Fs: fs, poster: poster, plant: plant}

	cropInfo, err := imageutil.CropPosterFromCover(swapFs, cover, poster, 0)
	require.NoError(t, err)
	require.NotNil(t, cropInfo)
	require.False(t, swapFs.swapped, "the crop producer captured the identity from the open handle — no post-close lookup ran")

	producer := installedIdentityFromFileInfo(cropInfo)
	require.True(t, producer.known)
	require.True(t, producer.hasDevIno, "OsFs carries kernel identity through the open handle")

	// The bind's Lstat is the first no-follow lookup — it fires the swap and
	// reads the substitute; the producer record names the genuine object, so
	// the bind refuses typed and the substitute stays byte-intact.
	prov, bindErr := bindCandidateProvenance(swapFs, poster, producer)
	require.ErrorIs(t, bindErr, errStagedInputSubstituted)
	require.False(t, prov.identity.known, "nothing verifiable is handed down on the refusal")
	require.Nil(t, prov.handle, "the refusal fires BEFORE any handle is opened")
	got, rerr := os.ReadFile(poster)
	require.NoError(t, rerr)
	require.Equal(t, plant, got, "the substitute is preserved byte-intact for manual cleanup")
}

// TestDownloadW68_PublishCompletedWithIdentityFilesProducerRecord (F2): the
// completed-with-identity leg (waves 61/62 — the ENOSYS-times-skipped publish
// hands back the post-publish-verified destination identity alongside
// ErrPublishCompleted) files THAT record on producerIdentity and records the
// completed publish as success (dest enters CreatedPaths through Downloaded &&
// !Replaced). The seam replays the ENOSYS leg without the cross-package
// fd-times plumbing; tempPath is preserved (the publish completed but the
// staged name could not be re-proven — possibly foreign).
func TestDownloadW68_PublishCompletedWithIdentityFilesProducerRecord(t *testing.T) {
	payload := []byte("w68-poster-bytes")
	server := w41MediaServer(t, payload)

	base := afero.NewMemMapFs()
	dest := "/output/W68-ID/poster.jpg"
	d := NewDownloader(server.Client(), base, &Config{}, nil)

	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		stagedBytes, rerr := afero.ReadFile(p.FS, p.Staged)
		require.NoError(t, rerr, "the staged copy holds the downloaded bytes at publish time")
		require.NoError(t, afero.WriteFile(p.FS, p.Dest, stagedBytes, 0o644),
			"the completed publish lands the staged bytes at dest")
		_ = p.FS.Remove(p.Staged) // the successful publish consumed the staged name
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		info, lerr := p.FS.Stat(p.Dest) // the verified published identity
		require.NoError(t, lerr)
		return w64FrozenFileInfo{FileInfo: info, size: info.Size(), modTime: info.ModTime()},
			fmt.Errorf("staged times for %s never applied — no fd-scoped times primitive on this platform and the name-based fallback is refused; the destination carries the published bytes: %w", p.Dest, fsutil.ErrPublishCompleted)
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	result, err := d.download(context.Background(), server.URL+"/poster.jpg", dest, MediaTypePoster, true)
	require.NoError(t, err, "the completed-with-identity publish is a success, not a failure")
	require.True(t, result.Downloaded, "the verified completed publish is recorded as a completed download")
	require.NoError(t, result.Error)
	require.False(t, result.Replaced, "create-path completed publish keeps replaced=false → dest enters CreatedPaths")
	require.True(t, result.producerIdentity.known, "the verified published identity rides the result")
	require.Equal(t, int64(len(payload)), result.producerIdentity.size)
	require.True(t, destStillHoldsInstalledObject(base, dest, result.producerIdentity),
		"the record names the landed destination object")
	require.Equal(t, payload, mustReadDownloaderW7(t, base, dest))
	require.Contains(t, logs.String(), "left in place", "the possibly-foreign staged name is warn-logged for manual cleanup")
}
