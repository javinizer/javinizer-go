package core

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func newCandidateGCCoreDepsPR260(t *testing.T) (*config.Config, *commandutil.CoreDeps) {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.System.VersionCheckEnabled = false
	cfg.System.ImageCacheEnabled = false
	cfg.Metadata.ActressDatabase.CandidateRetentionDays = 30
	deps, err := commandutil.NewDependencies(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = deps.Close() })
	return cfg, deps
}

func TestBootstrapAPIDepsPrunesStaleCandidatesPR260(t *testing.T) {
	cfg, coreDeps := newCandidateGCCoreDepsPR260(t)
	staleAt := time.Now().AddDate(0, 0, -60)
	candidate := &models.Actress{JapaneseName: "stale", Origin: "scrape", CreatedAt: staleAt, UpdatedAt: staleAt}
	require.NoError(t, coreDeps.DB.Repositories().ActressRepo.Create(context.Background(), candidate))
	deps, rt, err := bootstrapAPIDeps(cfg, "", nil, coreDeps)
	require.NoError(t, err)
	t.Cleanup(rt.Shutdown)
	remaining, err := deps.Repos.ActressRepo.CountCandidates(context.Background())
	require.NoError(t, err)
	require.Zero(t, remaining)
}

func TestBootstrapAPIDepsContinuesWhenCandidatePruneFailsPR260(t *testing.T) {
	cfg, coreDeps := newCandidateGCCoreDepsPR260(t)
	require.NoError(t, coreDeps.DB.Migrator().DropTable(&models.Actress{}))
	_, rt, err := bootstrapAPIDeps(cfg, "", nil, coreDeps)
	require.NoError(t, err)
	t.Cleanup(rt.Shutdown)
}
