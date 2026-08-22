package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-41 (P2) — "honor completed
// publishes before removing staging": when the create path's
// fsutil.PublishNoReplace returns an error wrapping fsutil.ErrPublishCompleted,
// the destination WAS published (the staged bytes ARE the media), while the
// staged name could not be re-proven — fsutil deliberately left it in place
// (fsutil.ErrPublishNoReplaceStagedUnverified: it may now address a FOREIGN
// occupant swapped on inside the link→cleanup window). download()'s install
// error branch used to unlink tempPath wholesale (destroying foreign bytes)
// and report the download as failed.
//
// Wave-68 (codex P2, PR#215 F2) deepened wave-41's posture: the completed
// leg must CERTIFY the publish through a verified published identity (the
// record the bound publish hands back alongside ErrPublishCompleted — waves
// 61/62, the ENOSYS-times-skipped leg). When that identity is genuinely
// unavailable (virtual-fs posture, or ErrPublishCompleted without a dest
// info binding — published == nil) the publish completed but its provenance
// CANNOT be certified, so a foreign temp replacement would ride
// publish-as-poster against an unknown record (downstream skips the producer
// gates). The leg now REFUSES to certify an unproven publish instead of
// recording it as success: nothing certified (Downloaded=false, no
// producerIdentity), tempPath preserved byte-intact (possibly foreign), the
// completed error surfaces. The completed-WITH-identity success leg is pinned
// by TestDownloadW68_PublishCompletedWithIdentityFilesProducerRecord.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// w41PublishCompletedRenameFs replays the POSIX hard-link fallback's
// completed-despite-error publish outcome on a virtual filesystem: the rename
// to dest PUBLISHES for real (like link(2) landing the staged inode at the
// destination — the destination verified present), the staged name is then
// reoccupied by the injected foreign occupant when one is set, and the typed
// ErrPublishCompleted-carrying error is returned. With occupant==nil it
// replays wave-20's benign cleanup leg (the staged name simply unreaped —
// already vacated by the virtual rename).
type w41PublishCompletedRenameFs struct {
	afero.Fs
	dest     string
	err      error
	occupant []byte
	staged   string
}

