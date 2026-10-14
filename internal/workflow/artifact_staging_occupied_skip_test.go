package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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

// A skipped source is excluded regardless of later destination occupancy.
// Non-skipped sources and non-deferred flows keep their existing behavior.
func TestRefusedSidecarExcludedSiblings(t *testing.T) {
	const (
		stagedSource = "/stage/.source/MOV-1.mp4"
		stagedSrt    = "/stage/.source/MOV-1.srt"
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
		excluded := newStage(fs).refusedSidecarExcludedSiblings(skippedResult())
		require.True(t, excluded[filepath.Clean(stagedSrt)], "staged path keys the exclusion")
	})

	t.Run("absent destination still excludes the refused source", func(t *testing.T) {
		excluded := newStage(afero.NewMemMapFs()).refusedSidecarExcludedSiblings(skippedResult())
		require.True(t, excluded[filepath.Clean(stagedSrt)])
	})

	t.Run("unprovable destination state excludes fail-closed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &pr260StatFailureFs{Fs: base, path: subTarget}
		excluded := newStage(fs).refusedSidecarExcludedSiblings(skippedResult())
		require.True(t, excluded[filepath.Clean(stagedSrt)], "uncertainty must never license an overwrite the organizer refused")
	})

	t.Run("installed subtitle does not exclude anything", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("subtitle"), 0o644))
		res := skippedResult()
		res.Subtitles[0].Skipped = false
		res.Subtitles[0].Copied = true
		require.Empty(t, newStage(fs).refusedSidecarExcludedSiblings(res))
	})

	t.Run("publish-completed error seat was installed — never excluded", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("subtitle"), 0o644))
		res := skippedResult()
		res.Subtitles[0].Skipped = false
		res.Subtitles[0].Error = fmt.Errorf("subtitle published but source cleanup refused: %w", fsutil.ErrPublishCompleted)
		require.Empty(t, newStage(fs).refusedSidecarExcludedSiblings(res),
			"an ErrPublishCompleted error slot proves this apply's own install, not a foreign occupant")
	})

	t.Run("skipped source stays excluded even without a target", func(t *testing.T) {
		res := skippedResult()
		res.Subtitles[0].NewPath = ""
		excluded := newStage(afero.NewMemMapFs()).refusedSidecarExcludedSiblings(res)
		require.True(t, excluded[filepath.Clean(stagedSrt)])
	})

	t.Run("skipped with empty source does not exclude", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("foreign"), 0o644))
		res := skippedResult()
		res.Subtitles[0].OriginalPath = ""
		require.Empty(t, newStage(fs).refusedSidecarExcludedSiblings(res))
	})

	t.Run("different source does not exclude an admitted sibling", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("foreign"), 0o644))
		res := skippedResult()
		res.Subtitles[0].OriginalPath = "/src/other.srt"
		require.Empty(t, newStage(fs).refusedSidecarExcludedSiblings(res))
	})

	t.Run("non-deferred and copy-suppressed shapes never exclude", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, subTarget, []byte("foreign"), 0o644))
		moveStage := newStage(fs)
		moveStage.original.Organize.MoveFiles = true
		require.Empty(t, moveStage.refusedSidecarExcludedSiblings(skippedResult()))
		linkStage := newStage(fs)
		linkStage.original.Organize.LinkMode = organizer.LinkModeHard
		require.Empty(t, linkStage.refusedSidecarExcludedSiblings(skippedResult()))
		stagedStage := newStage(fs)
		stagedStage.videoDeferred = false
		require.Empty(t, stagedStage.refusedSidecarExcludedSiblings(skippedResult()))
		require.Empty(t, newStage(fs).refusedSidecarExcludedSiblings(nil))
	})
}

// Exclusion follows the skipped source even when FolderPath is empty or the
// organizer's occupied target does not live beside the primary video.
func TestRefusedSidecarExcludedSiblingsTracksSourceWithoutFolderPath(t *testing.T) {
	const (
		stagedSource = "/stage/.source/MOV-2.mp4"
		stagedSrt    = "/stage/.source/MOV-2.srt"
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

	t.Run("occupied destination excludes the staged sibling", func(t *testing.T) {
		res := skippedResult(filepath.Join(finalDir, "RENAMED.srt"))
		require.Empty(t, res.FolderPath, "FolderPath is unnecessary for source-based matching")
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, res.Subtitles[0].NewPath, []byte("foreign"), 0o644))
		excluded := newStage(fs).refusedSidecarExcludedSiblings(res)
		require.True(t, excluded[filepath.Clean(stagedSrt)], "the skipped source identifies its staged sibling")
	})

	t.Run("occupied target in another directory still excludes its source", func(t *testing.T) {
		res := skippedResult("/elsewhere/RENAMED.srt")
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/elsewhere", 0o755))
		require.NoError(t, afero.WriteFile(fs, res.Subtitles[0].NewPath, []byte("foreign"), 0o644))
		excluded := newStage(fs).refusedSidecarExcludedSiblings(res)
		require.True(t, excluded[filepath.Clean(stagedSrt)], "the source stays excluded regardless of the organizer's target directory")
	})
}

