package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/require"
)

type publicationFenceWorkflowStub struct {
	cmd ApplyCmd
}

func (s *publicationFenceWorkflowStub) Execute(_ context.Context, cmd ApplyCmd) (*ApplyResult, error) {
	s.cmd = cmd
	return &ApplyResult{Movie: cmd.Movie}, nil
}

func (*publicationFenceWorkflowStub) planDuplicatePriming(context.Context, ApplyCmd) (organizer.DuplicatePriming, error) {
	return organizer.DuplicatePriming{}, nil
}

type publicationFenceStub struct{}

func (*publicationFenceStub) WithApplyPublicationFence(context.Context, string, int64, func(*models.Movie) error) error {
	return nil
}

func TestWorkflowApplyCarriesPublicationFencePR260(t *testing.T) {
	fencer := &publicationFenceStub{}
	orchestrator := &publicationFenceWorkflowStub{}
	wf := &Workflow{apply: orchestrator}
	movie := &models.Movie{ID: "PR260-WIRING", ContentID: "pr260-wiring"}

	_, err := wf.Apply(context.Background(), ApplyCmd{Movie: movie, PublicationFence: fencer})
	require.NoError(t, err)
	require.Same(t, fencer, orchestrator.cmd.PublicationFence)
}
