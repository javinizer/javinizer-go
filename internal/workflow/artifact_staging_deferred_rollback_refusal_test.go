package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// codex P1 (PRRT_kwDORn9KaM6qLkJ4): the deferred-move rollback arms refuse at
// every doubt point — never armed name-only for a missing identity, never
// silently partial when neither anchor can bind, and a sidecar whose
// confirmation can no longer see its own install fails the publication
// instead of silently graduating an unproven occupant.

type deferredArmEnv struct {
	fs     afero.Fs
	db     *database.DB
	orch   *applyOrchImpl
	movie  models.Movie
	match  models.FileMatchInfo
	dest   string
	source string
}

func buildDeferredArmEnv(t *testing.T, slug string, wrapFs func(afero.Fs) afero.Fs, wrapOrg func(afero.Fs, organizer.OrganizerInterface) organizer.OrganizerInterface) *deferredArmEnv {
	t.Helper()
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, slug, "")
	base, root, source, _, _, _, match := pr260FencedFiles(t, slug)
	if wrapFs != nil {
		base = wrapFs(base)
	}
	engine := template.NewEngine()
	var org organizer.OrganizerInterface = organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, engine, nil)
	if wrapOrg != nil {
		org = wrapOrg(base, org)
	}
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), slug, base, nil, nil, nil)
	nameCfg := nfo.NFONameConfig{FilenameTemplate: "<ACTRESS>.nfo", FirstNameOrder: true}
	orch := newApplyOrchestrator(base, org, nil, nil, nil, ApplyConfig{NFONameCfg: nameCfg}, engine, log, nil, nil)
	return &deferredArmEnv{fs: base, db: db, orch: orch, movie: movie, match: match, dest: filepath.Join(root, "library"), source: source}
}

// deferredOrganizerBase forwards the planning-seam methods the deferred
// publication asserts on, so the refusal-arm wrappers below can mutate
// execution outcomes without re-declaring the whole seam.
type deferredOrganizerBase struct {
	organizer.OrganizerInterface
}

func (b deferredOrganizerBase) exec() artifactPlanExecutor {
	return b.OrganizerInterface.(artifactPlanExecutor)
}

func (b deferredOrganizerBase) PlanOrganize(ctx context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	return b.exec().PlanOrganize(ctx, cmd)
}

func (b deferredOrganizerBase) PlanSourceExists(plan *organizer.OrganizePlan) bool {
	return b.exec().PlanSourceExists(plan)
}

func (b deferredOrganizerBase) ExecuteOrganizePlan(plan *organizer.OrganizePlan, hardlinkFailure bool, link organizer.LinkMode) (*organizer.OrganizeResult, error) {
	return b.exec().ExecuteOrganizePlan(plan, hardlinkFailure, link)
}

func (b deferredOrganizerBase) SubtitleSourcePaths(match models.FileMatchInfo) []string {
	return b.OrganizerInterface.(artifactSubtitleSelector).SubtitleSourcePaths(match)
}

func (b deferredOrganizerBase) PlanSubtitleMoves(plan *organizer.OrganizePlan) []models.SubtitleMove {
	return b.exec().PlanSubtitleMoves(plan)
}

// nilPrimaryIdentityOrganizer simulates an execution outcome whose move lane
// produced no publish-proven identity — the arm must refuse rather than fall
// back to a name-only re-stat.
type nilPrimaryIdentityOrganizer struct {
	deferredOrganizerBase
}

func (w nilPrimaryIdentityOrganizer) ExecuteOrganizePlan(plan *organizer.OrganizePlan, hardlinkFailure bool, link organizer.LinkMode) (*organizer.OrganizeResult, error) {
	res, err := w.deferredOrganizerBase.ExecuteOrganizePlan(plan, hardlinkFailure, link)
	if err == nil && res != nil {
		res.InstalledIdentity = nil
	}
	return res, err
}

func TestDeferredMoveRollbackArmRefusesMissingPrimaryIdentity(t *testing.T) {
	env := buildDeferredArmEnv(t, "arm-missing-primary", nil, func(_ afero.Fs, o organizer.OrganizerInterface) organizer.OrganizerInterface {
		return nilPrimaryIdentityOrganizer{deferredOrganizerBase{o}}
	})
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	_, err := env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	assert.ErrorContains(t, err, "arm deferred primary rollback")
	assert.ErrorContains(t, err, "missing bound installed identity")
}