// codex P2 (PRRT_kwDORn9KaM6m7sQ0), reversed timing of the occupied-skip
// case: the subtitle destination is already occupied when the apply's arm
// probe runs (the endpoint is omitted from every armed pin/intent), and the
// occupant vacates between the arm and the execute. The organizer lane must
// REFUSE the now-vacant slot — nothing durable journaled that endpoint — and
// report Skipped. The staged copy must honor that refusal even though the
// endpoint is now vacant; the rest of the apply completes and reverts normally.
func TestDeferredCopyProbeOmittedEndpointKeepsVacatedSkip(t *testing.T) {
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

	require.NoFileExists(t, subTarget, "a skipped source never re-enters the tree publication lane")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	for _, pd := range ledger.PlannedDeletes {
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
		assert.NotEqual(t, filepath.Clean(subTarget), filepath.Clean(pd.Path), "the refused subtitle has no tree install pin")
	}
	assert.NotContains(t, ledger.Delete, subTarget)

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	subGone, statErr := afero.Exists(env.fs, subTarget)
	require.NoError(t, statErr)
	assert.False(t, subGone, "the refused subtitle remains absent after revert")
	require.FileExists(t, video, "the installed copy primary is retained")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}

// A normalized language suffix changes the final leaf name. An occupied skip
// must exclude the staged copy by source, otherwise the fallback publishes a
// second subtitle under the unnormalized name (including custom extensions).
func TestDeferredNormalizedOccupiedSkipNeverPublishesFallback(t *testing.T) {
	for _, extension := range []string{".srt", ".ttml"} {
		for _, move := range []bool{false, true} {
			mode := "copy"
			if move {
				mode = "move"
			}
			for _, occupiedAtProbe := range []bool{true, false} {
				timing := "after_probe"
				if occupiedAtProbe {
					timing = "before_probe"
				}
				t.Run(extension+"/"+mode+"/"+timing, func(t *testing.T) {
					env := setupCopyIntentE2E(t, "normalized-occupied"+extension+"-"+timing)
					subtitle := strings.TrimSuffix(env.source, filepath.Ext(env.source)) + ".en" + extension
					require.NoError(t, env.fs.Rename(env.subtitle, subtitle))
					env.subtitle = subtitle
					env.org = organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{extension}}, nil, nil)
					env.orch.organizer = env.org
					plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
					require.NoError(t, err)
					moves := env.org.PlanSubtitleMoves(plan)
					require.Len(t, moves, 1)
					target := moves[0].NewPath
					require.True(t, strings.HasSuffix(target, ".eng"+extension))
					fallback := filepath.Join(plan.TargetDir, stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(subtitle)))
					require.NotEqual(t, target, fallback, "normalization must differ from the generic fallback")
					occupy := func() {
						require.NoError(t, env.fs.MkdirAll(filepath.Dir(target), 0o755))
						require.NoError(t, afero.WriteFile(env.fs, target, []byte("foreign subtitle"), 0o644))
					}
					if occupiedAtProbe {
						occupy()
					} else {
						env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, preExecute: func(*organizer.OrganizePlan) { occupy() }}
					}
					cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
					result, err := env.orch.Execute(t.Context(), cmd)
					require.NoError(t, err)
					require.NotNil(t, result.OrganizeResult)
					require.Len(t, result.OrganizeResult.Subtitles, 1)
					seat := result.OrganizeResult.Subtitles[0]
					require.True(t, seat.Skipped)
					assert.Equal(t, subtitle, seat.OriginalPath)
					assert.False(t, seat.Copied || seat.Moved)
					assert.NoFileExists(t, fallback, "a skipped source cannot publish an unnormalized duplicate")
					occupant, err := afero.ReadFile(env.fs, target)
					require.NoError(t, err)
					assert.Equal(t, "foreign subtitle", string(occupant))
					sourceSubtitle, err := afero.ReadFile(env.fs, subtitle)
					require.NoError(t, err)
					assert.Equal(t, "subtitle", string(sourceSubtitle))
					if move {
						assert.NoFileExists(t, env.source)
						assert.NoFileExists(t, env.multipart)
					} else {
						pr260AssertRetained(t, env.fs, env.source, subtitle, env.multipart, env.unrelated)
					}
					ledger := p3Ledger(t, env.repo, result.OperationID)
					for _, path := range []string{target, fallback} {
						assert.NotContains(t, ledger.Delete, path)
						for _, pin := range ledger.PlannedDeletes {
							assert.NotEqual(t, path, pin.Path, "neither subtitle endpoint is owned by this apply")
						}
						for _, replacement := range ledger.Replacements {
							assert.NotEqual(t, path, replacement.Destination)
						}
					}
					partTarget := filepath.Join(plan.TargetDir, stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(env.multipart)))
					require.FileExists(t, partTarget, "multipart siblings still publish independently")
					reverted, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, err)
					require.Equal(t, 1, reverted.Succeeded)
					occupant, err = afero.ReadFile(env.fs, target)
					require.NoError(t, err)
					assert.Equal(t, "foreign subtitle", string(occupant))
					assert.NoFileExists(t, fallback)
					assert.NoFileExists(t, partTarget)
					pr260AssertRetained(t, env.fs, env.source, subtitle, env.multipart, env.unrelated)
					pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))
				})
			}
		}
	}
}
