package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-42 (P2) — wave-41's "honor
// completed publishes before removing staging" mirrored onto the
// cropped-poster install path (downloadPoster). With poster cropping active
// in overwrite mode and the destination ABSENT, the create leg of
// installOverwriting publishes the cropped candidate through
// fsutil.PublishNoReplace; its ErrPublishCompleted-carrying errors prove the
// destination WAS installed while the candidate NAME could not be re-proven
// (fsutil.ErrPublishNoReplaceStagedUnverified: a foreign occupant may now
// live at the candidate name and fsutil deliberately left it byte-intact).
// downloadPoster's install failure branch used to report the download as
// failed (Downloaded=false, LocalPath="" — the destination dropped from
// Download's CreatedPaths, so a later revert left the new media behind) and
// let the deferred crop/full scratch unlinks reap the candidate name —
// destroying the foreign occupant when the staged-unverified class replays.
// The branch now classifies on fsutil.PublishCompleted FIRST exactly like
// http.go's wave-41 leg: the completed class records exactly the success
// leg's accounting (Downloaded, Replaced=false → CreatedPaths), a
// stagedRetained gate keeps BOTH deferred scratch unlinks off the candidate
// name (the candidate is cropPath when a crop applied, fullPath when it did
// not; the non-candidate scratch still cleans as before), and the retained
// name is warn-logged; every other error keeps the pre-wave-42 failure leg.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// w42CropPosterConfig mirrors the geometry-suite config; ShouldCropPoster=true
// on the movie pushes the flow through CropPosterFromCover so the install
// candidate is the crop scratch.
func w42CropPosterConfig() *Config {
	return &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
}

// w42ResolvePosterDest resolves the poster destination through the SAME
// resolver the flow uses, so the seam fs's Rename hook binds to the real
// destination name before any download runs.
func w42ResolvePosterDest(d *Downloader, movie *models.Movie) string {
	return d.pathResolver.ResolvePosterPath(movie, nil, true, d.buildTemplateContext(movie, nil), "/output")
}

func w42CropMovie(id, url string) *models.Movie {
	return &models.Movie{ID: id, Poster: models.PosterState{PosterURL: url, ShouldCropPoster: true}}
}

