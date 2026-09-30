package workflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// The rehome exclusion for organizer-INSTALLED seats (codex P2,
// PRRT_kwDORn9KaM6m7CBi): a Copied subtitle — or its publish-completed error
// twin — proves the destination already holds this apply's bytes, so the
// staged duplicate must never re-aim the endpoint through installPaths.
// Skipped and plain-error seats stay installable through the fallback.
func TestInstalledSidecarExcludedSiblings(t *testing.T) {
	const (
		srcA    = "/src/MOV-1.srt"
		srcB    = "/src/MOV-1.other.srt"
		stagedA = "/stage/.source/MOV-1.srt"
		stagedB = "/stage/.source/MOV-1.other.srt"
		targetA = "/lib/MOV-1/MOV-1.srt"
		targetB = "/lib/MOV-1/MOV-1.other.srt"
	)
	newStage := func(move bool, link organizer.LinkMode, deferred bool) *artifactStage {
		return &artifactStage{
			fs:            afero.NewMemMapFs(),
			videoDeferred: deferred,
			stagedSource:  "/stage/.source/MOV-1.mp4",
			original:      ApplyCmd{Organize: OrganizeOptions{MoveFiles: move, LinkMode: link}},
			siblings: []artifactSibling{
				{sourcePath: srcA, stagedPath: stagedA},
				{sourcePath: srcB, stagedPath: stagedB},
			},
		}
	}
	seat := func(orig, target string, copied, skipped bool, serr error) organizer.SubtitleResult {
		sr := organizer.SubtitleResult{SubtitleMove: models.SubtitleMove{OriginalPath: orig, NewPath: target}, Error: serr}
		sr.Copied = copied
		sr.Skipped = skipped
		return sr
	}
	deferredCopyStage := func() *artifactStage { return newStage(false, organizer.LinkModeNone, true) }

	t.Run("copied seat excludes only its own staged sibling", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat(srcA, targetA, true, false, nil)}}
		excluded := deferredCopyStage().installedSidecarExcludedSiblings(res)
		require.True(t, excluded[filepath.Clean(stagedA)], "the installed source's staged copy stays out of the tree")
		assert.False(t, excluded[filepath.Clean(stagedB)], "uninstalled siblings still rehome")
	})

	t.Run("publish-completed error seat excludes (round-28 classification is installed)", func(t *testing.T) {
		pcErr := fmt.Errorf("subtitle published but source cleanup refused — both copies retained (%w)", fsutil.ErrPublishCompleted)
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat(srcA, targetA, false, false, pcErr)}}
		excluded := deferredCopyStage().installedSidecarExcludedSiblings(res)
		require.True(t, excluded[filepath.Clean(stagedA)])
	})

	t.Run("two installed seats exclude both siblings", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{
			seat(srcA, targetA, true, false, nil),
			seat(srcB, targetB, true, false, nil),
		}}
		excluded := deferredCopyStage().installedSidecarExcludedSiblings(res)
		require.True(t, excluded[filepath.Clean(stagedA)])
		require.True(t, excluded[filepath.Clean(stagedB)])
	})

	t.Run("skipped seat does not exclude (the occupancy lane owns it)", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat(srcA, targetA, false, true, nil)}}
		require.Empty(t, deferredCopyStage().installedSidecarExcludedSiblings(res))
	})

	t.Run("plain error seat does not exclude (genuinely uninstalled keeps the fallback)", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat(srcA, targetA, false, false, errors.New("subtitle publish failed pre-install"))}}
		require.Empty(t, deferredCopyStage().installedSidecarExcludedSiblings(res))
	})

	t.Run("copied seat with empty target does not exclude", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat(srcA, "", true, false, nil)}}
		require.Empty(t, deferredCopyStage().installedSidecarExcludedSiblings(res))
	})

	t.Run("copied seat with empty original does not exclude", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat("", targetA, true, false, nil)}}
		require.Empty(t, deferredCopyStage().installedSidecarExcludedSiblings(res))
	})

	t.Run("installed seat for an unadmitted source excludes nothing", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat("/src/never-admitted.srt", targetA, true, false, nil)}}
		require.Empty(t, deferredCopyStage().installedSidecarExcludedSiblings(res))
	})

	t.Run("non-deferred, move, and link shapes never exclude", func(t *testing.T) {
		res := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{seat(srcA, targetA, true, false, nil)}}
		require.Empty(t, newStage(true, organizer.LinkModeNone, true).installedSidecarExcludedSiblings(res), "deferred move lane installs sidecars itself")
		require.Empty(t, newStage(false, organizer.LinkModeHard, true).installedSidecarExcludedSiblings(res), "link flows never rehome")
		require.Empty(t, newStage(false, organizer.LinkModeNone, false).installedSidecarExcludedSiblings(res), "non-deferred flows own a different deliver lane")
	})

	t.Run("nil final result never excludes", func(t *testing.T) {
		require.Empty(t, deferredCopyStage().installedSidecarExcludedSiblings(nil))
	})
}

