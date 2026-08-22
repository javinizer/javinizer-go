package downloader

// POSTER-WRITE-HARDENING wave-53 tests (codex P2, PR#215 finding 2 + finding 3)
// for promotePosterCandidateNoReplace: every outcome of the non-overwrite
// promote's validated-handle bound publish — clean success, collision (racer
// keeps the destination), publish-completed (candidate retained), proven
// substitution (candidate retained + typed refusal), the both-fail unprobeable
// refusal (finding 3), a plain publish failure, and the degraded recorded-only
// posture (no handle rides).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/stretchr/testify/require"

	"github.com/spf13/afero"
)

// nextStagedRepublishOrdinal mints strictly-increasing re-stage ordinals for
// the bound publish's proven-substitution recovery leg (wave-53). The leg
// itself runs only against a live directory writer mid-publish — no unit test
// triggers it end-to-end — so the minting is covered directly here.
func TestNextStagedRepublishOrdinalW53(t *testing.T) {
	a := nextStagedRepublishOrdinal()
	b := nextStagedRepublishOrdinal()
	require.Positive(t, a)
	require.Greater(t, b, a, "the ordinal is strictly monotonic across mints")
}

// w53StageCandidate writes a candidate body on OsFs and returns its path +
// the dest path (proven-absent — the promote publishes no-replace).
func w53StageCandidate(t *testing.T, body string) (fs afero.Fs, candidate, dest string) {
	t.Helper()
	base := afero.NewOsFs()
	dir := t.TempDir()
	candidate = filepath.Join(dir, "poster.jpg.crop.tmp")
	dest = filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(candidate, []byte(body), 0o644))
	return base, candidate, dest
}

func TestPromotePosterCandidateW53_BoundHandlePublishesCleanly(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest, captureInstalledDestIdentity(fs, candidate))
	require.NoError(t, err)
	require.Equal(t, promotePosterCandidateSucceeded, outcome)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "cropped candidate", string(got))
}

func TestPromotePosterCandidateW53_CollisionKeepsRacer(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	// Plant racer bytes at the destination before the promote publish.
	require.NoError(t, os.WriteFile(dest, []byte("racer"), 0o644))
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest, captureInstalledDestIdentity(fs, candidate))
	require.NoError(t, err, "a collision is the existing-artwork outcome, not a failure")
	require.Equal(t, promotePosterCandidateCollision, outcome)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "racer", string(got), "the racer's bytes are preserved")
}

func TestPromotePosterCandidateW53_PublishCompletedRetainsCandidate(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		// Consume the handle (the production seam always closes it).
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		return nil, errors.Join(fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest, captureInstalledDestIdentity(fs, candidate))
	require.NoError(t, err, "a completed-despite-error publish is a completed download")
	require.Equal(t, promotePosterCandidateCompleted, outcome)
	// The candidate is retained for manual cleanup (the wave-42 discipline).
	exists, _ := afero.Exists(fs, candidate)
	require.True(t, exists, "the possibly-foreign candidate name is retained")
}

func TestPromotePosterCandidateW53_PrePublishSubstitutionRefused(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		// The bound publish's pre-publish identity proof failed: the candidate
		// name no longer addresses the handle's inode (foreign substitution).
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		return nil, fsutil.ErrPublishStagedVerify
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest, captureInstalledDestIdentity(fs, candidate))
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.Equal(t, promotePosterCandidateRetained, outcome, "the substitute is preserved byte-intact")
	// Nothing was published onto the destination.
	_, derr := os.Lstat(dest)
	require.True(t, os.IsNotExist(derr), "no byte ever flowed into the destination")
}

func TestPromotePosterCandidateW53_BoundPublishIdentityBreakRefused(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		return nil, fsutil.ErrPublishStagedIdentityBreak
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest, captureInstalledDestIdentity(fs, candidate))
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.Equal(t, promotePosterCandidateRetained, outcome)
}

func TestPromotePosterCandidateW53_BothFailRefusesClosed(t *testing.T) {
	// A non-existent candidate: captureInstalledDestIdentity (Lstat) AND the
	// no-follow re-open both fail — the candidate is completely unprobeable.
	fs := afero.NewMemMapFs()
	outcome, err := promotePosterCandidateNoReplace(fs, "/missing-candidate", "/missing-dest", installedDestIdentity{})
	require.Error(t, err)
	require.ErrorIs(t, err, errCandidateProvenanceUnprobeable)
	require.Equal(t, promotePosterCandidateRetained, outcome)
}

