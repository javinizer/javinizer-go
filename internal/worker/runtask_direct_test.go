package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestRunTaskDirect_DeadlineExceeded(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-direct-dl", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-direct-dl", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:direct-dl", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 1*time.Millisecond, nil, registry)
}

func TestRunTaskDirect_RetryableError(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&retryableResolverScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-direct-rt", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-direct-rt", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:direct-rt", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 5*time.Second, nil, registry)
}

func TestRunTaskDirect_VerifiedResult(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&verifiedResolverScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-direct-vf", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-direct-vf", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:direct-vf", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 5*time.Second, nil, registry)
}
