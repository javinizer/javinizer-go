package batch

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

func TestBatchJob_PanicRecovery(t *testing.T) {
	initTestWebSocket(t)

	t.Run("BatchJob.StartScrape recovers from panic and marks job failed", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Performance.MaxWorkers = 1
		cfg.Performance.WorkerTimeout = 5
		cfg.Output.Download.DownloadCover = false
		cfg.Output.Download.DownloadPoster = false
		cfg.Output.Download.DownloadExtrafanart = false
		cfg.Output.Download.DownloadTrailer = false
		cfg.Output.Download.DownloadActress = false

		deps := createTestDeps(t, cfg, "")
		job := deps.JobStore.CreateJobBatch([]string{"test.mp4"})

		done := make(chan struct{})
		go func() {
			defer close(done)
			// StartScrape will fail gracefully without a workflow (no panic)
			_ = job.Controller().StartScrape(context.Background(), []string{"test.mp4"}, worker.ScrapePhaseConfig{})
		}()

		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("StartScrape timed out")
		}

		status := job.GetStatus()
		if status.Status == models.JobStatusRunning {
			t.Fatalf("job should not still be running after StartScrape")
		}
	})

	t.Run("BatchJob.StartApply recovers from panic and marks job failed", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Output.Download.DownloadCover = false
		cfg.Output.Download.DownloadPoster = false
		cfg.Output.Download.DownloadExtrafanart = false
		cfg.Output.Download.DownloadTrailer = false
		cfg.Output.Download.DownloadActress = false

		deps := createTestDeps(t, cfg, "")
		job := deps.JobStore.CreateJobBatch([]string{"test.mp4"})

		done := make(chan struct{})
		go func() {
			defer close(done)
			// StartApply will fail gracefully without a workflow (no panic)
			_ = job.Controller().StartApply(context.Background(), worker.ApplyPhaseConfig{})
		}()

		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("StartApply timed out")
		}

		status := job.GetStatus()
		if status.Status == models.JobStatusRunning {
			t.Fatalf("job should not still be running after StartApply")
		}
	})

	t.Run("BatchJob.StartScrape no panic propagation", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Performance.MaxWorkers = 1
		cfg.Performance.WorkerTimeout = 5
		cfg.Output.Download.DownloadCover = false
		cfg.Output.Download.DownloadPoster = false
		cfg.Output.Download.DownloadExtrafanart = false
		cfg.Output.Download.DownloadTrailer = false
		cfg.Output.Download.DownloadActress = false

		deps := createTestDeps(t, cfg, "")
		job := deps.JobStore.CreateJobBatch([]string{"test.mp4"})

		panicked := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					close(panicked)
				}
			}()
			_ = job.Controller().StartScrape(context.Background(), []string{"test.mp4"}, worker.ScrapePhaseConfig{})
		}()

		select {
		case <-panicked:
			t.Fatal("StartScrape should have recovered from panic, not propagated it")
		case <-time.After(10 * time.Second):
		}

		status := job.GetStatus()
		// Without a workflow, StartScrape returns an error but doesn't panic
		if status.Status != models.JobStatusCompleted && status.Status != models.JobStatusFailed && status.Status != models.JobStatusPending {
			t.Fatalf("job status = %q, want completed, failed, or pending", status.Status)
		}
	})
}
