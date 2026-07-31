package core

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/require"
)

func TestStopActressSyncManagerNilRuntime(t *testing.T) {
	var rt *APIRuntime
	rt.stopActressSyncManager()
}

func TestActressSyncManagerSnapshot(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	t.Cleanup(func() { _ = db.Close() })

	registry := scraperutil.NewScraperRegistry()
	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry, DB: db},
		Repos:    db.Repositories(),
	}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(config.DefaultConfig(nil, nil))
	rt.Runtime = NewRuntimeState()
	manager := rt.EnsureActressSyncManager()
	require.NotNil(t, manager)
	t.Cleanup(rt.stopActressSyncManager)

	actress := &models.Actress{DMMID: 42, JapaneseName: "テスト"}
	require.NoError(t, deps.Repos.ActressRepo.Create(t.Context(), actress))
	job, err := manager.CreateJob(t.Context(), worker.ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{actress.ID}})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		current, findErr := manager.GetJob(job.ID)
		return findErr == nil && (current.Status == models.ActressSyncJobCompleted || current.Status == models.ActressSyncJobCancelled)
	}, 3*time.Second, 10*time.Millisecond)
}