func TestPromotePosterCandidateW53_PlainFailureSurfacesError(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	publishErr := errors.New("w53 promote publish wedged")
	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		return nil, publishErr
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest, captureInstalledDestIdentity(fs, candidate))
	require.Error(t, err)
	require.ErrorIs(t, err, publishErr)
	require.ErrorContains(t, err, "failed to finalize poster")
	require.Equal(t, promotePosterCandidateFailed, outcome, "a plain failure is provably ours — reaped, not retained")
}

// w53OpenFileFailFs fails OpenFile (the no-follow re-open fault) while
// leaving Lstat (captureInstalledDestIdentity) intact — the degrade posture
// where bindCandidateProvenance hands down a known identity but no handle.
type w53OpenFileFailFs struct {
	afero.Fs
}

func (f w53OpenFileFailFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	return nil, errors.New("w53 candidate open wedge")
}

func TestPromotePosterCandidateW53_DegradedRecordedOnlyPostureRefused(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	// Wrap so the promote's bindCandidateProvenance degrades: Lstat succeeds
	// (known identity) but the no-follow re-open fails (no handle). Wave-54
	// (finding 3) refuses typed instead of publishing by mutation of a merely-
	// recorded pathname — the candidate is preserved byte-intact, the
	// destination untouched (the re-acquire bind also fails the re-open).
	degraded := w53OpenFileFailFs{Fs: fs}
	outcome, err := promotePosterCandidateNoReplace(degraded, candidate, dest, captureInstalledDestIdentity(fs, candidate))
	require.ErrorIs(t, err, errStagedInputSubstituted, "the degraded leg re-acquires or refuses — never publishes by name")
	require.Equal(t, promotePosterCandidateRetained, outcome)
	_, rerr := os.ReadFile(dest)
	require.Error(t, rerr, "the destination is untouched — nothing published by name")
	got, _ := os.ReadFile(candidate)
	require.Equal(t, "cropped candidate", string(got), "the candidate is preserved byte-intact")
}

// TestDownloadPosterW53_PromoteSubstitutionRetainsCandidate covers the
// downloadPoster Retained case end-to-end: the promote's bound publish
// reports a proven substitution (errStagedInputSubstituted) and downloadPoster
// preserves the possibly-foreign candidate name for manual cleanup, surfaces
// the typed refusal, and leaves the destination untouched.
func TestDownloadPosterW53_PromoteSubstitutionRetainsCandidate(t *testing.T) {
	server := covW1BPosterServer()
	defer server.Close()

	base := afero.NewOsFs()
	destDir := t.TempDir()
	movie := w51PosterMovie("W53-SUB", server.URL+"/poster.jpg")
	dest := w51PosterDest(t, NewDownloader(nil, base, w51PosterConfig(), nil), movie, destDir)
	d := NewDownloader(server.Client(), base, w51PosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	prev := publishStagedBoundFn
	real := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		// Only the promote's bound publish (NoReplace=true) reports the proven
		// substitution; the full download's create install (NoReplace=false)
		// runs through the real bound publish unchanged.
		if p.NoReplace && p.Handle != nil {
			_ = p.Handle.Close() // the production seam always closes the handle
			return nil, fsutil.ErrPublishStagedVerify
		}
		return real(p)
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.Contains(t, logs.String(), "foreign substitution between crop and promote")

	// The candidate (crop.tmp) is retained for manual cleanup; the destination
	// is untouched (nothing was published onto it).
	entries, rerr := os.ReadDir(destDir)
	require.NoError(t, rerr)
	retained := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".crop.tmp") {
			retained = true
		}
	}
	require.True(t, retained, "the possibly-foreign candidate is retained for manual cleanup")
	_, derr := os.Lstat(dest)
	require.True(t, os.IsNotExist(derr), "no byte ever flowed into the destination")
}

