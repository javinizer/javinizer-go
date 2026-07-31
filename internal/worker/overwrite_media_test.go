package worker

import (
	"context"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureApplyWorkflow struct {
	stubApplyWorkflow
	mu   sync.Mutex
	cmds []workflow.ApplyCmd
}

func (w *captureApplyWorkflow) Apply(_ context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	w.mu.Lock()
	w.cmds = append(w.cmds, cmd)
	w.mu.Unlock()
	return &workflow.ApplyResult{Movie: cmd.Movie}, nil
}

func (w *captureApplyWorkflow) commands() []workflow.ApplyCmd {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]workflow.ApplyCmd(nil), w.cmds...)
}

func TestApplyPhase_RunSharesDedupAcrossApplyCommands(t *testing.T) {
	wf := &captureApplyWorkflow{}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency.MaxWorkers = 2
	inputs.Results = map[string]*resultstore.MovieResult{
		"/source/one.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/source/one.mp4", MovieID: "TEST-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "TEST-001"},
		},
		"/source/two.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/source/two.mp4", MovieID: "TEST-002"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "TEST-002"},
		},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		Download:               true,
		OverwriteExistingMedia: true,
	})

	commands := wf.commands()
	require.Len(t, commands, 2)
	assert.NotNil(t, commands[0].Dedup)
	assert.Same(t, commands[0].Dedup, commands[1].Dedup)
	assert.True(t, commands[0].OverwriteExistingMedia)
	assert.True(t, commands[1].OverwriteExistingMedia)
}
