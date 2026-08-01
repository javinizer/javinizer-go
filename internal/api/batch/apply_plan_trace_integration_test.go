package batch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
)

func TestStartScrapeUseCase_InvalidPlanAndDeniedDestination(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	root := t.TempDir()
	cfg.API.Security.AllowedDirectories = []string{root}
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)

	_, err := StartScrapeUseCase(context.Background(), rt, StartScrapeInput{
		ApplyPlan: &applyplan.Plan{Version: 2},
	})
	assert.ErrorContains(t, err, "invalid apply plan")

	_, err = StartScrapeUseCase(context.Background(), rt, StartScrapeInput{
		ApplyPlan: applyplan.Default(applyplan.VideoOperationOrganize, "/outside"),
	})
	assert.ErrorContains(t, err, "access denied")
}

func TestApplyPlanTrace_RequestPersistenceAndEffectiveConfig(t *testing.T) {
	tests := []struct {
		name          string
		operation     applyplan.VideoOperation
		wantUpdate    bool
		wantMode      operationmode.OperationMode
		wantApplySkip bool
		wantForceName bool
	}{
		{"organize", applyplan.VideoOperationOrganize, false, operationmode.OperationModeOrganize, false, false},
		{"rename in place", applyplan.VideoOperationRenameInPlace, false, operationmode.OperationModeInPlace, false, true},
		{"rename file", applyplan.VideoOperationRenameFile, false, operationmode.OperationModeInPlaceNoRenameFolder, false, true},
		{"leave in place", applyplan.VideoOperationLeaveInPlace, true, operationmode.OperationModeMetadataArtwork, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			inputDir := filepath.Join(root, "input")
			outputDir := filepath.Join(root, "output")
			require.NoError(t, os.MkdirAll(inputDir, 0o755))
			require.NoError(t, os.MkdirAll(outputDir, 0o755))
			filePath := filepath.Join(inputDir, "IPX-123.mp4")
			require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

			cfg := config.DefaultConfig(nil, nil)
			cfg.API.Security.AllowedDirectories = []string{root}
			cfg.Output.Download.DownloadCover = false
			cfg.Output.Download.DownloadPoster = false
			cfg.Output.Download.DownloadTrailer = false
			cfg.Output.Download.DownloadExtrafanart = false
			deps := createTestDeps(t, cfg, "")
			rt := testkit.GetTestRuntime(deps)

			destination := ""
			if tc.operation == applyplan.VideoOperationOrganize {
				destination = outputDir
			}
			plan := applyplan.Default(tc.operation, destination)
			out, err := StartScrapeUseCase(context.Background(), rt, StartScrapeInput{
				Files: []string{filePath}, ApplyPlan: plan,
			})
			require.NoError(t, err)
			require.NotEmpty(t, out.JobID)

			// Wait for the job to reach a terminal status so async worker
			// goroutines finish before t.TempDir cleanup runs (otherwise
			// RemoveAll fails with "directory not empty").
			t.Cleanup(func() {
				for i := 0; i < 100; i++ {
					job, ok := deps.JobStore.GetBatchJob(out.JobID)
					if !ok {
						break
					}
					if s := job.GetStatus().Status; s == models.JobStatusCompleted || s == models.JobStatusOrganized || s == models.JobStatusFailed || s == models.JobStatusCancelled {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				rt.Shutdown()
			})

			row, err := deps.Repos.JobRepo.FindByID(context.Background(), out.JobID)
			require.NoError(t, err)
			persisted, decodeErrs := jobpersist.Decode(row)
			require.Empty(t, decodeErrs)
			require.NotNil(t, persisted.ApplyPlan)
			assert.Equal(t, plan, persisted.ApplyPlan)
			assert.Equal(t, tc.wantUpdate, persisted.Update)
			assert.Equal(t, tc.wantMode, persisted.OperationModeOverride)

			job := &stubControlledJob{status: statusWithPlan(t, persisted.ApplyPlan)}
			factory := rt.Snapshot().BatchJobFactory()
			if tc.operation == applyplan.VideoOperationLeaveInPlace {
				applyCfg, applyErr := resolveUpdateApplyConfig(rt.Snapshot(), factory, job, contracts.UpdateRequest{})
				require.NoError(t, applyErr)
				assert.True(t, applyCfg.OrganizeOptions.Skip)
				assert.Empty(t, applyCfg.Destination)
				return
			}
			applyCfg, applyErr := resolveOrganizeApplyConfig(rt.Snapshot(), factory, job, contracts.OrganizeRequest{})
			require.NoError(t, applyErr)
			assert.Equal(t, tc.wantApplySkip, applyCfg.OrganizeOptions.Skip)
			assert.Equal(t, tc.wantForceName, applyCfg.OrganizeOptions.ForceRenameFile)
			assert.Equal(t, tc.wantMode, applyCfg.OperationModeOverride)
			assert.Equal(t, destination, applyCfg.Destination)
		})
	}
}
