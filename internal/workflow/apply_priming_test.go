package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// primingStubOrganizer implements organizer.OrganizerInterface plus the
// read-only priming seam (PlanOrganize + PlanSourceExists) the workflow's
// planning leg asserts on (codex r2 P2).
type primingStubOrganizer struct {
	plan        *organizer.OrganizePlan
	planErr     error
	sourceGone  bool // PlanSourceExists reports false (source vanished)
	existsCalls int
	gotCmd      organizer.OrganizeCmd
}

func (p *primingStubOrganizer) Organize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	return &organizer.OrganizeResult{}, nil
}

func (p *primingStubOrganizer) PlanOrganize(_ context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	p.gotCmd = cmd
	return p.plan, p.planErr
}

func (p *primingStubOrganizer) PlanSourceExists(*organizer.OrganizePlan) bool {
	p.existsCalls++
	return !p.sourceGone
}

// recordingPrimingOrch is an applyOrchestrator fake recording priming calls.
type recordingPrimingOrch struct {
	prim  organizer.DuplicatePriming
	err   error
	calls int
}

func (r *recordingPrimingOrch) Execute(context.Context, ApplyCmd) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}

func (r *recordingPrimingOrch) planDuplicatePriming(context.Context, ApplyCmd) (organizer.DuplicatePriming, error) {
	r.calls++
	return r.prim, r.err
}

func TestNoOpApplyOrchestrator_PlanDuplicatePriming(t *testing.T) {
	prim, err := noOpApplyOrchestrator{}.planDuplicatePriming(context.Background(), ApplyCmd{})
	assert.NoError(t, err)
	assert.Equal(t, organizer.DuplicatePriming{}, prim, "an unconfigured apply registers no claims")
}

func TestApplyOrch_PlanDuplicatePriming(t *testing.T) {
	ctx := context.Background()
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-123"},
		Match:    models.FileMatchInfo{Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv", MovieID: "ABC-123"},
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true, ForceUpdate: true, LinkMode: organizer.LinkModeHard, ForceRenameFile: true},
		DryRun:   true,
	}

	newOrch := func(org organizer.OrganizerInterface) *applyOrchImpl {
		return newApplyOrchestrator(afero.NewMemMapFs(), org, nil, nil, nil, ApplyConfig{}, nil, nil, nil, nil)
	}

	t.Run("skipped organize registers no claim and never plans", func(t *testing.T) {
		org := &primingStubOrganizer{planErr: errors.New("must not be called")}
		prim, err := newOrch(org).planDuplicatePriming(ctx, ApplyCmd{Organize: OrganizeOptions{Skip: true}})
		assert.NoError(t, err)
		assert.Equal(t, organizer.DuplicatePriming{}, prim)
		assert.Equal(t, organizer.OrganizeCmd{}, org.gotCmd, "the planner is untouched for skipped runs")
	})

	t.Run("an organizer without the planning seam registers no claim", func(t *testing.T) {
		prim, err := newOrch(&stubOrganizer{}).planDuplicatePriming(ctx, cmd)
		assert.NoError(t, err)
		assert.Equal(t, organizer.DuplicatePriming{}, prim)
	})

	t.Run("plan errors propagate", func(t *testing.T) {
		planErr := errors.New("template boom")
		_, err := newOrch(&primingStubOrganizer{planErr: planErr}).planDuplicatePriming(ctx, cmd)
		assert.ErrorIs(t, err, planErr)
	})

	t.Run("successful planning maps the claim and the mirrored command", func(t *testing.T) {
		org := &primingStubOrganizer{plan: &organizer.OrganizePlan{
			SourcePath: "/in/A.mkv", TargetPath: "/dest/ABC-123/ABC-123.mkv", WillMove: true,
		}}
		prim, err := newOrch(org).planDuplicatePriming(ctx, cmd)
		require.NoError(t, err)
		assert.Equal(t, organizer.DuplicatePriming{
			SourcePath: "/in/A.mkv",
			TargetPath: "/dest/ABC-123/ABC-123.mkv",
			WillMove:   true,
		}, prim)
		// The planning command mirrors stepOrganize's assembly — minus the
		// duplicate tracker itself, which only execution registers against.
		assert.Equal(t, "/dest", org.gotCmd.DestDir)
		assert.Equal(t, "/in/A.mkv", org.gotCmd.Match.Path)
		assert.True(t, org.gotCmd.ForceUpdate)
		assert.True(t, org.gotCmd.MoveFiles)
		assert.True(t, org.gotCmd.DryRun)
		assert.True(t, org.gotCmd.ForceRenameFile)
		assert.Equal(t, organizer.LinkModeHard, org.gotCmd.LinkMode)
		assert.Nil(t, org.gotCmd.DuplicateTracker, "planning must not observe against the run's tracker")
		assert.Equal(t, 1, org.existsCalls, "a movable plan with a non-empty target is existence-checked before claiming")
	})

	t.Run("a claimant whose source vanished at priming registers no claim", func(t *testing.T) {
		org := &primingStubOrganizer{
			plan:       &organizer.OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/dest/ABC-123/ABC-123.mkv", WillMove: true},
			sourceGone: true,
		}
		prim, err := newOrch(org).planDuplicatePriming(ctx, cmd)
		assert.NoError(t, err, "a vanished source is not a planning error — the worker fails with the identical plan/validation error later")
		assert.Equal(t, organizer.DuplicatePriming{}, prim,
			"codex r2 P2: an inexecutable plan must never own the canonical key at priming time")
		assert.Equal(t, 1, org.existsCalls)
	})

	t.Run("residents are existence-checked pre-park (codex P2, PR #241 F1)", func(t *testing.T) {
		// A VERIFIED resident parks normally — the F1 gate keeps stationary
		// claims exactly as they were when the source is present.
		org := &primingStubOrganizer{plan: &organizer.OrganizePlan{
			SourcePath: "/dest/ABC-123/ABC-123.mkv", TargetPath: "/dest/ABC-123/ABC-123.mkv", WillMove: false,
		}}
		prim, err := newOrch(org).planDuplicatePriming(ctx, cmd)
		require.NoError(t, err)
		assert.Equal(t, organizer.DuplicatePriming{
			SourcePath: "/dest/ABC-123/ABC-123.mkv", TargetPath: "/dest/ABC-123/ABC-123.mkv", WillMove: false,
		}, prim)
		assert.Equal(t, 1, org.existsCalls, "the source-existence gate covers residents too")

		// An unverifiable resident never parks: its born-settled ghost claim
		// would otherwise seal the key for the whole run (codex P2, PR #241
		// F1) — the residual priming→worker vanish window is covered by the
		// resident's own failure releasing its parked claim.
		gone := &primingStubOrganizer{
			plan:       &organizer.OrganizePlan{SourcePath: "/dest/ABC-123/ABC-123.mkv", TargetPath: "/dest/ABC-123/ABC-123.mkv", WillMove: false},
			sourceGone: true,
		}
		prim, err = newOrch(gone).planDuplicatePriming(ctx, cmd)
		require.NoError(t, err)
		assert.Equal(t, organizer.DuplicatePriming{}, prim,
			"a resident whose source vanished at priming never parks a ghost claim")
		assert.Equal(t, 1, gone.existsCalls)
	})

	t.Run("empty-target plans never reach the existence check", func(t *testing.T) {
		org := &primingStubOrganizer{plan: &organizer.OrganizePlan{
			SourcePath: "/in/A.mkv", TargetPath: "", WillMove: true,
		}}
		prim, err := newOrch(org).planDuplicatePriming(ctx, cmd)
		require.NoError(t, err)
		assert.Equal(t, "", prim.TargetPath)
		assert.Equal(t, 0, org.existsCalls, "empty-target primings register nothing, so existence is irrelevant")
	})
}

