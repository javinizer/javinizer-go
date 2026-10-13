package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// codex P1 (PRRT_kwDORn9KaM6m_lgp): a plan carrying admission proofs installs
// each bound subtitle source identity-bound through the verified no-replace
// twin; unbound plans keep the legacy by-name composite.

var errAdmissionProofRefused = errors.New("admission proof refused the object")

func refusingSubtitleProof(string, os.FileInfo) error { return errAdmissionProofRefused }

func acceptingSubtitleProof(string, os.FileInfo) error { return nil }

func verifiedSubtitleTestPlan() *OrganizePlan {
	return &OrganizePlan{
		Match:      models.FileMatchInfo{MovieID: "ABC-123", Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4"},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
	}
}

// A bound source whose proof refuses the object is NOT installed — move or
// copy — and the seat carries the typed admission refusal (never one of the
// consumption classes). The object at the source name survives byte-intact.
// The unbound contrast on the identical post-refusal state proves the binding
// (not the composite) decided the outcome: by-name consumption proceeds.
func TestHandleSubtitles_BoundProofRefusalPreventsByNameConsumption(t *testing.T) {
	lanes := map[string]subtitleInstall{
		"move": subtitleMoveInstall,
		"copy": subtitleCopyInstall,
	}
	for name, lane := range lanes {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			org := NewOrganizer(fs, &Config{MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, nil, nil)
			source := "/source/ABC-123.srt"
			dest := "/dest/ABC-123/ABC-123.srt"
			require.NoError(t, fs.MkdirAll("/source", 0o777))
			require.NoError(t, afero.WriteFile(fs, source, []byte("admitted subtitle"), 0o644))
			require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0o777))

			plan := verifiedSubtitleTestPlan()
			plan.BindVerifiedSubtitleSources(map[string]fsutil.VerifiedSourceProof{filepath.Clean(source): refusingSubtitleProof})
			result := &OrganizeResult{}
			org.handleSubtitles(plan, result, lane)

			require.Len(t, result.Subtitles, 1)
			seat := result.Subtitles[0]
			require.Error(t, seat.Error)
			assert.True(t, errors.Is(seat.Error, errAdmissionProofRefused), "the proof error rides: %v", seat.Error)
			assert.True(t, errors.Is(seat.Error, fsutil.ErrTakeAsideForeign), "typed admission refusal: %v", seat.Error)
			assert.False(t, seat.Moved || seat.Copied || seat.Skipped, "a refusal is none of the consumption classes")
			exists, err := afero.Exists(fs, dest)
			require.NoError(t, err)
			assert.False(t, exists, "nothing published under a refused proof")
			got, err := afero.ReadFile(fs, source)
			require.NoError(t, err)
			assert.Equal(t, "admitted subtitle", string(got), "the object rides back / is never consumed")

			legacy := verifiedSubtitleTestPlan()
			legacyResult := &OrganizeResult{}
			org.handleSubtitles(legacy, legacyResult, lane)
			require.Len(t, legacyResult.Subtitles, 1)
			require.NoError(t, legacyResult.Subtitles[0].Error)
			got, err = afero.ReadFile(fs, dest)
			require.NoError(t, err, "the unbound by-name lane consumes the same object")
			assert.Equal(t, "admitted subtitle", string(got))
			if lane.copied {
				assert.True(t, legacyResult.Subtitles[0].Copied)
				exists, err = afero.Exists(fs, source)
				require.NoError(t, err)
				assert.True(t, exists, "copy retains the source")
			} else {
				assert.True(t, legacyResult.Subtitles[0].Moved)
			}
		})
	}
}

