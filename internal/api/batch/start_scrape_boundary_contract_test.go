package batch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/stretchr/testify/require"
)

func TestStartScrapeUseCaseDiscoversSiblingOnceAndPropagatesManualInput(t *testing.T) {
	root := t.TempDir()
	parts := []string{
		filepath.Join(root, "IPX-535-pt1.mp4"),
		filepath.Join(root, "IPX-535-pt2.mp4"),
		filepath.Join(root, "IPX-535-pt3.mp4"),
	}
	unrelated := filepath.Join(root, "IPX-999-pt1.mp4")
	for _, path := range append(append([]string{}, parts...), unrelated) {
		require.NoError(t, os.WriteFile(path, []byte("video"), 0o644))
	}
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	cfg.API.Security.MaxFilesPerScan = 100
	scraper := &noPosterStubScraper{}
	cfg.Scrapers.Priority = []string{scraper.Name()}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(scraper)
	rt := testkit.GetTestRuntime(deps)
	const manualID = "IPX-777"
	manualInputs := map[string]string{parts[0]: manualID, parts[1]: manualID}

	// Two submitted parts share a directory. Discovery must append only the
	// missing third part, retaining the manual ID and multipart metadata.
	out, err := StartScrapeUseCase(context.Background(), rt, StartScrapeInput{
		Files:            parts[:2],
		ManualInputs:     manualInputs,
		SelectedScrapers: []string{scraper.Name()},
	})
	require.NoError(t, err)
	require.NotEmpty(t, out.JobID)
	require.True(t, rt.WaitBackgroundTasks(10*time.Second), "join scrape and final persistence before inspecting the job")
	job, ok := deps.JobStore.GetBatchJob(out.JobID)
	require.True(t, ok)
	status := job.GetStatus()
	require.Equal(t, models.JobStatusCompleted, status.Status)
	require.ElementsMatch(t, parts, status.Files, "each submitted or discovered part must occur exactly once")
	require.Equal(t, len(parts), status.TotalFiles)
	require.Equal(t, len(parts), status.Completed)
	require.Zero(t, status.Failed)
	require.Len(t, status.Results, len(parts))
	require.NotContains(t, status.Results, unrelated)
	for i, path := range parts {
		result := status.Results[path]
		require.NotNil(t, result)
		require.Equal(t, models.JobStatusCompleted, result.Status)
		require.Equal(t, manualID, result.FileMatchInfo.MovieID)
		require.True(t, result.FileMatchInfo.IsMultiPart)
		require.Equal(t, i+1, result.FileMatchInfo.PartNumber)
		require.NotNil(t, result.Movie)
		require.Equal(t, manualID, result.Movie.ID, "the actual scrape must receive the propagated manual input")
	}
	require.Equal(t, map[string]string{parts[0]: manualID, parts[1]: manualID}, manualInputs, "do not mutate caller-owned input")

	row, err := deps.Repos.JobRepo.FindByID(context.Background(), out.JobID)
	require.NoError(t, err)
	persisted, decodeErrors := jobpersist.Decode(row)
	require.Empty(t, decodeErrors)
	require.ElementsMatch(t, parts, persisted.Files)
	require.Len(t, persisted.Results, len(parts))
	for _, path := range parts {
		require.Equal(t, manualID, persisted.Results[path].FileMatchInfo.MovieID)
	}
}