// codex P2 (PRRT_kwDORn9KaM6m7CBi): a deferred copy whose subtitle the
// organizer lane confirmed Copied must not be republished through the staged
// tree. A foreign writer replacing the destination between execute and
// installTree defeats the sameBytes skip; the exclusion keeps the staged
// duplicate OUT, so the occupant survives byte-for-byte, the sidecar's armed
// batch leg consumes, and its durable pin graduates unretracted.
func TestDeferredCopyCopiedSidecarForeignSwapSurvives(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-installed-foreign-swap")
	probePlan, err := env.org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	subMoves := env.org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	subTarget := subMoves[0].NewPath

	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		// The foreign writer lands between the organizer's copy and the tree
		// install — the exact interleaving the exclusion covers.
		require.NoError(t, afero.WriteFile(env.fs, subTarget, []byte("foreign-swap-bytes"), 0o644))
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	occupant, err := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, err, "installPaths never overwrote the foreign occupant")
	assert.Equal(t, "foreign-swap-bytes", string(occupant))

	copied := false
	for _, sr := range result.OrganizeResult.Subtitles {
		if sr.Copied && filepath.Clean(sr.NewPath) == filepath.Clean(subTarget) {
			copied = true
		}
	}
	require.True(t, copied, "the organizer lane confirmed the copy")

	busy, err := afero.Exists(env.fs, subTarget+fsutil.ReplacementBusySuffix)
	require.NoError(t, err)
	assert.False(t, busy, "the sidecar's batch leg confirmed and released its claim")

	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	siblingBytes, err := afero.ReadFile(env.fs, filepath.Join(filepath.Dir(video), stem+"-cd2.mp4"))
	require.NoError(t, err, "unplanned siblings still rehome-install through the tree")
	assert.Equal(t, "part two", string(siblingBytes))
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	for _, rp := range ledger.Replacements {
		assert.NotEqual(t, subTarget, rp.Destination, "the occupant never registered as this apply's replacement")
	}
	for _, pd := range ledger.PlannedDeletes {
		assert.NotEqual(t, subTarget, pd.Path, "the confirmed pin was never retracted to pending — it graduated")
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
	}
	assert.Contains(t, ledger.Delete, subTarget, "the armed pin graduated into the plain delete ledger unretracted")
}