// Plant drift immediately after the bound primary observation, instead of
// counting target probes: read-only planning may legitimately probe the final
// path before publication and must not move this safety test's fault window.
func TestDeferredMoveRollbackArmJoinsRefusalsWhenBothAnchorsDrift(t *testing.T) {
	env := buildDeferredArmEnv(t, "arm-drift-joined", nil, nil)
	oldObserve := observeBoundPrimaryPublish
	t.Cleanup(func() { observeBoundPrimaryPublish = oldObserve })
	fired := false
	observeBoundPrimaryPublish = func(batch *downloader.ReplacementBatch, target string, identity *fsutil.BoundInstallIdentity) error {
		if err := oldObserve(batch, target, identity); err != nil {
			return err
		}
		fired = true
		require.NoError(t, afero.WriteFile(env.fs, target, []byte("drifted install bytes"), 0o644))
		return nil
	}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	_, err := env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	require.True(t, fired, "the drift actually landed inside the arm window")
	assert.ErrorContains(t, err, "no longer names its bound installed output")
	assert.Equal(t, 2, strings.Count(err.Error(), "no longer names its bound installed output"),
		"both the preferred and the alternate anchor refused, joined")
}

// nilSubtitleIdentityOrganizer simulates moved sidecar reports carrying no
// publish-proven identity — the arm must refuse instead of adopting by name.
type nilSubtitleIdentityOrganizer struct {
	deferredOrganizerBase
}

func (w nilSubtitleIdentityOrganizer) ExecuteOrganizePlan(plan *organizer.OrganizePlan, hardlinkFailure bool, link organizer.LinkMode) (*organizer.OrganizeResult, error) {
	res, err := w.deferredOrganizerBase.ExecuteOrganizePlan(plan, hardlinkFailure, link)
	if err == nil && res != nil {
		for i := range res.Subtitles {
			res.Subtitles[i].InstalledIdentity = nil
		}
	}
	return res, err
}

func TestDeferredMoveSubtitleRollbackArmRefusesMissingIdentity(t *testing.T) {
	env := buildDeferredArmEnv(t, "arm-missing-subtitle", nil, func(_ afero.Fs, o organizer.OrganizerInterface) organizer.OrganizerInterface {
		return nilSubtitleIdentityOrganizer{deferredOrganizerBase{o}}
	})
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	_, err := env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	assert.ErrorContains(t, err, "arm subtitle rollback")
	assert.ErrorContains(t, err, "missing bound installed identity")
}

// vanishAtConfirmWindowFS deletes the copy-installed sidecar destination
// exactly after the leg's bound observation passed: the confirmation then
// lands on an indeterminate name and must fail the publication rather than
// silently graduate what it can no longer inspect. The window is pinned to
// the confirmation leg's own probe sequence of the destination.
type vanishAtConfirmWindowFS struct {
	afero.Fs
	calls int
	fired bool
}

const sidecarConfirmWindowProbe = 9

func (fs *vanishAtConfirmWindowFS) Stat(name string) (os.FileInfo, error) {
	info, err := fs.Fs.Stat(name)
	if !fs.fired && filepath.Ext(name) == ".srt" && strings.Contains(name, "library") {
		fs.calls++
		if fs.calls == sidecarConfirmWindowProbe {
			fs.fired = true
			_ = fs.Fs.Remove(name)
		}
	}
	return info, err
}

func TestDeferredCopySidecarConfirmFailsClosedWhenDestinationIndeterminate(t *testing.T) {
	probe := &vanishAtConfirmWindowFS{}
	env := buildDeferredArmEnv(t, "sidecar-confirm-vanish", func(base afero.Fs) afero.Fs { probe.Fs = base; return probe }, nil)
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	_, err := env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	require.True(t, probe.fired, "the destination vanished inside the confirmation window")
	// Native handle probes can reject at binding or at the following confirmation.
	// Both must refuse the vanished destination rather than report publication success.
	assert.True(t, strings.Contains(err.Error(), "bind copy-installed sidecar") ||
		strings.Contains(err.Error(), "inspect staged publication result"), "unexpected refusal: %v", err)
}
