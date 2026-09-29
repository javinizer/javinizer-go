package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P1 (PRRT_kwDORn9KaM6m3ujF): a deferred copy whose subtitle destination
// became occupied with DIFFERENT bytes after preflight gets a Skipped outcome
// from the organizer lane. The staged sibling copy must stay OUT of the
// install set: the occupant survives the apply byte-for-byte, the retracted
// pin never graduates, the rest still publishes, and a revert keeps the
// occupant untouched.
func TestDeferredCopyOccupiedSkipNeverOverwritesOccupant(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-occupied-skip")
	probePlan, planErr := env.org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, planErr)
	subMoves := env.org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	subTarget := subMoves[0].NewPath

	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, preExecute: func(*organizer.OrganizePlan) {
		// The foreign occupant lands after the pin/arm and before execution.
		require.NoError(t, env.fs.MkdirAll(filepath.Dir(subTarget), 0o755))
		require.NoError(t, afero.WriteFile(env.fs, subTarget, []byte("foreign-subtitle-bytes"), 0o644))
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	skipped := false
	for _, sr := range result.OrganizeResult.Subtitles {
		if sr.Skipped && filepath.Clean(sr.NewPath) == filepath.Clean(subTarget) {
			skipped = true
		}
	}
	require.True(t, skipped, "the foreign occupant forced the organizer's skip outcome")

	occupant, occErr := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, occErr, "the organizer-skipped destination was never republished through the tree")
	assert.Equal(t, "foreign-subtitle-bytes", string(occupant), "the foreign occupant survives the apply byte-for-byte")

	siblingTarget := filepath.Join(filepath.Dir(video), stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(video), filepath.Base(env.multipart)))
	sibling, sibErr := afero.ReadFile(env.fs, siblingTarget)
	require.NoError(t, sibErr, "unrelated staged siblings still publish through the tree")
	assert.Equal(t, "part two", string(sibling))
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	for _, pd := range ledger.PlannedDeletes {
		assert.NotEqual(t, subTarget, pd.Path, "the skipped sidecar's pin was retracted at settle")
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
	}
	assert.NotContains(t, ledger.Delete, subTarget)
	for _, rp := range ledger.Replacements {
		assert.NotEqual(t, subTarget, rp.Destination, "the skipped occupant never registers as a replacement")
	}

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	occupantAfter, occAfterErr := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, occAfterErr)
	assert.Equal(t, "foreign-subtitle-bytes", string(occupantAfter), "the occupant survives the revert untouched")
	require.FileExists(t, video, "the installed copy primary is retained")
	siblingGone, sibGoneErr := afero.Exists(env.fs, siblingTarget)
	require.NoError(t, sibGoneErr)
	assert.False(t, siblingGone, "revert deletes only this row's own pinned sibling copy")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}

