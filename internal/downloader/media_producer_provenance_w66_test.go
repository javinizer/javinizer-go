package downloader

// POSTER-WRITE-HARDENING wave-66 (codex P2, PR#215 — bind the candidate to
// the PRODUCER'S identity). Pre-wave-66, bindCandidateProvenance's own
// captures (the install-time Lstat and the re-opened fd's fstat) were the
// FIRST identity ever taken of the mutable candidate name: a foreign
// substitute rotated onto it between the producer's write and the bind
// authenticated against ITSELF and was installed as ours. The producers now
// hand their write-time identity record down (downloadPoster's post-download
// capture of fullPath and post-crop capture of cropPath, in BOTH modes), the
// bind compares its Lstat/fstat captures against THAT record, and BOTH the
// overwrite install and the non-overwrite promote carry the same record —
// mismatch → typed refusal, substitute preserved byte-intact, install/refusal
// posture unchanged. The legs below: pre-bind substitution refusals
// end-to-end (overwrite path is the wave-47 (a) replay at fireOn 2, now
// refused at the bind; the non-overwrite promote leg is new here) and the
// bind's producer-record gate unit legs on OsFs (full kernel identity) and
// MemMapFs (name+size+mtime legs).

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// TestDownloadPosterW66_PromotePreBindSubstitutionRefused replays a foreign
// substitute rotated onto the crop candidate AFTER the crop/write completed
// and BEFORE the non-overwrite promote's provenance bind (fireOn 2: wave-66's
// producer capture at the crop is lookup #1, the promote's bind Lstat is #2).
// Pre-wave-66 the bind captured the substitute and it authenticated against
// itself; now the bind's Lstat≠producer-record check refuses typed: the
// substitute is preserved byte-intact at the retained candidate name, the
// destination never stored, and our own full-source scratch still reaps.
func TestDownloadPosterW66_PromotePreBindSubstitutionRefused(t *testing.T) {
	server := serveTwoToneSource(t)

	base := afero.NewMemMapFs()
	plant := []byte("w66 promote-window foreign substitute — planted post-crop, pre-bind")
	fsW := &w47CropSwapFs{Fs: base, fireOn: 2, plant: plant}
	movie := w42CropMovie("W66-PROMOTE", server.URL+"/cover.jpg")
	dest := w42ResolvePosterDest(NewDownloader(nil, base, w42CropPosterConfig(), nil), movie)
	d := NewDownloader(server.Client(), fsW, w42CropPosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	result, err := d.downloadPoster(context.Background(), movie, "/output", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.ErrorIs(t, result.Error, errStagedInputSubstituted)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.Empty(t, result.LocalPath)

	_, statErr := base.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the substitute must never reach the destination")

	require.True(t, fsW.swapped, "the pre-bind swap replay fired")
	require.Equal(t, plant, mustReadDownloaderW7(t, base, fsW.candidate),
		"the promote bind's producer-record gate kept the surviving name byte-intact")

	entries, readErr := afero.ReadDir(base, filepath.Dir(dest))
	require.NoError(t, readErr)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{filepath.Base(fsW.candidate), filepath.Base(fsW.candidate) + ".hidden"}, names,
		"full.tmp scratch reaped under its own identity binding; the retained candidate carries the substitute")

	// The parked genuine object is the cropped candidate the crop producer wrote.
	_, w, h := decodeResultPoster(t, base, fsW.candidate+".hidden")
	require.InDelta(t, 472, w, 3)
	require.Equal(t, 600, h)

	require.Contains(t, logs.String(), "no longer names the crop/write-produced object")
	require.Contains(t, logs.String(), "substitute preserved")
}

// TestBindCandidateProvenanceW66_ProducerRecordGate drives the bind's wave-66
// producer-record comparator directly: the OsFs legs carry full kernel
// identity (dev/ino + size + mtime), the MemMapFs legs the name+size(+mtime)
// pair, including the Lstat-wedged legs where the re-opened fd's fstat is the
// ONLY identity that can authenticate against the producer record.
func TestBindCandidateProvenanceW66_ProducerRecordGate(t *testing.T) {
	t.Run("producer record match binds on OsFs", func(t *testing.T) {
		fs := afero.NewOsFs()
		dir := t.TempDir()
		candidate := filepath.Join(dir, "poster.jpg.crop.tmp")
		require.NoError(t, os.WriteFile(candidate, []byte("cropped candidate"), 0o644))
		producer := captureInstalledDestIdentity(fs, candidate)
		require.True(t, producer.known)

		prov, err := bindCandidateProvenance(fs, candidate, producer)
		require.NoError(t, err)
		require.True(t, prov.identity.known)
		require.NotNil(t, prov.handle, "the matching producer record keeps the end-to-end handle binding")
		defer func() { _ = prov.handle.Close() }()
		require.Equal(t, producer.size, prov.identity.size)
	})

	// OsFs full-kernel-identity leg: the substitute is rotated onto the name
	// AFTER the producer record is captured — the bind's Lstat reads the
	// substitute, the producer record names a different object, refusal.
	t.Run("pre-bind substitute refused on OsFs, substitute preserved", func(t *testing.T) {
		fs := afero.NewOsFs()
		dir := t.TempDir()
		candidate := filepath.Join(dir, "poster.jpg.crop.tmp")
		require.NoError(t, os.WriteFile(candidate, []byte("cropped candidate"), 0o644))
		producer := captureInstalledDestIdentity(fs, candidate)
		require.True(t, producer.known)
		plant := []byte("w66 osfs pre-bind substitute of foreign length")
		require.NoError(t, os.Rename(candidate, candidate+".hidden"))
		require.NoError(t, os.WriteFile(candidate, plant, 0o644))

		prov, err := bindCandidateProvenance(fs, candidate, producer)
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.False(t, prov.identity.known, "nothing verifiable is handed down on the refusal")
		require.Nil(t, prov.handle, "the refusal fires BEFORE any handle is opened")
		got, rerr := os.ReadFile(candidate)
		require.NoError(t, rerr)
		require.Equal(t, plant, got, "the foreign substitute stays byte-intact for manual cleanup")
	})

	// MemMapFs name+size legs: no kernel identity, the size leg arbitrates.
	t.Run("producer record match binds on MemMapFs", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/candidate", []byte("cropped"), 0o644))
		producer := captureInstalledDestIdentity(fs, "/candidate")
		require.True(t, producer.known)
		require.False(t, producer.hasDevIno, "MemMapFs exposes no kernel identity — the name+size legs carry binding")

		prov, err := bindCandidateProvenance(fs, "/candidate", producer)
		require.NoError(t, err)
		require.True(t, prov.identity.known)
		require.NotNil(t, prov.handle)
		defer func() { _ = prov.handle.Close() }()
		require.Equal(t, producer.size, prov.identity.size)
	})

	t.Run("pre-bind substitute refused on MemMapFs, substitute preserved", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/candidate", []byte("cropped"), 0o644))
		producer := captureInstalledDestIdentity(fs, "/candidate")
		plant := []byte("w66 memfs pre-bind substitute — different length")
		require.NoError(t, fs.Rename("/candidate", "/candidate.hidden"))
		require.NoError(t, afero.WriteFile(fs, "/candidate", plant, 0o644))

		prov, err := bindCandidateProvenance(fs, "/candidate", producer)
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.False(t, prov.identity.known)
		require.Nil(t, prov.handle)
		got, rerr := afero.ReadFile(fs, "/candidate")
		require.NoError(t, rerr)
		require.Equal(t, plant, got, "the foreign substitute stays byte-intact for manual cleanup")
	})

	// The Lstat capture is wedged, so the wave-54 Lstat snapshot is unknown
	// and the re-opened fd's fstat is the ONLY identity that can authenticate
	// against the producer record: matching fstat binds, diverging fstat refuses.
	t.Run("Lstat wedged — fd fstat equal to the producer record binds", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/candidate", []byte("cropped"), 0o644))
		producer := captureInstalledDestIdentity(base, "/candidate")
		require.True(t, producer.known)
		fs := w57LstatFailFs{Fs: base}

		prov, err := bindCandidateProvenance(fs, "/candidate", producer)
		require.NoError(t, err, "a wedged Lstat degrades to the fd-fstat-vs-producer binding, never to unprovenanced")
		require.True(t, prov.identity.known)
		require.NotNil(t, prov.handle)
		defer func() { _ = prov.handle.Close() }()
		require.Equal(t, producer.size, prov.identity.size)
		require.True(t, prov.identity.modTime.Equal(producer.modTime))
	})

	t.Run("Lstat wedged — fd fstat diverging from the producer record refuses", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/candidate", []byte("cropped"), 0o644))
		producer := captureInstalledDestIdentity(base, "/candidate")
		require.True(t, producer.known)
		// w54SubstFs reports a FOREIGN size from the opened fd's Stat: the fd
		// provably does not name the producer-written object.
		fs := w57LstatFailFs{Fs: w54SubstFs{Fs: base}}

		prov, err := bindCandidateProvenance(fs, "/candidate", producer)
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.False(t, prov.identity.known)
		require.Nil(t, prov.handle, "the diverging fd is closed on the refusal")
	})
}

