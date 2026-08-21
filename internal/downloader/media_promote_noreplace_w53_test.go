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
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest)
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
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest)
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
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest)
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
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest)
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
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.Equal(t, promotePosterCandidateRetained, outcome)
}

func TestPromotePosterCandidateW53_BothFailRefusesClosed(t *testing.T) {
	// A non-existent candidate: captureInstalledDestIdentity (Lstat) AND the
	// no-follow re-open both fail — the candidate is completely unprobeable.
	fs := afero.NewMemMapFs()
	outcome, err := promotePosterCandidateNoReplace(fs, "/missing-candidate", "/missing-dest")
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
	outcome, err := promotePosterCandidateNoReplace(fs, candidate, dest)
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

func TestPromotePosterCandidateW53_DegradedRecordedOnlyPosturePublishesByName(t *testing.T) {
	fs, candidate, dest := w53StageCandidate(t, "cropped candidate")
	// Wrap so the promote's bindCandidateProvenance degrades: Lstat succeeds
	// (known identity) but the no-follow re-open fails (no handle) — the
	// wave-47 plain no-replace publish residual.
	degraded := w53OpenFileFailFs{Fs: fs}
	outcome, err := promotePosterCandidateNoReplace(degraded, candidate, dest)
	require.NoError(t, err)
	require.Equal(t, promotePosterCandidateSucceeded, outcome, "the degraded leg publishes by name into proven absence")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "cropped candidate", string(got))
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
	bindCandidateProvenanceFn = func(_ afero.Fs, candidate string) (stagedInstallProvenance, error) {
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
