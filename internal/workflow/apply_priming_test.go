package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// primingStubOrganizer implements organizer.OrganizerInterface plus the
// read-only PlanOrganize seam the workflow's planning leg asserts on.
type primingStubOrganizer struct {
	plan    *organizer.OrganizePlan
	planErr error
	gotCmd  organizer.OrganizeCmd
}

func (p *primingStubOrganizer) Organize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	return &organizer.OrganizeResult{}, nil
}

func (p *primingStubOrganizer) PlanOrganize(_ context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	p.gotCmd = cmd
	return p.plan, p.planErr
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
	})
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