// Occupancy is the exclusion determinant for the deferred-copy rehome: only a
// skipped subtitle whose destination EXISTS (or cannot be proven absent) marks
// its staged sibling for exclusion; the copy-install fallbacks (absent
// destination) and every non-deferred flow stay installable.
func TestOccupiedSkipExcludedSiblings(t *testing.T) {
	const (
		stagedSource = "/stage/.source/MOV-1.mp4"
		stagedSrt    = "/stage/.source/MOV-1.srt"
		stagedVideo  = "/stage/final/RENAMED.mp4"
		finalDir     = "/library/Occupied"
	)
	subTarget := filepath.Join(finalDir, "RENAMED.srt")

	newStage := func(fs afero.Fs) *artifactStage {
		return &artifactStage{
			fs:            fs,
			videoDeferred: true,
			stagedSource:  stagedSource,
			original:      ApplyCmd{Organize: OrganizeOptions{MoveFiles: false, LinkMode: organizer.LinkModeNone}},
			siblings: []artifactSibling{
				{sourcePath: "/src/MOV-1.srt", stagedPath: stagedSrt},
			},
		}
	}
	skippedResult := func() *organizer.OrganizeResult {
		return &organizer.OrganizeResult{
			NewPath:    filepath.Join(finalDir, "RENAMED.mp4"),
			FolderPath: finalDir,
			Subtitles: []organizer.SubtitleResult{
				{SubtitleMove: models.SubtitleMove{OriginalPath: "/src/MOV-1.srt", NewPath: subTarget}, Skipped: true},
			},
		}
	}

	t.Run("occupied skip excludes the staged sibling", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("foreign"), 0o644))
		excluded := newStage(fs).occupiedSkipExcludedSiblings(stagedVideo, skippedResult())
		require.True(t, excluded[filepath.Clean(stagedSrt)], "staged path keys the exclusion")
	})

	t.Run("absent destination stays installable (failed-publish fallback)", func(t *testing.T) {
		excluded := newStage(afero.NewMemMapFs()).occupiedSkipExcludedSiblings(stagedVideo, skippedResult())
		require.Empty(t, excluded)
	})

	t.Run("unprovable destination state excludes fail-closed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &pr260StatFailureFs{Fs: base, path: subTarget}
		excluded := newStage(fs).occupiedSkipExcludedSiblings(stagedVideo, skippedResult())
		require.True(t, excluded[filepath.Clean(stagedSrt)], "uncertainty must never license an overwrite the organizer refused")
	})

	t.Run("installed subtitle does not exclude anything", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("subtitle"), 0o644))
		res := skippedResult()
		res.Subtitles[0].Skipped = false
		res.Subtitles[0].Copied = true
		require.Empty(t, newStage(fs).occupiedSkipExcludedSiblings(stagedVideo, res))
	})

	t.Run("publish-completed error seat was installed — never excluded", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("subtitle"), 0o644))
		res := skippedResult()
		res.Subtitles[0].Skipped = false
		res.Subtitles[0].Error = fmt.Errorf("subtitle published but source cleanup refused: %w", fsutil.ErrPublishCompleted)
		require.Empty(t, newStage(fs).occupiedSkipExcludedSiblings(stagedVideo, res),
			"an ErrPublishCompleted error slot proves this apply's own install, not a foreign occupant")
	})

	t.Run("skipped with empty target does not exclude", func(t *testing.T) {
		res := skippedResult()
		res.Subtitles[0].NewPath = ""
		require.Empty(t, newStage(afero.NewMemMapFs()).occupiedSkipExcludedSiblings(stagedVideo, res))
	})

	t.Run("non-deferred and copy-suppressed shapes never exclude", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("foreign"), 0o644))
		moveStage := newStage(fs)
		moveStage.original.Organize.MoveFiles = true
		require.Empty(t, moveStage.occupiedSkipExcludedSiblings(stagedVideo, skippedResult()))
		linkStage := newStage(fs)
		linkStage.original.Organize.LinkMode = organizer.LinkModeHard
		require.Empty(t, linkStage.occupiedSkipExcludedSiblings(stagedVideo, skippedResult()))
		stagedStage := newStage(fs)
		stagedStage.videoDeferred = false
		require.Empty(t, stagedStage.occupiedSkipExcludedSiblings(stagedVideo, skippedResult()))
		require.Empty(t, newStage(fs).occupiedSkipExcludedSiblings(stagedVideo, nil))
		require.Empty(t, newStage(fs).occupiedSkipExcludedSiblings("", skippedResult()))
	})
}

