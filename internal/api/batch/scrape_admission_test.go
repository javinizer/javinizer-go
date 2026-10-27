package batch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOfflineEvidenceScrapeAdmissionErr(t *testing.T) {
	t.Run("root cancellation precedes linked contexts", func(t *testing.T) {
		rt := core.NewAPIRuntime(nil)
		// Deliberately still-live contexts model the legal propagation interval:
		// a cancelled root does not imply every descendant already reports Err.
		request, cancelRequest := context.WithCancel(t.Context())
		defer cancelRequest()
		batch, cancelBatch := context.WithCancel(t.Context())
		defer cancelBatch()
		rt.Shutdown()
		require.NoError(t, request.Err())
		require.NoError(t, batch.Err())
		require.ErrorIs(t, offlineEvidenceScrapeAdmissionErr(rt, true, request, batch), context.Canceled)
	})

	t.Run("operation cancellation remains authoritative", func(t *testing.T) {
		rt := core.NewAPIRuntime(nil)
		defer rt.Shutdown()
		live := t.Context()
		cancelled, cancel := context.WithCancel(live)
		cancel()
		deadline, stop := context.WithDeadline(live, time.Unix(0, 0))
		defer stop()
		require.NoError(t, offlineEvidenceScrapeAdmissionErr(rt, true, live, live))
		require.ErrorIs(t, offlineEvidenceScrapeAdmissionErr(rt, true, cancelled, live), context.Canceled)
		require.ErrorIs(t, offlineEvidenceScrapeAdmissionErr(rt, true, live, cancelled), context.Canceled)
		require.ErrorIs(t, offlineEvidenceScrapeAdmissionErr(rt, true, deadline, live), context.DeadlineExceeded)
		require.NoError(t, rt.ServerCtx().Err())
	})

	t.Run("opt out does not inspect runtime or contexts", func(t *testing.T) {
		require.NoError(t, offlineEvidenceScrapeAdmissionErr(nil, false, nil))
	})
}

func awaitScrapeAdmissionSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestStartScrapeUseCaseShutdownDuringCreate(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		name := "durable cancellation"
		if failSave {
			name = "cancellation persistence error"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "EVIDENCE-001.mp4")
			require.NoError(t, os.WriteFile(file, []byte("fixture"), 0600))
			cfg := config.DefaultConfig(nil, nil)
			cfg.System.TempDir = t.TempDir()
			cfg.API.Security.AllowedDirectories = []string{dir}
			cfg.Scrapers.Priority = []string{"stub-no-poster"}
			deps := createTestDeps(t, cfg, "")
			rt := testkit.GetTestRuntime(deps)
			source := &evidenceBatchSource{}
			deps.CoreDeps.GetRegistry().RegisterInstance(source)
			entered, resume := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(resume) }) }
			var blocked atomic.Bool
			const createCallback = "scrape_admission_create_barrier"
			require.NoError(t, deps.Repos.DB.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
				if tx.Statement.Table == "jobs" && blocked.CompareAndSwap(false, true) {
					close(entered)
					<-resume
				}
			}))
			defer func() { _ = deps.Repos.DB.Callback().Create().Remove(createCallback) }()
			saveErr := errors.New("fixture cancellation persistence unavailable")
			var failedSaves atomic.Int32
			if failSave {
				const saveCallback = "scrape_admission_cancel_save_failure"
				require.NoError(t, deps.Repos.DB.Callback().Update().Before("gorm:update").Register(saveCallback, func(tx *gorm.DB) {
					if _, savingJob := tx.Statement.Dest.(*models.Job); tx.Statement.Table == "jobs" && savingJob {
						failedSaves.Add(1)
						_ = tx.AddError(saveErr)
					}
				}))
				defer func() { _ = deps.Repos.DB.Callback().Update().Remove(saveCallback) }()
			}
			returned, shutdown := make(chan struct{}), make(chan struct{})
			var shutdownOnce sync.Once
			startShutdown := func() { shutdownOnce.Do(func() { go func() { rt.Shutdown(); close(shutdown) }() }) }
			defer func() {
				release()
				startShutdown()
				awaitScrapeAdmissionSignal(t, returned, "usecase cleanup")
				awaitScrapeAdmissionSignal(t, shutdown, "shutdown cleanup")
			}()
			var output *StartScrapeOutput
			var resultErr error
			go func() {
				output, resultErr = StartScrapeUseCase(t.Context(), rt, StartScrapeInput{
					ShowOfflineEvidence: true, Files: []string{file}, SelectedScrapers: []string{source.Name()},
				})
				close(returned)
			}()
			awaitScrapeAdmissionSignal(t, entered, "job persistence barrier")
			startShutdown()
			awaitScrapeAdmissionSignal(t, rt.ServerCtx().Done(), "root cancellation")
			require.ErrorIs(t, rt.ServerCtx().Err(), context.Canceled)
			select {
			case <-shutdown:
				t.Fatal("shutdown escaped admitted persistence")
			default:
			}
			release()
			awaitScrapeAdmissionSignal(t, returned, "usecase return")
			awaitScrapeAdmissionSignal(t, shutdown, "shutdown join")
			require.Nil(t, output)
			require.Zero(t, source.calls.Load())
			var jobs []models.Job
			require.NoError(t, deps.Repos.DB.Find(&jobs).Error)
			require.Len(t, jobs, 1)
			if failSave {
				require.ErrorIs(t, resultErr, saveErr)
				require.ErrorContains(t, resultErr, "cancel admitted job")
				require.Positive(t, failedSaves.Load())
				require.Equal(t, models.JobStatusPending, jobs[0].Status)
			} else {
				require.ErrorIs(t, resultErr, context.Canceled)
				require.Equal(t, models.JobStatusCancelled, jobs[0].Status)
			}
			job, ok := deps.JobStore.GetBatchJob(jobs[0].ID)
			require.True(t, ok)
			require.Equal(t, models.JobStatusCancelled, job.GetStatus().Status)
			require.True(t, rt.WaitBackgroundTasks(time.Second), "both admission leases must be released")
		})
	}
}

