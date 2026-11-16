package batch

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestOfflineEvidenceStartAdmissionBoundaries(t *testing.T) {
	for _, shutdownBeforeAdmission := range []bool{true, false} {
		name := "request_canceled_after_evidence_admission"
		if shutdownBeforeAdmission {
			name = "shutdown_before_nested_admission"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "EVIDENCE-001.mp4")
			require.NoError(t, os.WriteFile(file, []byte("owned source"), 0600))
			cfg := config.DefaultConfig(nil, nil)
			cfg.System.TempDir = t.TempDir()
			cfg.API.Security.AllowedDirectories = []string{dir}
			cfg.Scrapers.Priority = []string{"stub-no-poster"}
			deps := createTestDeps(t, cfg, "")
			source := &evidenceBatchSource{}
			deps.CoreDeps.GetRegistry().RegisterInstance(source)
			ownedDeps := *deps
			rt := core.NewAPIRuntime(&ownedDeps)
			rt.SetConfig(cfg)
			t.Cleanup(rt.Shutdown)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			input := StartScrapeInput{Files: []string{file}, ShowOfflineEvidence: true}
			var calls, releases atomic.Int32
			shutdownDone := make(chan struct{})
			begin := func(parent context.Context, requested bool, count int) (context.Context, func(), error) {
				calls.Add(1)
				require.True(t, requested)
				require.Equal(t, 1, count)
				if shutdownBeforeAdmission {
					// Shutdown cancels the real root and closes admission, but must wait
					// for the outer request admission to release before it can finish.
					go func() { rt.Shutdown(); close(shutdownDone) }()
					select {
					case <-rt.ServerCtx().Done():
					case <-time.After(5 * time.Second):
						t.Fatal("shutdown did not cancel server")
					}
					select {
					case <-shutdownDone:
						t.Fatal("shutdown did not wait for admitted request")
					default:
					}
				}
				batchCtx, release, err := rt.BeginOfflineScrapeEvidenceForFiles(parent, requested, count)
				if err != nil {
					return batchCtx, release, err
				}
				// This is a real cancellation after successful evidence admission. The
				// operation's values and ownership are preserved, not fabricated.
				cancel()
				return batchCtx, func() { releases.Add(1); release() }, nil
			}
			output, err := startScrapeUseCase(ctx, rt, input, begin)
			if shutdownBeforeAdmission {
				require.ErrorIs(t, err, core.ErrOfflineEvidenceShuttingDown)
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Nil(t, output, "interrupted admission cannot acknowledge success")
			require.Equal(t, int32(1), calls.Load())
			if shutdownBeforeAdmission {
				select {
				case <-shutdownDone:
				case <-time.After(5 * time.Second):
					t.Fatal("outer admission leaked")
				}
				require.Zero(t, releases.Load(), "refused nested admission has no lease")
			} else {
				require.Equal(t, int32(1), releases.Load(), "nested evidence lease released exactly once")
			}
			require.True(t, rt.WaitBackgroundTasks(time.Second), "all runtime admissions released")
			require.Zero(t, source.calls.Load())
			require.Empty(t, deps.JobStore.ListJobs())
			var count int64
			require.NoError(t, deps.Repos.DB.Model(&models.Job{}).Count(&count).Error)
			require.Zero(t, count, "no durable job created")
			contents, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, "owned source", string(contents))
			if !shutdownBeforeAdmission {
				// The canceled request must not poison the live runtime or prevent a
				// later request from acquiring and releasing its own evidence lease.
				output, err = StartScrapeUseCase(t.Context(), rt, input)
				require.NoError(t, err)
				require.NotEmpty(t, output.JobID)
				require.True(t, rt.WaitBackgroundTasks(5*time.Second))
				require.Positive(t, source.calls.Load())
				require.Len(t, deps.JobStore.ListJobs(), 1)
			}
		})
	}
}