// TestDownloadPosterW53_PromoteBindSubstitutionRetainsSubstitute drives
// downloadPoster's NON-overwrite promote Retained switch arm end-to-end
// through the bindCandidateProvenanceFn seam: a foreign substitute is
// planted onto the crop candidate name inside the crop→promote window and
// the promote's pre-publish provenance bind refuses it typed
// (errStagedInputSubstituted) → promotePosterCandidateRetained. Per the
// arm's comments, downloadPoster surfaces the refusal in fullResult.Error,
// clears LocalPath, reports Downloaded=false, retains the possibly-foreign
// candidate name byte-intact for manual cleanup (the stagedRetained gate
// keeps the deferred identity-bound scratch cleanup off the substitute),
// still reaps the non-candidate full-source scratch, and never publishes a
// byte onto the destination. No OverwriteExisting option is passed and the
// destination is proven absent — the legacy no-replace promote path.
func TestDownloadPosterW53_PromoteBindSubstitutionRetainsSubstitute(t *testing.T) {
	server := covW1BPosterServer()
	defer server.Close()

	base := afero.NewOsFs()
	destDir := t.TempDir()
	movie := w51PosterMovie("W53-BSUB", server.URL+"/poster.jpg")
	dest := w51PosterDest(t, NewDownloader(nil, base, w51PosterConfig(), nil), movie, destDir)
	d := NewDownloader(server.Client(), base, w51PosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	plant := []byte("w53 promote-window foreign substitute — planted post-crop, pre-bind")
	planted := false
	prev := bindCandidateProvenanceFn
	real := bindCandidateProvenanceFn
	bindCandidateProvenanceFn = func(fs afero.Fs, candidate string, producer installedDestIdentity) (stagedInstallProvenance, error) {
		if strings.HasSuffix(candidate, ".crop.tmp") {
			// A foreign writer rotated its bytes onto the candidate name after
			// the crop produced it; the bind refuses typed instead of
			// re-proving or publishing the substitute.
			if werr := afero.WriteFile(fs, candidate, plant, 0o600); werr == nil {
				planted = true
			}
			return stagedInstallProvenance{}, errStagedInputSubstituted
		}
		return real(fs, candidate, producer)
	}
	t.Cleanup(func() { bindCandidateProvenanceFn = prev })

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.ErrorIs(t, result.Error, errStagedInputSubstituted, "fullResult.Error carries the typed refusal")
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.False(t, result.Skipped)
	require.Empty(t, result.LocalPath)
	require.Contains(t, logs.String(), "substitute preserved")

	require.True(t, planted, "the seam planted the substitute at the promote-time bind")
	_, derr := os.Lstat(dest)
	require.True(t, os.IsNotExist(derr), "no byte ever flowed into the destination")

	// The stagedRetained lifecycle, observable: the retained crop.tmp
	// candidate carries the planted substitute byte-intact while the
	// non-candidate full.tmp scratch still reaps under its own identity
	// binding — only the possibly-foreign name is left for manual cleanup.
	entries, rerr := os.ReadDir(destDir)
	require.NoError(t, rerr)
	var retained []string
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".full.tmp", "the full-source scratch still reaps under its identity binding")
		if strings.Contains(e.Name(), ".crop.tmp") {
			retained = append(retained, e.Name())
		}
	}
	require.Len(t, retained, 1, "exactly the possibly-foreign candidate name is retained")
	got, gerr := os.ReadFile(filepath.Join(destDir, retained[0]))
	require.NoError(t, gerr)
	require.Equal(t, plant, got, "the substitute is preserved byte-intact (stagedRetained gated the deferred cleanup off it)")
}

// TestDownloadPosterW53_OverwriteBothFailRefusesClosed covers the overwrite
// install path's both-fail refusal (media.go: the candidate is unprobeable at
// bind time). Fail CLOSED: nothing published, the existing destination is
// untouched, and the candidate is retained for manual cleanup.
func TestDownloadPosterW53_OverwriteBothFailRefusesClosed(t *testing.T) {
	server := covW1BPosterServer()
	defer server.Close()

	base := afero.NewOsFs()
	destDir := t.TempDir()
	movie := w51PosterMovie("W53-OVF", server.URL+"/poster.jpg")
	dest := w51PosterDest(t, NewDownloader(nil, base, w51PosterConfig(), nil), movie, destDir)
	// Overwrite mode requires the destination to already exist.
	require.NoError(t, os.WriteFile(dest, []byte("existing artwork"), 0o644))
	d := NewDownloader(server.Client(), base, w51PosterConfig(), nil)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	prev := bindCandidateProvenanceFn
	bindCandidateProvenanceFn = func(_ afero.Fs, _ string, _ installedDestIdentity) (stagedInstallProvenance, error) {
		return stagedInstallProvenance{}, errCandidateProvenanceUnprobeable
	}
	t.Cleanup(func() { bindCandidateProvenanceFn = prev })

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil, true) // overwrite
	require.Error(t, err)
	require.ErrorIs(t, err, errCandidateProvenanceUnprobeable)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.Contains(t, logs.String(), "could not be proven")

	// The existing destination is untouched (fail closed — nothing published).
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "existing artwork", string(got), "the destination's existing bytes are preserved")
}
