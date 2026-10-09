package organizer

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Organizer-side contract for codex P2 (PRRT_kwDORn9KaM6m7CBR): two source
// subtitles whose language suffixes normalize onto one destination name
// (.en.srt and .eng.srt both become <target>.eng.srt) install first-wins —
// directory order enumerates them alphabetically, the FIRST source installs,
// and every later duplicate reports Skipped. PlanSubtitleMoves still
// enumerates BOTH planned endpoints; the artifact staging arming loop dedupes
// by normalized target and pins the FIRST planned source, mirroring this.
func TestHandleSubtitles_DuplicateNormalizedEndpointsFirstWins(t *testing.T) {
	srcDir := filepath.Join("/", "source")
	srcFirst := filepath.Join(srcDir, "ABC-123.en.srt")
	srcSecond := filepath.Join(srcDir, "ABC-123.eng.srt")
	targetDir := filepath.Join("/", "dest", "ABC-123")
	newPlan := func() *OrganizePlan {
		return &OrganizePlan{
			Match: models.FileMatchInfo{
				MovieID: "ABC-123",
				Path:    filepath.Join(srcDir, "ABC-123.mp4"), Name: "ABC-123.mp4", Extension: ".mp4",
			},
			TargetDir:  targetDir,
			TargetFile: "ABC-123.mp4",
			TargetPath: filepath.Join(targetDir, "ABC-123.mp4"),
		}
	}
	newFS := func(t *testing.T) afero.Fs {
		t.Helper()
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(srcDir, 0o777))
		require.NoError(t, afero.WriteFile(fs, srcFirst, []byte("first english"), 0o644))
		require.NoError(t, afero.WriteFile(fs, srcSecond, []byte("second english"), 0o644))
		return fs
	}
	cfg := &Config{MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}
	endpoint := filepath.Join(targetDir, "ABC-123.eng.srt")

	t.Run("copy install copies the first and skips the duplicate", func(t *testing.T) {
		fs := newFS(t)
		org := NewOrganizer(fs, cfg, nil, nil)
		result := &OrganizeResult{}
		org.handleSubtitles(newPlan(), result, subtitleCopyInstall)

		require.Len(t, result.Subtitles, 2)
		assert.True(t, result.Subtitles[0].Copied)
		assert.Equal(t, srcFirst, result.Subtitles[0].OriginalPath)
		assert.Equal(t, endpoint, result.Subtitles[0].NewPath)
		assert.True(t, result.Subtitles[1].Skipped, "the duplicate endpoint seat skips (the first install occupies it)")
		assert.Equal(t, srcSecond, result.Subtitles[1].OriginalPath)
		assert.Equal(t, endpoint, result.Subtitles[1].NewPath)
		bytes, err := afero.ReadFile(fs, endpoint)
		require.NoError(t, err)
		assert.Equal(t, "first english", string(bytes), "the alphabetically-first source wins the endpoint")
		for _, src := range []string{srcFirst, srcSecond} {
			exists, err := afero.Exists(fs, src)
			require.NoError(t, err)
			assert.True(t, exists, "copy install retains every source: %s", src)
		}
	})

	t.Run("move install moves the first and skips the duplicate", func(t *testing.T) {
		fs := newFS(t)
		org := NewOrganizer(fs, cfg, nil, nil)
		result := &OrganizeResult{}
		org.handleSubtitles(newPlan(), result, subtitleMoveInstall)

		require.Len(t, result.Subtitles, 2)
		assert.True(t, result.Subtitles[0].Moved)
		assert.True(t, result.Subtitles[1].Skipped)
		bytes, err := afero.ReadFile(fs, endpoint)
		require.NoError(t, err)
		assert.Equal(t, "first english", string(bytes))
		firstGone, err := afero.Exists(fs, srcFirst)
		require.NoError(t, err)
		assert.False(t, firstGone, "the winning source moved")
		secondRetained, err := afero.Exists(fs, srcSecond)
		require.NoError(t, err)
		assert.True(t, secondRetained, "the skipped duplicate source stays put")
	})

	t.Run("plan probe enumerates both endpoints unmutated", func(t *testing.T) {
		fs := newFS(t)
		org := NewOrganizer(fs, cfg, nil, nil)
		moves := org.PlanSubtitleMoves(newPlan())
		require.Len(t, moves, 2, "the nil-install probe enumerates BOTH duplicate endpoints — dedupe is the arming lane's job")
		assert.Equal(t, srcFirst, moves[0].OriginalPath)
		assert.Equal(t, srcSecond, moves[1].OriginalPath)
		assert.Equal(t, endpoint, moves[0].NewPath)
		assert.Equal(t, endpoint, moves[1].NewPath)
		bytes, err := afero.ReadFile(fs, srcFirst)
		require.NoError(t, err)
		assert.Equal(t, "first english", string(bytes), "the probe never mutates the sources")
	})
}