type admissionBlockingSource struct {
	noPosterStubScraper
	calls   atomic.Int32
	entered chan context.Context
	resume  chan struct{}
}

func (s *admissionBlockingSource) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	s.calls.Add(1)
	s.entered <- ctx
	select {
	case <-s.resume:
		return s.noPosterStubScraper.Search(ctx, id)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestStartScrapeUseCaseAcceptedJobOutlivesRequest(t *testing.T) {
	for _, requested := range []bool{false, true} {
		name := "default off"
		if requested {
			name = "offline evidence requested"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "EVIDENCE-001.mp4")
			require.NoError(t, os.WriteFile(file, []byte("fixture"), 0600))
			cfg := config.DefaultConfig(nil, nil)
			cfg.System.TempDir = t.TempDir()
			cfg.API.Security.AllowedDirectories = []string{dir}
			cfg.Scrapers.Priority = []string{"stub-no-poster"}
			deps := createTestDeps(t, cfg, "")
			rt := testkit.GetTestRuntime(deps)
			source := &admissionBlockingSource{entered: make(chan context.Context, 1), resume: make(chan struct{})}
			deps.CoreDeps.GetRegistry().RegisterInstance(source)
			var once sync.Once
			release := func() { once.Do(func() { close(source.resume) }) }
			defer func() { release(); rt.Shutdown() }()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			output, err := StartScrapeUseCase(ctx, rt, StartScrapeInput{
				ShowOfflineEvidence: requested, Files: []string{file}, SelectedScrapers: []string{source.Name()},
			})
			require.NoError(t, err)
			require.NotNil(t, output)
			var sourceCtx context.Context
			select {
			case sourceCtx = <-source.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("accepted job never reached source")
			}
			cancel()
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.NoError(t, sourceCtx.Err(), "accepted batch must not inherit request cancellation")
			release()
			job, ok := deps.JobStore.GetBatchJob(output.JobID)
			require.True(t, ok)
			require.NoError(t, job.Wait())
			require.True(t, rt.WaitBackgroundTasks(5*time.Second))
			require.Equal(t, int32(1), source.calls.Load())
			persisted, err := deps.Repos.JobRepo.FindByID(t.Context(), output.JobID)
			require.NoError(t, err)
			require.Equal(t, models.JobStatusCompleted, persisted.Status)
		})
	}
}
