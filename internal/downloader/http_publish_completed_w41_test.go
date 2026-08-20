package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-41 (P2) — "honor completed
// publishes before removing staging": when the create path's
// fsutil.PublishNoReplace returns an error wrapping fsutil.ErrPublishCompleted,
// the destination WAS published (the staged bytes ARE the media), while the
// staged name could not be re-proven — fsutil deliberately left it in place
// (fsutil.ErrPublishNoReplaceStagedUnverified: it may now address a FOREIGN
// occupant swapped on inside the link→cleanup window). download()'s install
// error branch used to unlink tempPath wholesale (destroying foreign bytes)
// and report the download as failed (dropping dest from CreatedPaths, so a
// later revert left the new media behind). The branch now classifies on
// fsutil.PublishCompleted FIRST: the completed class records exactly the
// success leg's accounting (Downloaded, Replaced=false → CreatedPaths), skips
// the staged removal, and warn-logs the retained name; every other error
// keeps the pre-wave-41 failure leg.

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

// (a) The staged-unverified class: dest is recorded exactly like the
// completed create (Downloaded, no Replaced → the CreatedPaths accounting in
// Download), tempPath is NEVER removed, the foreign occupant at tempPath
// survives byte-intact, and the retained-name posture is warn-logged.
func TestDownloadW41_PublishCompletedStagedUnverifiedRecordsCreatedAndKeepsForeignOccupant(t *testing.T) {
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
	require.NoError(t, err)
	require.True(t, result.Downloaded, "the completed publish is a success, not a failure")
	require.NoError(t, result.Error)
	require.False(t, result.Skipped)
	require.False(t, result.Replaced, "create-path completed publish keeps replaced=false → dest enters CreatedPaths")
	require.Equal(t, dest, result.LocalPath)
	require.Equal(t, int64(len(payload)), result.Size)

	require.Equal(t, payload, mustReadDownloaderW7(t, base, dest), "the destination verified present with the downloaded bytes")
	require.NotEmpty(t, fsW.staged, "the seam must have observed the staged name")
	_, statErr := base.Stat(fsW.staged)
	require.NoError(t, statErr, "tempPath was NOT removed")
	require.Equal(t, foreign, mustReadDownloaderW7(t, base, fsW.staged),
		"the foreign occupant at tempPath survives byte-intact — pre-wave-41 it was unlinked")
	require.Contains(t, logs.String(), "possibly foreign")
	require.Contains(t, logs.String(), "left in place")
}

// (b) The benign cleanup leg (plain ErrPublishCompleted with no unverified
// sentinel): identical CreatedPaths accounting.
func TestDownloadW41_PublishCompletedBenignRecordsCreated(t *testing.T) {
	payload := []byte("w41-cover-bytes")
	server := w41MediaServer(t, payload)

	base := afero.NewMemMapFs()
	dest := "/output/W41-BENIGN/cover.jpg"
	completedErr := fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed AND publish rollback failed: %w",
		dest+".staged", dest, fsutil.ErrPublishCompleted)
	fsW := &w41PublishCompletedRenameFs{Fs: base, dest: dest, err: completedErr}
	d := NewDownloader(server.Client(), fsW, &Config{}, nil)

	result, err := d.download(context.Background(), server.URL+"/cover.jpg", dest, MediaTypeCover, true)
	require.NoError(t, err)
	require.True(t, result.Downloaded)
	require.NoError(t, result.Error)
	require.False(t, result.Replaced, "replaced=false → dest enters CreatedPaths exactly like (a)")
	require.Equal(t, dest, result.LocalPath)
	require.Equal(t, int64(len(payload)), result.Size)
	require.Equal(t, payload, mustReadDownloaderW7(t, base, dest))
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
