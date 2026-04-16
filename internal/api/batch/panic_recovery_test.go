package batch

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/worker"
)

func TestBatchJob_PanicRecovery(t *testing.T) {
	initTestWebSocket(t)

	t.Run("processBatchJob recovers from panic and marks job failed", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Performance.MaxWorkers = 1
		cfg.Performance.WorkerTimeout = 5
		cfg.Output.DownloadCover = false
		cfg.Output.DownloadPoster = false
		cfg.Output.DownloadExtrafanart = false
		cfg.Output.DownloadTrailer = false
		cfg.Output.DownloadActress = false

		deps := createTestDeps(t, cfg, "")
		job := deps.JobQueue.CreateJob([]string{"test.mp4"})

		done := make(chan struct{})
		go func() {
			defer close(done)
			processBatchJob(&BatchProcessOptions{
				Job:        job,
				JobQueue:   deps.JobQueue,
				Registry:   deps.Registry,
				Aggregator: deps.Aggregator,
				MovieRepo:  deps.MovieRepo,
				Matcher:    deps.Matcher,
				Cfg:        cfg,
				DB:         deps.DB,
			})
		}()

		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("processBatchJob timed out")
		}

		status := job.GetStatus()
		if status.Status == worker.JobStatusRunning {
			t.Fatalf("job should not still be running after processBatchJob")
		}
	})

	t.Run("processUpdateJob recovers from panic and marks job failed", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.DownloadCover = false
		cfg.Output.DownloadPoster = false
		cfg.Output.DownloadExtrafanart = false
		cfg.Output.DownloadTrailer = false
		cfg.Output.DownloadActress = false

		deps := createTestDeps(t, cfg, "")
		job := deps.JobQueue.CreateJob([]string{"test.mp4"})

		done := make(chan struct{})
		go func() {
			defer close(done)
			processUpdateJob(job, cfg, deps.DB, deps.Registry, nil, nil)
		}()

		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("processUpdateJob timed out")
		}

		status := job.GetStatus()
		if status.Status == worker.JobStatusRunning {
			t.Fatalf("job should not still be running after processUpdateJob")
		}
	})

	t.Run("processBatchJob panic recovery sets failed status", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Performance.MaxWorkers = 1
		cfg.Performance.WorkerTimeout = 5
		cfg.Output.DownloadCover = false
		cfg.Output.DownloadPoster = false
		cfg.Output.DownloadExtrafanart = false
		cfg.Output.DownloadTrailer = false
		cfg.Output.DownloadActress = false

		deps := createTestDeps(t, cfg, "")
		job := deps.JobQueue.CreateJob([]string{"test.mp4"})

		panicked := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					close(panicked)
				}
			}()
			processBatchJob(&BatchProcessOptions{
				Job:        job,
				JobQueue:   deps.JobQueue,
				Registry:   deps.Registry,
				Aggregator: deps.Aggregator,
				MovieRepo:  deps.MovieRepo,
				Matcher:    deps.Matcher,
				Cfg:        cfg,
				DB:         deps.DB,
			})
		}()

		select {
		case <-panicked:
			t.Fatal("processBatchJob should have recovered from panic, not propagated it")
		case <-time.After(10 * time.Second):
		}

		status := job.GetStatus()
		if status.Status != worker.JobStatusCompleted && status.Status != worker.JobStatusFailed {
			t.Fatalf("job status = %q, want completed or failed", status.Status)
		}
	})
}