// (a) The staged-unverified class with a FOREIGN PLANT at the candidate name:
// the destination is recorded exactly like the completed create (visible at
// Download's CreatedPaths accounting), the retained candidate name keeps the
// foreign occupant byte-intact, and the retained-name posture is warn-logged.
func TestDownloadPosterW42_PublishCompletedStagedUnverifiedRecordsCreatedAndKeepsForeignOccupant(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	foreign := []byte("foreign-arbitrary-occupant")
	joined := errors.Join(fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	fsW := &w41PublishCompletedRenameFs{Fs: base, err: joined, occupant: foreign}
	movie := w42CropMovie("W42-STAGED", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	fsW.dest = dest
	d := NewDownloader(server.Client(), fsW, w42CropPosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	// Assert at the revert-facing accounting level: the completed publish is
	// indistinguishable from the success leg — dest enters CreatedPaths, so a
	// later revert leaves the new media behind instead of deleting it.
	outcome, err := d.Download(context.Background(), DownloadCmd{Movie: movie, DestDir: "/output", OverwriteExistingMedia: true})
	require.NoError(t, err)
	require.Contains(t, outcome.CreatedPaths, dest, "CompletedPaths must record the completed publish as created")

	var result DownloadResult
	found := false
	for _, r := range outcome.Results {
		if r.Type == MediaTypePoster {
			result, found = r, true
		}
	}
	require.True(t, found, "a poster result is produced")
	require.True(t, result.Downloaded, "the completed publish is a success, not a failure")
	require.NoError(t, result.Error)
	require.False(t, result.Skipped)
	require.False(t, result.Replaced, "cropped create-path completed publish keeps replaced=false → CreatedPaths")
	require.Equal(t, dest, result.LocalPath)
	require.Greater(t, result.Size, int64(0), "finalizePosterResult accounted the published destination")

	// The destination carries the CROPPED candidate bytes (the auto-crop
	// keeps the right ~47.2% of the 1000px source), never the full download.
	_, w, h := decodeResultPoster(t, base, dest)
	require.InDelta(t, 472, w, 3, "the published bytes are the cropped candidate, not the full source")
	require.Equal(t, 600, h)

	require.NotEmpty(t, fsW.staged, "the seam must have observed the candidate name")
	_, statErr := base.Stat(fsW.staged)
	require.NoError(t, statErr, "the candidate name was NOT removed")
	require.Equal(t, foreign, mustReadDownloaderW7(t, base, fsW.staged),
		"the foreign occupant at the candidate name survives byte-intact — pre-wave-42 it was unlinked")

	// WHICH deferred cleanup runs: the full-source scratch is still reaped
	// (its name is provably ours); only the gated candidate name remains.
	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{filepath.Base(dest), filepath.Base(fsW.staged)}, names,
		"full.tmp scratch reaped as before; only the retained candidate name stays")

	require.Contains(t, logs.String(), "possibly foreign")
	require.Contains(t, logs.String(), "left in place")
}

// (b) The benign cleanup leg (plain ErrPublishCompleted with no unverified
// sentinel): identical CreatedPaths accounting, and no retained name — the
// virtual rename already vacated the candidate, so the directory holds the
// destination alone.
func TestDownloadPosterW42_PublishCompletedBenignRecordsCreated(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	fsW := &w41PublishCompletedRenameFs{Fs: base}
	movie := w42CropMovie("W42-BENIGN", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	fsW.dest = dest
	fsW.err = fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed AND publish rollback failed: %w",
		dest+".staged", dest, fsutil.ErrPublishCompleted)
	d := NewDownloader(server.Client(), fsW, w42CropPosterConfig(), nil)

	result, err := d.downloadPoster(context.Background(), movie, "/output", nil, true)
	require.NoError(t, err)
	require.True(t, result.Downloaded)
	require.NoError(t, result.Error)
	require.False(t, result.Replaced, "replaced=false → dest enters CreatedPaths exactly like (a)")
	require.Equal(t, dest, result.LocalPath)
	require.Greater(t, result.Size, int64(0))

	_, w, _ := decodeResultPoster(t, base, dest)
	require.InDelta(t, 472, w, 3, "the published bytes are the cropped candidate")

	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "the benign leg leaves NO retained name — candidate vacated by the rename, full scratch reaped")
	require.Equal(t, filepath.Base(dest), entries[0].Name())
}

// (c) A plain install failure (nothing published, no ErrPublishCompleted)
// keeps the pre-wave-42 leg: error surfaced, BOTH scratch names reaped, dest
// absent — the completed-only gate never engages. Run on the real OsFs so
// the wave-65 identity probe matches (a read-only fd's close never re-stamps
// the candidate the way afero's mem handle does) and both scratches reap
// exactly as before; the MemMapFs re-stamp retain leg is covered by the
// cov-w1b install-error test.
func TestDownloadPosterW42_PlainInstallFailureStillReapsBothScratchNames(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewOsFs()
	destDir := t.TempDir()
	sentinel := errors.New("w42 publish wedged before landing")
	fsW := &w15PublishWedgeFs{Fs: base}
	movie := w42CropMovie("W42-PLAIN", server.URL+"/cover.jpg")
	rd := NewDownloader(nil, base, w42CropPosterConfig(), nil)
	dest := rd.pathResolver.ResolvePosterPath(movie, nil, true, rd.buildTemplateContext(movie, nil), destDir)
	fsW.dest = dest
	fsW.err = sentinel
	d := NewDownloader(server.Client(), fsW, w42CropPosterConfig(), nil)

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil, true)
	require.ErrorIs(t, err, sentinel)
	require.False(t, result.Downloaded)
	require.ErrorIs(t, result.Error, sentinel)
	require.Empty(t, result.LocalPath)

	_, statErr := base.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "nothing was published")
	entries, readErr := os.ReadDir(destDir)
	require.NoError(t, readErr)
	require.Empty(t, entries, "plain failures keep the prior leg: full AND crop scratch names are reaped")
}
