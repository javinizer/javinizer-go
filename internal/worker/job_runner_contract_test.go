package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// Embedding the unused control capabilities makes an unexpected call fail rather
// than silently allowing the runner to perform work outside its orchestration seam.
type runnerContractJob struct {
	ControlledJob
	status                        *BatchJobStatus
	calls                         []string
	scrapeStartErr, applyStartErr error
	waitErrors                    []error
	waits                         int
	scrapeContext, applyContext   context.Context
	files                         []string
	scrapeConfig                  ScrapePhaseConfig
	applyConfig                   ApplyPhaseConfig
}

func (j *runnerContractJob) GetStatus() *BatchJobStatus {
	j.calls = append(j.calls, "status")
	return j.status
}
func (j *runnerContractJob) StartScrape(ctx context.Context, files []string, cfg ScrapePhaseConfig) error {
	j.calls = append(j.calls, "scrape")
	j.scrapeContext, j.files, j.scrapeConfig = ctx, append([]string(nil), files...), cfg
	return j.scrapeStartErr
}
func (j *runnerContractJob) StartApply(ctx context.Context, cfg ApplyPhaseConfig) error {
	j.calls = append(j.calls, "apply")
	j.applyContext, j.applyConfig = ctx, cfg
	return j.applyStartErr
}
func (j *runnerContractJob) Wait() error {
	j.calls = append(j.calls, "wait")
	err := j.waitErrors[j.waits]
	j.waits++
	// Results become available only after a successful scrape has settled.
	if j.waits == 1 && err == nil {
		j.status.Results = map[string]*resultstore.MovieResult{
			j.status.Files[0]: {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "RUN-001"}},
		}
	}
	return err
}

func TestJobRunnerStopsAtFailedPhaseBoundary(t *testing.T) {
	cause := errors.New("phase boundary refused")
	for _, tc := range []struct {
		name                          string
		empty                         bool
		scrapeStartErr, applyStartErr error
		waitErrors                    []error
		wantError                     string
		wantCalls                     []string
	}{
		{name: "empty job", empty: true, wantCalls: []string{"status"}},
		{name: "scrape refuses to start", scrapeStartErr: cause, wantError: "scrape phase failed to start: phase boundary refused", wantCalls: []string{"status", "scrape"}},
		{name: "scrape completion fails", waitErrors: []error{cause}, wantError: "scrape phase failed: phase boundary refused", wantCalls: []string{"status", "scrape", "wait"}},
		{name: "apply refuses to start", applyStartErr: cause, waitErrors: []error{nil}, wantError: "apply phase failed to start: phase boundary refused", wantCalls: []string{"status", "scrape", "wait", "status", "apply"}},
		{name: "apply completion fails", waitErrors: []error{nil, cause}, wantError: "apply phase failed: phase boundary refused", wantCalls: []string{"status", "scrape", "wait", "status", "apply", "wait"}},
		{name: "both phases settle", waitErrors: []error{nil, nil}, wantCalls: []string{"status", "scrape", "wait", "status", "apply", "wait"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := &BatchJobStatus{}
			status.ID = "runner-contract"
			status.Destination = "output"
			if !tc.empty {
				status.Files = []string{"RUN-001.mp4"}
			}
			job := &runnerContractJob{status: status, scrapeStartErr: tc.scrapeStartErr, applyStartErr: tc.applyStartErr, waitErrors: tc.waitErrors}
			ctx := t.Context()
			err := NewJobRunner(job, BatchJobConfig{MaxWorkers: 1, ScraperPriority: []string{"fixture"}, NFOEnabled: true}).Run(ctx)
			if tc.wantError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.wantError)
				require.ErrorIs(t, err, cause, "phase context must preserve the original cause")
			}
			require.Equal(t, tc.wantCalls, job.calls, "no phase or wait may run after a failed boundary")
			if !tc.empty {
				require.Same(t, ctx, job.scrapeContext)
				require.Equal(t, status.Files, job.files)
				require.Equal(t, []string{"fixture"}, job.scrapeConfig.SelectedScrapers)
			}
			if job.applyContext != nil {
				require.Same(t, ctx, job.applyContext)
				require.Equal(t, "output", job.applyConfig.Destination)
				require.False(t, job.applyConfig.OrganizeOptions.ForceUpdate, "default orchestration must not authorize overwriting destination video")
			}
		})
	}
}