// TestPromotePosterCandidateW66_ProducerMismatchRefused drives the promote's
// producer record directly: the candidate is swapped AFTER the producer
// record is captured, so the promote's bind sees Lstat≠producer and refuses
// typed — the non-overwrite leg carries the same producer record as the
// overwrite install.
func TestPromotePosterCandidateW66_ProducerMismatchRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/candidate", []byte("cropped"), 0o644))
	producer := captureInstalledDestIdentity(fs, "/candidate")
	require.True(t, producer.known)
	// A foreign writer rotates the candidate name post-producer (the promote
	// leg's own bind must catch it — pre-wave-66 the substitute authenticated
	// against itself here).
	plant := []byte("w66 promote producer-mismatch substitute, longer than the crop")
	require.NoError(t, afero.WriteFile(fs, "/candidate", plant, 0o644))

	outcome, err := promotePosterCandidateNoReplace(fs, "/candidate", "/dest", producer)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.Equal(t, promotePosterCandidateRetained, outcome)
	got, rerr := afero.ReadFile(fs, "/candidate")
	require.NoError(t, rerr)
	require.Equal(t, plant, got, "the substitute is preserved byte-intact")
	_, derr := fs.Stat("/dest")
	require.ErrorIs(t, derr, os.ErrNotExist, "no byte ever flowed into the destination")
}
