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
	"gorm.io/gorm"
)

func TestOfflineEvidenceCancellationDuringDiscoveryDoesNotCreateJob(t *testing.T) {
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
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Observe a real cache-loading query while discovery builds its workflow.
	// A client can disconnect while this database call is in flight; preserve
	// the query result and exercise the subsequent admission check.
	var calls atomic.Int32
	const callback = "cancel_discovery_cache_load"
	require.NoError(t, deps.Repos.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if calls.CompareAndSwap(0, 1) {
			cancel()
		}
	}))
	t.Cleanup(func() { _ = deps.Repos.DB.Callback().Query().Remove(callback) })
	ownedDeps := *deps
	rt := core.NewAPIRuntime(&ownedDeps)
	rt.SetConfig(cfg)
	t.Cleanup(rt.Shutdown)

	output, err := StartScrapeUseCase(ctx, rt, StartScrapeInput{Files: []string{file}, ShowOfflineEvidence: true})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, output)
	require.Equal(t, int32(1), calls.Load(), "cancellation must occur during discovery")
	require.Zero(t, source.calls.Load(), "no scraper may run after cancellation")
	require.Empty(t, deps.JobStore.ListJobs())
	var count int64
	require.NoError(t, deps.Repos.DB.Model(&models.Job{}).Count(&count).Error)
	require.Zero(t, count, "no durable job may be created")
	require.True(t, rt.WaitBackgroundTasks(time.Second), "request admission must be released")
	contents, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "owned source", string(contents))
}