// A journal or organizer outcome can leave FolderPath empty even when NewPath
// is known: the exclusion join must then derive sibling targets from
// filepath.Dir(NewPath), never from the empty folder (which would join
// relative names and miss every occupant).
func TestOccupiedSkipExcludedSiblingsFolderPathEmptyFallsBackToNewPathDir(t *testing.T) {
	const (
		stagedSource = "/stage/.source/MOV-2.mp4"
		stagedSrt    = "/stage/.source/MOV-2.srt"
		stagedVideo  = "/stage/final/RENAMED.mp4"
		finalDir     = "/library/Fallback"
	)

	newStage := func(fs afero.Fs) *artifactStage {
		return &artifactStage{
			fs:            fs,
			videoDeferred: true,
			stagedSource:  stagedSource,
			original:      ApplyCmd{Organize: OrganizeOptions{MoveFiles: false, LinkMode: organizer.LinkModeNone}},
			siblings: []artifactSibling{
				{sourcePath: "/src/MOV-2.srt", stagedPath: stagedSrt},
			},
		}
	}
	skippedResult := func(target string) *organizer.OrganizeResult {
		return &organizer.OrganizeResult{
			NewPath: filepath.Join(finalDir, "RENAMED.mp4"),
			Subtitles: []organizer.SubtitleResult{
				{SubtitleMove: models.SubtitleMove{OriginalPath: "/src/MOV-2.srt", NewPath: target}, Skipped: true},
			},
		}
	}

	t.Run("occupant under Dir(NewPath) excludes the staged sibling", func(t *testing.T) {
		res := skippedResult(filepath.Join(finalDir, "RENAMED.srt"))
		require.Empty(t, res.FolderPath, "FolderPath deliberately unset — the Dir(NewPath) fallback leg")
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, res.Subtitles[0].NewPath, []byte("foreign"), 0o644))
		excluded := newStage(fs).occupiedSkipExcludedSiblings(stagedVideo, res)
		require.True(t, excluded[filepath.Clean(stagedSrt)], "the target joined on Dir(NewPath) matched the occupied destination")
	})

	t.Run("occupant away from Dir(NewPath) excludes nothing", func(t *testing.T) {
		res := skippedResult("/elsewhere/RENAMED.srt")
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/elsewhere", 0o755))
		require.NoError(t, afero.WriteFile(fs, res.Subtitles[0].NewPath, []byte("foreign"), 0o644))
		require.Empty(t, newStage(fs).occupiedSkipExcludedSiblings(stagedVideo, res),
			"the fallback never consults the skipped row's own directory")
	})
}

// codex P2 (PRRT_kwDORn9KaM6m7sQ0), reversed timing of the occupied-skip
// case: the subtitle destination is already occupied when the apply's arm
// probe runs (the endpoint is omitted from every armed pin/intent), and the
// occupant vacates between the arm and the execute. The organizer lane must
// REFUSE the now-vacant slot — nothing durable journaled that endpoint — and
// report Skipped; the deferred-copy tree leg then delivers the staged sibling
// through its own journaled install, so the apply still completes and the
// revert cleans exactly the ledgered bytes.
func TestDeferredCopyProbeOmittedEndpointRefusesVacatedInstall(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-probe-gate")
	probePlan, planErr := env.org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, planErr)
	subMoves := env.org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	subTarget := subMoves[0].NewPath

	// The occupant exists before the apply's arm probe: the endpoint is
	// omitted from every pending pin/intent the apply journals.
	require.NoError(t, env.fs.MkdirAll(filepath.Dir(subTarget), 0o755))
	require.NoError(t, afero.WriteFile(env.fs, subTarget, []byte("foreign-subtitle-bytes"), 0o644))

	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, preExecute: func(*organizer.OrganizePlan) {
		// …and vacates after the arm, before execution consumes the slot.
		require.NoError(t, env.fs.Remove(subTarget))
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	skipped := false
	for _, sr := range result.OrganizeResult.Subtitles {
		if filepath.Clean(sr.NewPath) == filepath.Clean(subTarget) {
			require.True(t, sr.Skipped, "the probe-omitted endpoint refuses the vacated slot (fail-closed)")
			assert.False(t, sr.Copied || sr.Moved, "no unjournaled install through the organizer lane")
			skipped = true
		}
	}
	require.True(t, skipped, "the gated endpoint reports through the skip classification")

	installed, readErr := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, readErr, "the staged sibling still delivers through the journaled tree leg")
	assert.Equal(t, "subtitle", string(installed))
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	pinned := false
	for _, pd := range ledger.PlannedDeletes {
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
		if filepath.Clean(pd.Path) == filepath.Clean(subTarget) {
			require.NotEmpty(t, pd.SHA256, "the tree pin keeps its content proof")
			pinned = true
		}
	}
	assert.True(t, pinned, "the journaled tree install leaves a durable hash-pinned delete record")

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	subGone, statErr := afero.Exists(env.fs, subTarget)
	require.NoError(t, statErr)
	assert.False(t, subGone, "revert reaps the journaled tree install")
	require.FileExists(t, video, "the installed copy primary is retained")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}