// The publish-completed classification is installed for the exclusion too:
// doctoring the seat into its round-28 shape must also keep the staged
// duplicate out of the tree, and the retained hash pin must keep the foreign
// occupant safe through a LATER revert (bytes no longer match the pin).
func TestDeferredCopyPublishCompletedSidecarForeignSwapSurvives(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-pc-foreign-swap")
	probePlan, err := env.org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	subMoves := env.org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	subTarget := subMoves[0].NewPath

	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		require.NoError(t, afero.WriteFile(env.fs, subTarget, []byte("foreign-swap-bytes"), 0o644))
		doctorPublishCompletedSubtitle(result)
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err, "a publish-completed subtitle error stays nonfatal")
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	reported := false
	for _, sr := range result.OrganizeResult.Subtitles {
		if filepath.Clean(sr.NewPath) == filepath.Clean(subTarget) {
			require.Error(t, sr.Error)
			assert.True(t, fsutil.PublishCompleted(sr.Error))
			assert.False(t, sr.Copied)
			reported = true
		}
	}
	require.True(t, reported)

	occupant, err := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, err, "the publish-completed seat's staged duplicate never republished through the tree")
	assert.Equal(t, "foreign-swap-bytes", string(occupant))
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	pinned := false
	for _, pd := range ledger.PlannedDeletes {
		if pd.Path == subTarget {
			pinned = true
		}
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
	}
	assert.True(t, pinned, "the publish-completed sidecar's hash pin stayed armed through reconcile and completion")
	assert.NotContains(t, ledger.Delete, subTarget)

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	after, err := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, err, "the pin's hash guard retains the foreign occupant through the revert")
	assert.Equal(t, "foreign-swap-bytes", string(after))
	require.FileExists(t, video, "the installed copy primary is retained")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}

// denyOpenReadFS refuses fs.Open of one path while armed: the execute leg's
// subtitle copy fails as a PLAIN error (destination never touched), so the
// seat proves nothing installed and the staged copy must still rehome.
type denyOpenReadFS struct {
	afero.Fs
	path string
	deny *atomic.Bool
}

func (f *denyOpenReadFS) Open(name string) (afero.File, error) {
	if f.deny.Load() && filepath.Clean(name) == filepath.Clean(f.path) {
		return nil, errors.New("deny-open-read fault")
	}
	return f.Fs.Open(name)
}

// Contrast leg: a genuinely UNINSTALLED sidecar (plain subtitle error, target
// left absent) keeps the existing rehome path — its staged copy publishes
// through the tree with its own fresh pin, and a revert reaps it.
func TestDeferredCopyPlainErrorSidecarStillRehomeInstalls(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-plain-error-rehome")
	probePlan, err := env.org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	subMoves := env.org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	subTarget := subMoves[0].NewPath

	flag := &atomic.Bool{}
	denyFS := &denyOpenReadFS{Fs: env.fs, path: env.subtitle, deny: flag}
	engine := template.NewEngine()
	denyOrg := organizer.NewOrganizer(denyFS, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, engine, nil)
	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: denyOrg, preExecute: func(*organizer.OrganizePlan) {
		flag.Store(true)
	}}

	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	reported := false
	for _, sr := range result.OrganizeResult.Subtitles {
		if filepath.Clean(sr.NewPath) == filepath.Clean(subTarget) {
			reported = true
			require.Error(t, sr.Error, "the copy refusal rides the seat")
			assert.False(t, fsutil.PublishCompleted(sr.Error), "pre-install failure proves nothing installed")
			assert.False(t, sr.Copied)
			assert.False(t, sr.Skipped)
		}
	}
	require.True(t, reported)

	rehomed, err := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, err, "the genuinely uninstalled sidecar still rehome-installs through the tree")
	assert.Equal(t, "subtitle", string(rehomed))
	busy, err := afero.Exists(env.fs, subTarget+fsutil.ReplacementBusySuffix)
	require.NoError(t, err)
	assert.False(t, busy, "the around-execute sidecar leg released and the tree leg confirmed")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	pinned := false
	for _, pd := range ledger.PlannedDeletes {
		if pd.Path == subTarget {
			pinned = true
		}
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
	}
	assert.True(t, pinned, "the tree lane journaled its own fresh pin for the fallback install")

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	gone, err := afero.Exists(env.fs, subTarget)
	require.NoError(t, err)
	assert.False(t, gone, "revert reaps the fallback-installed sidecar")
	require.FileExists(t, video, "the installed copy primary is retained")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}
