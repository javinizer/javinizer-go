package batch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

// This observer only reads the publicly published running result, then delegates
// to the real workflow. It neither manufactures an error nor edits result state.
type observeRunningResultWorkflow struct {
	workflow.WorkflowInterface
	observe func()
}

func (w *observeRunningResultWorkflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	w.observe()
	return w.WorkflowInterface.Scrape(ctx, cmd)
}

func TestFailedRealScrapeKeepsPublishedResultIDResolvable(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".mp4") // Valid owned file, but no basename identity.
	require.NoError(t, os.WriteFile(file, []byte("unchanged input"), 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join(dir, "work")
	deps := testkit.CreateTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	snapshot := rt.Snapshot()
	factory := snapshot.BatchJobFactory()
	require.NotNil(t, factory)
	wf, err := snapshot.BatchWorkflow("failed-result-identity")
	require.NoError(t, err)
	type observed struct {
		results []resultstore.MovieResult
		indexed bool
	}
	observations := make(chan observed, 1)
	var job worker.BatchJobInterface
	observer := &observeRunningResultWorkflow{WorkflowInterface: wf, observe: func() {
		results := job.GetResults()
		indexed := false
		if len(results) == 1 {
			_, _, indexed = job.GetFileResultByResultID(results[0].ResultID)
		}
		observations <- observed{results, indexed}
	}}
	job = factory.CreateJob([]string{file}, worker.BatchJobOptions{WF: observer})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, job.StartScrape(ctx, []string{file}, worker.ScrapePhaseConfig{}))
	settled := make(chan struct{})
	var phaseErr error
	go func() { phaseErr = job.Wait(); close(settled) }()
	// Registered after testkit setup: cancellation and join run before DB close.
	// The closed channel can be joined again after either success or an assertion.
	t.Cleanup(func() { cancel(); <-settled })
	select {
	case <-settled:
		require.NoError(t, phaseErr, "a settled batch phase may contain per-file failures")
	case <-ctx.Done():
		t.Fatal("real scrape did not settle")
	}
	var running observed
	select {
	case running = <-observations:
	default:
		t.Fatal("real workflow was never entered")
	}
	require.Len(t, running.results, 1)
	require.Equal(t, models.JobStatusRunning, running.results[0].Status)
	require.NotEmpty(t, running.results[0].ResultID)
	require.True(t, running.indexed, "running stable ID is initially addressable")
	results := job.GetResults()
	require.Len(t, results, 1)
	failed := results[0]
	require.Equal(t, models.JobStatusFailed, failed.Status)
	require.Contains(t, failed.Error, "empty MovieID")
	require.Empty(t, failed.FileMatchInfo.MovieID)
	require.Equal(t, running.results[0].ResultID, failed.ResultID)
	require.Greater(t, failed.Revision, running.results[0].Revision)
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "unchanged input", string(data))
	resolved, path, found := job.GetFileResultByResultID(failed.ResultID)
	require.True(t, found, "a published failed result must remain addressable by its stable ResultID")
	require.Equal(t, file, path)
	require.Equal(t, failed, *resolved)
}
