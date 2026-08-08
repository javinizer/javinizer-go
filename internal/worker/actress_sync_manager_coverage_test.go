package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestListActiveJobsOnManager(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	jobs, err := manager.ListActiveJobs()
	require.NoError(t, err)
	if jobs == nil {
		t.Fatal("expected non-nil jobs")
	}
	_ = models.ActressSyncJob{}
	_ = time.Now()
	_ = config.Config{}
	_ = scraperutil.NewScraperRegistry()
}
