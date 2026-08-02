package batch

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStartScrapeAsyncLaunch_TrackedAndJoined mirrors
// TestUpdateAsyncLaunch_TrackedAndJoined for the scrape path: the usecase
// launches the phase on a TRACKED goroutine that additionally Wait()s (via
// phaseDone), so test teardown's drain joins the whole phase — including its
// deferred persistence — rather than just the launch call.
func TestStartScrapeAsyncLaunch_TrackedAndJoined(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)

	out, err := StartScrapeUseCase(context.Background(), rt, StartScrapeInput{})
	require.NoError(t, err)
	require.NotEmpty(t, out.JobID)

	require.True(t, rt.WaitBackgroundTasks(10*time.Second),
		"usecase-launched scrape must be tracked and joinable")

	job, ok := deps.GetJobStore().GetBatchJob(out.JobID)
	require.True(t, ok)
	assert.NotEqual(t, "Running", string(job.GetStatus().Status),
		"after join the job must have left the Running state")
}

// TestLaunchJobScrape_LogsAndReturnsError covers the helper's error arm —
// unreachable through StartScrapeUseCase (fresh Pending jobs can't fail to
// start), so it is exercised directly with a job in the wrong state.
func TestLaunchJobScrape_LogsAndReturnsError(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-900.mp4"})
	setJobStatus(job, models.JobStatusRunning)
	defer setJobStatus(job, models.JobStatusCompleted) // restore terminal status for teardown hygiene (setup-only Running state)

	err := launchJobScrape(job.Controller(), context.Background(), []string{}, worker.ScrapePhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot start")
}

// TestLaunchJobScrape_Success covers the happy arm: the phase starts and can
// be joined to completion.
func TestLaunchJobScrape_Success(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{})
	require.NoError(t, launchJobScrape(job.Controller(), context.Background(), []string{}, worker.ScrapePhaseConfig{}))

	require.NoError(t, job.Controller().Wait())
	assert.NotEqual(t, models.JobStatusRunning, job.Lifecycle().GetJobStatus())
}

// TestStartScrapeAsyncLaunch_WaitErrorPath drives a scrape that ends in a
// failed/cancelled terminal state so the tracked goroutine's Wait-error arm
// (logging.Warnf) executes. The movie file does not exist, so every per-file
// scrape fails and the phase terminates unsuccessfully.
func TestStartScrapeAsyncLaunch_WaitErrorPath(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{"/tmp"}
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)

	out, err := StartScrapeUseCase(context.Background(), rt, StartScrapeInput{
		Files: []string{"/tmp/does-not-exist-ZZZ-999.mp4"},
	})
	if err != nil {
		// File validation may reject the input pre-launch; in that case this
		// test cannot drive the Wait-error arm and is skipped deliberately.
		t.Skipf("usecase rejected input before launch: %v", err)
	}

	require.True(t, rt.WaitBackgroundTasks(10*time.Second),
		"tracked scrape goroutine must complete even for failed jobs")

	job, ok := deps.GetJobStore().GetBatchJob(out.JobID)
	require.True(t, ok)
	status := job.GetStatus().Status
	assert.NotEqual(t, models.JobStatusRunning, status,
		"failed scrape must reach a terminal state")
}
