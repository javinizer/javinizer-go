package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type beginGuardOrganizer struct {
	calls int
}

func (o *beginGuardOrganizer) Organize(_ context.Context, _ organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	o.calls++
	return &organizer.OrganizeResult{NewPath: "/dest/movie.mp4"}, nil
}

// TestApply_BeginFailureFailsDestructiveStep proves a failed ledger Begin is a
// hard precondition: no organize/download/NFO step may run without a durable
// inverse for destructive work.
func TestApply_BeginFailureFailsDestructiveStep(t *testing.T) {
	org := &beginGuardOrganizer{}
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		organizer: org,
		revertLog: &stubRevertLog{beginErr: errTestBegin},
	}

	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "BEGIN-FAIL-001"},
		Match:    models.FileMatchInfo{Path: "/source/BEGIN-FAIL-001.mp4", MovieID: "BEGIN-FAIL-001"},
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	})

	require.ErrorIs(t, err, errTestBegin)
	require.NotNil(t, result)
	require.Equal(t, "revert_begin", result.FailedStep)
	require.Equal(t, 0, org.calls, "no destructive step may run after Begin fails")
}
