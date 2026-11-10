package batch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
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

// A source barrier ensures cancellation happens after the real scrape starts,
// rather than racing admission or relying on per-file errors to fail the job.
type cancellationJoinScraper struct {
	noPosterStubScraper
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
}

func (s *cancellationJoinScraper) Search(ctx context.Context, _ string) (*models.ScraperResult, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
	return nil, ctx.Err()
}

func TestStartScrapeAsyncLaunch_WaitErrorPath(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "IPX-900.mp4")
	require.NoError(t, os.WriteFile(file, []byte("video"), 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	source := &cancellationJoinScraper{
		entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}),
	}
	cfg.Scrapers.Priority = []string{source.Name()}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(source)
	rt := testkit.GetTestRuntime(deps)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(source.release) }) }
	// Release the barrier even if an assertion fails, before runtime teardown.
	defer release()

	out, err := StartScrapeUseCase(context.Background(), rt, StartScrapeInput{
		Files: []string{file}, SelectedScrapers: []string{source.Name()},
	})
	require.NoError(t, err)
	require.NotEmpty(t, out.JobID)
	job, ok := deps.GetJobStore().GetBatchJob(out.JobID)
	require.True(t, ok)
	defer job.Cancel()
	select {
	case <-source.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("admitted scrape did not reach the source")
	}
	require.Equal(t, models.JobStatusRunning, job.GetStatus().Status)
	job.Cancel()
	select {
	case <-source.cancelled:
	case <-time.After(10 * time.Second):
		t.Fatal("job cancellation did not reach the source context")
	}
	// Cancellation alone is not completion: the tracked task must retain
	// ownership until the source returns and phase persistence has finished.
	require.False(t, rt.WaitBackgroundTasks(10*time.Millisecond),
		"tracked scrape must not finish while its source is still blocked")
	release()
	require.True(t, rt.WaitBackgroundTasks(10*time.Second),
		"tracked scrape must join the cancelled phase and its final persistence")
	require.EqualError(t, job.Wait(), "job "+out.JobID+" cancelled")
	status := job.GetStatus()
	require.Equal(t, models.JobStatusCancelled, status.Status)
	require.Empty(t, status.CurrentPhase)
	require.Zero(t, status.Completed)
	require.Len(t, status.Results, 1)
	for _, result := range status.Results {
		require.NotEqual(t, models.JobStatusCompleted, result.Status)
		require.Nil(t, result.Movie)
	}
	row, err := deps.Repos.JobRepo.FindByID(context.Background(), out.JobID)
	require.NoError(t, err)
	persisted, decodeErrors := jobpersist.Decode(row)
	require.Empty(t, decodeErrors)
	require.Equal(t, models.JobStatusCancelled, persisted.Status)
	require.Empty(t, persisted.CurrentPhase)
	require.Len(t, persisted.Results, 1)
	persistedResult := persisted.Results[file]
	require.NotNil(t, persistedResult)
	require.Equal(t, status.Results[file].Status, persistedResult.Status)
	require.Equal(t, status.Results[file].ResultID, persistedResult.ResultID)
	require.Nil(t, persistedResult.Movie)
	require.NotNil(t, persistedResult.EndedAt,
		"joining the tracked task must include final result persistence")
	var movies int64
	require.NoError(t, deps.Repos.DB.Model(&models.Movie{}).Count(&movies).Error)
	require.Zero(t, movies, "cancelled scrape must not commit partial movie metadata")
	require.NoError(t, deps.JobStore.DeleteJob(out.JobID),
		"joined scrape must release its admission lease so the job can be deleted")
}
