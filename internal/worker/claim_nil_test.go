package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestClaimNil_RunningTasksNilInit(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	actressRepo := database.NewActressRepository(db)
	require.NoError(t, actressRepo.Create(context.Background(), &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-claimnil", Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-claimnil", JobID: job.ID, Label: "test", DedupeKey: "actress:claimnil", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))

	manager := &ActressSyncManager{repo: repo, owner: "test"}
	claimed, err := manager.claimAndTrack(context.Background(), 60*time.Second)
	require.NoError(t, err)
	require.NotNil(t, claimed)
}