func (f *w41PublishCompletedRenameFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		f.staged = oldname
		if err := f.Fs.Rename(oldname, newname); err != nil {
			return err
		}
		if f.occupant != nil {
			if err := afero.WriteFile(f.Fs, oldname, f.occupant, 0o644); err != nil {
				return err
			}
		}
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

func w41MediaServer(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	return server
}

// (a) The staged-unverified class on a virtual filesystem (no verified
// publish identity — published == nil): the completed publish is REFUSED —
// Downloaded=false, no producerIdentity certified — tempPath is NEVER removed
// (it may address a foreign occupant), the foreign occupant at tempPath
// survives byte-intact, and the refusal is warn-logged. The destination WAS
// published (the staged bytes landed) but cannot be certified without a
// verified identity, so the completed error surfaces.
func TestDownloadW41_PublishCompletedStagedUnverifiedRefusedAndKeepsForeignOccupant(t *testing.T) {
	payload := []byte("w41-poster-bytes")
	server := w41MediaServer(t, payload)

	base := afero.NewMemMapFs()
	dest := "/output/W41-STAGED/poster.jpg"
	foreign := []byte("foreign-arbitrary-occupant")
	joined := errors.Join(fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	fsW := &w41PublishCompletedRenameFs{Fs: base, dest: dest, err: joined, occupant: foreign}
	d := NewDownloader(server.Client(), fsW, &Config{}, nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	result, err := d.download(context.Background(), server.URL+"/poster.jpg", dest, MediaTypePoster, true)
	require.Error(t, err, "an unproven completed publish is refused, not certified as success")
	require.ErrorIs(t, err, fsutil.ErrPublishCompleted, "the completed error surfaces verbatim")
	require.False(t, result.Downloaded, "the unproven publish is NOT recorded as a completed download")
	require.False(t, result.producerIdentity.known, "no producerIdentity is certified without a verified publish identity")
	require.False(t, result.Skipped)
	require.False(t, result.Replaced, "dest never enters CreatedPaths for an unproven publish")

	require.Equal(t, payload, mustReadDownloaderW7(t, base, dest), "the destination was published (cannot be undone) — the refusal certifies nothing, it does not revert bytes")
	require.NotEmpty(t, fsW.staged, "the seam must have observed the staged name")
	_, statErr := base.Stat(fsW.staged)
	require.NoError(t, statErr, "tempPath was NOT removed — it may address a foreign occupant")
	require.Equal(t, foreign, mustReadDownloaderW7(t, base, fsW.staged),
		"the foreign occupant at tempPath survives byte-intact")
	require.Contains(t, logs.String(), "refusing to certify")
	require.Contains(t, logs.String(), "left in place")
}

// (b) The benign cleanup leg (plain ErrPublishCompleted with no unverified
// sentinel) on a virtual filesystem: identical refusal posture — no verified
// identity, so the completed publish is refused, not certified.
func TestDownloadW41_PublishCompletedBenignRefused(t *testing.T) {
	payload := []byte("w41-cover-bytes")
	server := w41MediaServer(t, payload)

	base := afero.NewMemMapFs()
	dest := "/output/W41-BENIGN/cover.jpg"
	completedErr := fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed AND publish rollback failed: %w",
		dest+".staged", dest, fsutil.ErrPublishCompleted)
	fsW := &w41PublishCompletedRenameFs{Fs: base, dest: dest, err: completedErr}
	d := NewDownloader(server.Client(), fsW, &Config{}, nil)

	result, err := d.download(context.Background(), server.URL+"/cover.jpg", dest, MediaTypeCover, true)
	require.Error(t, err, "an unproven completed publish is refused")
	require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
	require.False(t, result.Downloaded, "replaced=false → dest never enters CreatedPaths for an unproven publish")
	require.False(t, result.producerIdentity.known)
	require.Equal(t, payload, mustReadDownloaderW7(t, base, dest), "the destination was published — the refusal certifies nothing")
}

// (c) A plain install failure (nothing published, no ErrPublishCompleted)
// keeps the pre-wave-41 leg: error surfaced, tempPath removed, dest absent.
func TestDownloadW41_PlainInstallFailureStillCleansStagedCopy(t *testing.T) {
	payload := []byte("w41-wedged-bytes")
	server := w41MediaServer(t, payload)

	base := afero.NewMemMapFs()
	dest := "/output/W41-PLAIN/poster.jpg"
	sentinel := errors.New("w41 publish wedged before landing")
	fsW := &w15PublishWedgeFs{Fs: base, dest: dest, err: sentinel}
	d := NewDownloader(server.Client(), fsW, &Config{}, nil)

	result, err := d.download(context.Background(), server.URL+"/poster.jpg", dest, MediaTypePoster, true)
	require.ErrorIs(t, err, sentinel)
	require.False(t, result.Downloaded)
	require.ErrorIs(t, result.Error, sentinel)

	_, statErr := base.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "nothing was published")
	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	require.Empty(t, entries, "the staged tempPath is removed exactly like before")
}

// Guard: the typed refusal classes never route into the completed success
// leg — the classifier reads ErrPublishCompleted only, and the refusal legs
// themselves are pinned end-to-end by the wave-15 suite.
func TestDownloadW41_PublishCompletedClassifierStaysDisjointFromRefusals(t *testing.T) {
	require.True(t, fsutil.PublishCompleted(errors.Join(fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)))
	require.True(t, fsutil.PublishCompleted(fmt.Errorf("cleanup failed: %w", fsutil.ErrPublishCompleted)))
	require.False(t, fsutil.PublishCompleted(fsutil.ErrPublishCollision))
	require.False(t, fsutil.PublishCompleted(fsutil.ErrPublishNoReplaceUnsupported))
}