// Winner-refusal half of codex P2 (PRRT_kwDORn9KaM6nmSaI): two sources share
// one normalized endpoint, and the FIRST-planned source's install FAILS before
// publishing — here its bound admission proof refuses the object at the
// consume, the same shape as a rename-swap detected after the pre-execution
// gate. The pre-execution journal dedupes the endpoint to the first source
// only (the move lane's pending intent names it, the copy lane's armed pin
// carries its digest), so the second duplicate must NEVER install into the
// still-vacant endpoint: its move would leave no journaled inverse for crash
// recovery to reconcile, and its copy bytes could never authenticate against
// the first source's hash pin. Strict first-wins holds: the duplicate skips,
// the endpoint stays vacant, and both sources survive byte-intact — the
// first-source journal entry settles the endpoint on its own.
func TestHandleSubtitles_DuplicateBlockedAfterWinnerRefusal(t *testing.T) {
	lanes := map[string]subtitleInstall{
		"move": subtitleMoveInstall,
		"copy": subtitleCopyInstall,
	}
	for name, lane := range lanes {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			org := NewOrganizer(fs, &Config{MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, nil, nil)
			// Host-form path literals: handleSubtitles enumerates sources through
			// the host-aware filepath package, so on Windows the seats report
			// OriginalPath as `\source\…` and a posix literal never string-equals it.
			srcDir := filepath.Join("/", "source")
			srcFirst := filepath.Join(srcDir, "ABC-123.en.srt")
			srcSecond := filepath.Join(srcDir, "ABC-123.eng.srt")
			destDir := filepath.Join("/", "dest", "ABC-123")
			dest := filepath.Join(destDir, "ABC-123.eng.srt")
			require.NoError(t, fs.MkdirAll(srcDir, 0o777))
			require.NoError(t, afero.WriteFile(fs, srcFirst, []byte("first english"), 0o644))
			require.NoError(t, afero.WriteFile(fs, srcSecond, []byte("second english"), 0o644))
			require.NoError(t, fs.MkdirAll(destDir, 0o777))

			plan := verifiedSubtitleTestPlan()
			plan.BindVerifiedSubtitleSources(map[string]fsutil.VerifiedSourceProof{
				filepath.Clean(srcFirst):  refusingSubtitleProof,
				filepath.Clean(srcSecond): acceptingSubtitleProof,
			})
			result := &OrganizeResult{}
			org.handleSubtitles(plan, result, lane)

			require.Len(t, result.Subtitles, 2)
			winner := result.Subtitles[0]
			require.Equal(t, srcFirst, winner.OriginalPath)
			require.Error(t, winner.Error)
			assert.True(t, errors.Is(winner.Error, errAdmissionProofRefused), "the proof error rides: %v", winner.Error)
			assert.True(t, errors.Is(winner.Error, fsutil.ErrTakeAsideForeign), "typed admission refusal: %v", winner.Error)
			assert.False(t, winner.Moved || winner.Copied || winner.Skipped, "a refusal is none of the consumption classes")

			dup := result.Subtitles[1]
			require.Equal(t, srcSecond, dup.OriginalPath)
			assert.True(t, dup.Skipped, "strict first-wins blocks the duplicate once the winner attempted the endpoint")
			assert.NoError(t, dup.Error)
			assert.False(t, dup.Moved || dup.Copied, "the duplicate NEVER installs after the winner failed before publishing")

			exists, err := afero.Exists(fs, dest)
			require.NoError(t, err)
			assert.False(t, exists, "the still-vacant endpoint stays vacant — no unjournaled install can become the winner")
			for path, want := range map[string]string{srcFirst: "first english", srcSecond: "second english"} {
				got, readErr := afero.ReadFile(fs, path)
				require.NoError(t, readErr)
				assert.Equal(t, want, string(got), "source retained byte-intact: %s", path)
			}
		})
	}
}
