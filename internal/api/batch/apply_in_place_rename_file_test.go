package batch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// Issue #226, spec scenario "folder-only in-place rename via web batch apply":
// a persisted rename-in-place apply plan + global rename_file=false must apply
// as folder-only rename — folder renamed to folder_format, video file name
// preserved. The apply config is resolved through the real plan projection
// (resolveOrganizeApplyConfig) and the job's apply phase runs against a real
// (temp-dir) filesystem, mirroring the web batch/apply path end to end.
func TestApplyPlan_RenameInPlace_HonorsRenameFileFalse_FolderOnlyRename(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Operation.RenameFile = false
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{}
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Feature.Enabled = false

	deps := createTestDeps(t, cfg, "")

	// Dedicated folder: contains only the one movie's video, so the in-place
	// folder rename is eligible (strategy_inplace.go dedicated-folder gate).
	root := t.TempDir()
	sourceFolder := filepath.Join(root, "old folder")
	require.NoError(t, os.MkdirAll(sourceFolder, 0o755))
	filePath := filepath.Join(sourceFolder, "IPX-777-anything.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, Name: "IPX-777-anything.mp4", MovieID: "IPX-777", Extension: ".mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777", ContentID: "ipx777", Title: "Movie"},
	})

	// Resolve the apply config through the real projection, as the HTTP apply
	// endpoint does, from a persisted rename-in-place plan.
	plan := applyplan.Default(applyplan.VideoOperationRenameInPlace, "")
	rt := testkit.GetTestRuntime(deps)
	jobWithPlan := &stubControlledJob{status: statusWithPlan(t, plan)}
	factory := rt.Snapshot().BatchJobFactory()
	applyCfg, err := resolveOrganizeApplyConfig(rt.Snapshot(), factory, jobWithPlan, contracts.OrganizeRequest{})
	require.NoError(t, err)
	require.False(t, applyCfg.OrganizeOptions.ForceRenameFile, "rename-in-place must not force file rename (#226)")
	require.Equal(t, operationmode.OperationModeInPlace, applyCfg.OperationModeOverride)

	// Run the apply phase with the projection-resolved config.
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
	wfFactory, err := workflow.NewWorkflowFactory(fc)
	require.NoError(t, err)
	wf, err := wfFactory.NewWorkflow(job.ID.String())
	require.NoError(t, err)
	job.Controller().SetWorkflow(wf)
	job.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	setJobStatus(job, models.JobStatusCompleted)
	require.NoError(t, job.Controller().StartApply(context.Background(), applyCfg))
	require.NoError(t, job.Controller().Wait())

	status := job.GetStatus()
	require.Equal(t, models.JobStatusOrganized, status.Status, "apply should complete: %#v", status)

	// Folder renamed to <ID>; original file name preserved.
	assert.FileExists(t, filepath.Join(root, "IPX-777", "IPX-777-anything.mp4"))
	_, statErr := os.Stat(sourceFolder)
	assert.True(t, os.IsNotExist(statErr), "legacy folder name should be renamed away")

	// Contrapositive guard for the default path: rename_file=true renames both.
	fs2root := t.TempDir()
	source2 := filepath.Join(fs2root, "another folder")
	require.NoError(t, os.MkdirAll(source2, 0o755))
	file2 := filepath.Join(source2, "IPX-778-anything.mp4")
	require.NoError(t, os.WriteFile(file2, []byte("video"), 0o644))

	cfg.Output.Operation.RenameFile = true
	fc2, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
	wfFactory2, err := workflow.NewWorkflowFactory(fc2)
	require.NoError(t, err)
	job2 := deps.JobStore.CreateJobBatch([]string{file2})
	setJobResult(job2, file2, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: file2, Name: "IPX-778-anything.mp4", MovieID: "IPX-778", Extension: ".mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-778", ContentID: "ipx778", Title: "Movie 2"},
	})
	// Re-resolve a fresh apply config for the second job: half one's applyCfg
	// carries broadcasters bound to the stub job identity, so re-resolving keeps
	// attribution honest rather than reusing cross-job plumbing.
	job2WithPlan := &stubControlledJob{status: statusWithPlan(t, plan)}
	applyCfg2, err := resolveOrganizeApplyConfig(rt.Snapshot(), factory, job2WithPlan, contracts.OrganizeRequest{})
	require.NoError(t, err)
	require.False(t, applyCfg2.OrganizeOptions.ForceRenameFile)
	wf2, err := wfFactory2.NewWorkflow(job2.ID.String())
	require.NoError(t, err)
	job2.Controller().SetWorkflow(wf2)
	job2.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	setJobStatus(job2, models.JobStatusCompleted)
	require.NoError(t, job2.Controller().StartApply(context.Background(), applyCfg2))
	require.NoError(t, job2.Controller().Wait())
	require.Equal(t, models.JobStatusOrganized, job2.GetStatus().Status)
	assert.FileExists(t, filepath.Join(fs2root, "IPX-778", "IPX-778.mp4"))
}