// A bound source whose proof accepts the object installs through the verified
// composite with the ordinary lane outcomes (Moved/Copied), the destination
// receiving exactly the object the proof admitted.
func TestHandleSubtitles_BoundProofAcceptanceDeliversAdmittedBytes(t *testing.T) {
	lanes := map[string]subtitleInstall{
		"move": subtitleMoveInstall,
		"copy": subtitleCopyInstall,
	}
	for name, lane := range lanes {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			org := NewOrganizer(fs, &Config{MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, nil, nil)
			source := "/source/ABC-123.srt"
			dest := "/dest/ABC-123/ABC-123.srt"
			require.NoError(t, fs.MkdirAll("/source", 0o777))
			require.NoError(t, afero.WriteFile(fs, source, []byte("admitted subtitle"), 0o644))
			require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0o777))

			plan := verifiedSubtitleTestPlan()
			plan.BindVerifiedSubtitleSources(map[string]fsutil.VerifiedSourceProof{filepath.Clean(source): acceptingSubtitleProof})
			result := &OrganizeResult{}
			org.handleSubtitles(plan, result, lane)

			require.Len(t, result.Subtitles, 1)
			require.NoError(t, result.Subtitles[0].Error)
			got, err := afero.ReadFile(fs, dest)
			require.NoError(t, err)
			assert.Equal(t, "admitted subtitle", string(got))
			if lane.copied {
				assert.True(t, result.Subtitles[0].Copied)
				exists, statErr := afero.Exists(fs, source)
				require.NoError(t, statErr)
				assert.True(t, exists, "copy retains the source")
			} else {
				assert.True(t, result.Subtitles[0].Moved)
				exists, statErr := afero.Exists(fs, source)
				require.NoError(t, statErr)
				assert.False(t, exists, "move consumes the source")
			}
		})
	}
}

func TestOrganizePlanBindVerifiedSubtitleSourcesSetterSemantics(t *testing.T) {
	plan := &OrganizePlan{}
	plan.BindVerifiedSubtitleSources(nil)
	assert.Nil(t, plan.verifiedSubtitleProofs, "a nil bind leaves the plan unbound")

	plan.BindVerifiedSubtitleSources(map[string]fsutil.VerifiedSourceProof{"/a.srt": refusingSubtitleProof})
	require.Len(t, plan.verifiedSubtitleProofs, 1)
	assert.NotNil(t, plan.verifiedSubtitleProofs["/a.srt"])

	plan.BindVerifiedSubtitleSources(map[string]fsutil.VerifiedSourceProof{"/b.srt": acceptingSubtitleProof})
	require.Len(t, plan.verifiedSubtitleProofs, 1, "setter semantics mirror BindVerifiedSource — a rebind REPLACES (the fenced flow binds each freshly replanned plan once)")
	assert.NotNil(t, plan.verifiedSubtitleProofs["/b.srt"])
}

// codex P1 (PRRT_kwDORn9KaM6qLkJ4): the verified move subtitle lane carries
// the publish identity just like the copy lane, so deferred rollback arms do
// not have to re-adopt the destination by name.
func TestSubtitleMoveInstallReturnsBoundIdentity(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "movie.en.srt")
	dst := filepath.Join(dir, "library", "movie.en.srt")
	require.NoError(t, afero.WriteFile(fs, src, []byte("subtitle"), 0o640))

	identity, err := subtitleMoveInstall.run(fs, src, dst, acceptingSubtitleProof)
	require.NoError(t, err)
	require.NotNil(t, identity)
	info, statErr := os.Lstat(dst)
	require.NoError(t, statErr)
	// Assert through the production observation predicate instead of os.SameFile:
	// a wrapper FileInfo silently compares unequal on some toolchains (observed
	// on Windows/Go 1.26), while the observation failure mode is refusal, which
	// this lane would have surfaced at production time as the install leg's
	// identity not proving out. same-object → adopted as the operation's own.
	observed, oerr := fsutil.ObserveVerifiedInstall(fs, dst, identity)
	require.NoError(t, oerr)
	require.NotNil(t, observed, "the moved subtitle proves back to the destination's current entry")
	obsInfo, obsErr := os.Lstat(dst)
	require.NoError(t, obsErr)
	assert.Equal(t, info.Size(), obsInfo.Size())
	assert.Equal(t, info.ModTime(), obsInfo.ModTime())
}
