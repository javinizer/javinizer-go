package worker

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMergeActressesWithSourceForTask(t *testing.T) {
	db, actressRepo, _, source := newActressSyncFixture(t, &models.Actress{JapaneseName: "merge source"})
	target := &models.Actress{JapaneseName: "merge target"}
	require.NoError(t, actressRepo.Create(t.Context(), target))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "merge-source-job", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: "merge-source-task", JobID: job.ID, ActressID: &source.ID, Status: models.ActressSyncTaskPending, Stage: "queued", DedupeKey: "merge-source", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo}).repo
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("merge-owner", now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	callback := mergeActressesWithSourceCallback(t.Context(), actressRepo, claimed)
	_, err = callback(target.ID, source.ID, *source)
	require.NoError(t, err)
	require.Equal(t, target.ID, *claimed.ActressID)

	_, err = callback(999999, source.ID, *source)
	require.Error(t, err)
}