// TestApplyOrch_PlanDuplicatePriming_RealOrganizerResidentGate pins codex P2
// (PR #241 F1) end to end over the REAL organizer: the priming gate verifies
// a stationary resident's source BEFORE its key is parked — a resident
// already gone at priming registers nothing (no born-settled ghost claim),
// while a present resident parks its ordinary WillMove=false claim.
func TestApplyOrch_PlanDuplicatePriming_RealOrganizerResidentGate(t *testing.T) {
	fs := afero.NewMemMapFs()
	org := organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0o755))
	impl := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, nil, nil, nil, nil)
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-123"},
		Match:    models.FileMatchInfo{MovieID: "ABC-123", Path: "/dest/ABC-123/ABC-123.mkv", Name: "ABC-123.mkv", Extension: ".mkv"},
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}

	// Resident ABSENT at priming: no priming — the ghost can never park (the
	// residual priming→worker vanish window is covered by the resident's own
	// failure releasing its parked claim).
	prim, err := impl.planDuplicatePriming(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, organizer.DuplicatePriming{}, prim,
		"an unverifiable resident must never park a born-settled ghost claim")

	// Resident PRESENT at priming: parks its stationary claim exactly as
	// before (codex P1 behavior unchanged for verified residents).
	require.NoError(t, afero.WriteFile(fs, "/dest/ABC-123/ABC-123.mkv", []byte("resident-bytes"), 0o644))
	prim, err = impl.planDuplicatePriming(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, "/dest/ABC-123/ABC-123.mkv", filepath.ToSlash(prim.SourcePath))
	assert.Equal(t, "/dest/ABC-123/ABC-123.mkv", filepath.ToSlash(prim.TargetPath))
	assert.False(t, prim.WillMove, "a verified stationary input keeps its resident priming")
}

// TestWorkflow_PlanDuplicatePriming_Delegates pins the Workflow composition
// root routing of the optional priming seam (#240 finding A).
func TestWorkflow_PlanDuplicatePriming_Delegates(t *testing.T) {
	rec := &recordingPrimingOrch{
		prim: organizer.DuplicatePriming{SourcePath: "/in/A.mkv", TargetPath: "/dest/x.mkv", WillMove: true},
	}
	w := &Workflow{apply: rec}
	prim, err := w.PlanDuplicatePriming(context.Background(), ApplyCmd{})
	require.NoError(t, err)
	assert.Equal(t, rec.prim, prim)
	assert.Equal(t, 1, rec.calls, "exactly one delegated planning call")

	rec.err = errors.New("plan boom")
	_, err = w.PlanDuplicatePriming(context.Background(), ApplyCmd{})
	assert.ErrorIs(t, err, rec.err)
}
