package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressSyncCandidateAndFillBlank(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 42, LastName: "Existing"}
	require.NoError(t, repo.Create(context.Background(), actress))
	candidates, err := repo.ListSyncCandidates(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	fields, err := repo.FillBlankMetadata(context.Background(), actress.ID, 42, models.ActressInfo{DMMID: 42, FirstName: "New", LastName: "Overwrite", JapaneseName: "名前", ThumbURL: "thumb"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"thumb_url", "japanese_name", "first_name"}, fields)
	updated, err := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Existing", updated.LastName)
	require.Equal(t, "New", updated.FirstName)
	_, err = repo.FillBlankMetadata(context.Background(), actress.ID, 42, models.ActressInfo{DMMID: 99, JapaneseName: "wrong"})
	require.Error(t, err)
}

func TestActressSyncRepairsOnlyKnownInvalidThumbnail(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	malformed := &models.Actress{DMMID: 43, FirstName: "Takami", LastName: "Iseya", JapaneseName: "伊勢谷たかみ", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/iseya_takami"}
	valid := &models.Actress{DMMID: 44, FirstName: "Custom", LastName: "Image", JapaneseName: "カスタム", ThumbURL: "https://example.com/custom.jpg"}
	require.NoError(t, repo.Create(context.Background(), malformed))
	require.NoError(t, repo.Create(context.Background(), valid))

	candidates, err := repo.ListSyncCandidates(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, malformed.ID, candidates[0].ID)

	fields, err := repo.FillBlankMetadata(context.Background(), malformed.ID, malformed.DMMID, models.ActressInfo{
		DMMID: malformed.DMMID, ThumbURL: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg",
	})
	require.NoError(t, err)
	require.Empty(t, fields)

	resolved := "https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg"
	fields, err = repo.FillBlankMetadata(context.Background(), malformed.ID, malformed.DMMID, models.ActressInfo{DMMID: malformed.DMMID, ThumbURL: resolved})
	require.NoError(t, err)
	require.Equal(t, []string{"thumb_url"}, fields)
	updated, err := repo.FindByID(context.Background(), malformed.ID)
	require.NoError(t, err)
	require.Equal(t, resolved, updated.ThumbURL)

	custom := "https://example.com/owner-selected.jpg"
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", malformed.ID).Update("thumb_url", custom).Error)
	fields, err = repo.FillBlankMetadata(context.Background(), malformed.ID, malformed.DMMID, models.ActressInfo{DMMID: malformed.DMMID, ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/replacement.jpg"})
	require.NoError(t, err)
	require.Empty(t, fields)
	updated, err = repo.FindByID(context.Background(), malformed.ID)
	require.NoError(t, err)
	require.Equal(t, custom, updated.ThumbURL)
}

func TestActressSyncAtomicClaimAndRecovery(t *testing.T) {
	db := newDatabaseTestDB(t)
	actress := &models.Actress{DMMID: 77, JapaneseName: "女優"}
	require.NoError(t, NewActressRepository(db).Create(context.Background(), actress))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: "actress:test", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	var wg sync.WaitGroup
	claimed := make(chan *models.ActressSyncTask, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, _ := repo.ClaimNext(uuid.NewString(), now.Add(time.Minute))
			claimed <- item
		}()
	}
	wg.Wait()
	close(claimed)
	count := 0
	var active *models.ActressSyncTask
	for item := range claimed {
		if item != nil {
			count++
			active = item
		}
	}
	require.Equal(t, 1, count)
	require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", active.ID).Updates(map[string]any{"lease_expires_at": now.Add(-time.Minute), "attempts": 3}).Error)
	require.NoError(t, repo.RecoverExpiredLeases(now))
	tasks, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncTaskFailed, tasks[0].Status)
	require.Equal(t, "attempt_cap_reached", tasks[0].ErrorMessage)
}

func TestActressSyncStaleLeaseTransitionsAreFenced(t *testing.T) {
	db := newDatabaseTestDB(t)
	actress := &models.Actress{DMMID: 77, JapaneseName: "女優"}
	require.NoError(t, NewActressRepository(db).Create(context.Background(), actress))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: "actress:stale", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("owner", now.Add(-time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	newExpiry := now.Add(time.Hour)
	require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", claimed.ID).Updates(map[string]any{"lease_token": "new-token", "lease_expires_at": newExpiry}).Error)

	transitioned, err := repo.recoverExpiredTask(db.DB, *claimed, false, now)
	require.NoError(t, err)
	require.False(t, transitioned)
	transitioned, err = repo.releaseOwnerTask(db.DB, *claimed, false, now)
	require.NoError(t, err)
	require.False(t, transitioned)
	require.Error(t, repo.Heartbeat(claimed.ID, claimed.LeaseToken, now.Add(time.Minute)))
	require.Error(t, repo.UpdateStage(claimed.ID, claimed.LeaseToken, "saving"))

	var current models.ActressSyncTask
	require.NoError(t, db.First(&current, "id = ?", claimed.ID).Error)
	require.Equal(t, models.ActressSyncTaskRunning, current.Status)
	require.Equal(t, "new-token", current.LeaseToken)
	require.WithinDuration(t, newExpiry, *current.LeaseExpiresAt, time.Second)
}

func TestActressSyncRequeueTaskRestoresPendingLease(t *testing.T) {
	db := newDatabaseTestDB(t)
	actress := &models.Actress{DMMID: 77, JapaneseName: "女優"}
	require.NoError(t, NewActressRepository(db).Create(context.Background(), actress))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: "actress:requeue", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncRepository(db)
	require.ErrorIs(t, repo.RequeueTask(nil, "token"), ErrInvalidLookup)
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("owner", now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, 1, claimed.Attempts)
	require.NoError(t, repo.RequeueTask(claimed, claimed.LeaseToken))
	stored, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncTaskPending, stored[0].Status)
	require.Equal(t, "queued", stored[0].Stage)
	require.Zero(t, stored[0].Attempts)
	require.Empty(t, stored[0].LeaseToken)
	require.Nil(t, stored[0].LeaseExpiresAt)
	reclaimed, err := repo.ClaimNext("owner-2", now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, reclaimed)
	require.Equal(t, 1, reclaimed.Attempts)
}

func TestActressSyncRequeueTaskCancelsAfterJobCancellation(t *testing.T) {
	db := newDatabaseTestDB(t)
	actress := &models.Actress{DMMID: 78, JapaneseName: "女優"}
	require.NoError(t, NewActressRepository(db).Create(context.Background(), actress))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "cancel-requeue", DedupeKey: "actress:cancel-requeue", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("owner", now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.CancelJob(job.ID))
	require.NoError(t, repo.RequeueTask(claimed, claimed.LeaseToken))

	stored, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, models.ActressSyncTaskCancelled, stored[0].Status)
	require.Equal(t, "cancelled", stored[0].Outcome)
	storedJob, err := repo.FindJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncJobCancelled, storedJob.Status)
}

func TestManualActressMergeMigratesActiveSyncTasks(t *testing.T) {
	for _, status := range []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning} {
		t.Run(status, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			actressRepo := NewActressRepository(db)
			target := &models.Actress{JapaneseName: "target"}
			source := &models.Actress{JapaneseName: "source"}
			require.NoError(t, actressRepo.Create(context.Background(), target))
			require.NoError(t, actressRepo.Create(context.Background(), source))
			now := time.Now().UTC()
			job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
			task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &source.ID, Label: "source", DedupeKey: fmt.Sprintf("actress:%d", source.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
			syncRepo := NewActressSyncRepository(db)
			require.NoError(t, syncRepo.CreateJob(job, []models.ActressSyncTask{task}))
			if status == models.ActressSyncTaskRunning {
				claimed, err := syncRepo.ClaimNext("manual-merge-test", now.Add(time.Hour))
				require.NoError(t, err)
				require.NotNil(t, claimed)
				task = *claimed
			}

			_, err := actressRepo.MergeWithSource(context.Background(), target.ID, source.ID, nil, models.Actress{})
			require.NoError(t, err)
			_, err = actressRepo.FindByID(context.Background(), source.ID)
			require.Error(t, err)
			stored, err := syncRepo.ListTasks(job.ID)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			require.NotNil(t, stored[0].ActressID)
			require.Equal(t, target.ID, *stored[0].ActressID)
			require.Equal(t, status, stored[0].Status)
			storedJob, err := syncRepo.FindJob(job.ID)
			require.NoError(t, err)
			require.Equal(t, 1, storedJob.TotalTasks)
			require.Equal(t, 0, storedJob.Completed)
		})
	}
}

func TestManualActressMergeCoalescesConflictingSyncTasks(t *testing.T) {
	cases := []struct {
		name         string
		sourceStatus string
		targetStatus string
		sourceScope  string
		targetScope  string
	}{
		{"pending-pending", models.ActressSyncTaskPending, models.ActressSyncTaskPending, "selected", "missing"},
		{"running-pending", models.ActressSyncTaskRunning, models.ActressSyncTaskPending, "selected", "missing"},
		{"pending-running", models.ActressSyncTaskPending, models.ActressSyncTaskRunning, "selected", "missing"},
		{"running-running", models.ActressSyncTaskRunning, models.ActressSyncTaskRunning, "selected", "missing"},
		{"pending-selected", models.ActressSyncTaskPending, models.ActressSyncTaskPending, "missing", "selected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			actressRepo := NewActressRepository(db)
			target := &models.Actress{JapaneseName: "target"}
			source := &models.Actress{JapaneseName: "source"}
			require.NoError(t, actressRepo.Create(context.Background(), target))
			require.NoError(t, actressRepo.Create(context.Background(), source))
			now := time.Now().UTC()
			makeJob := func(actressID uint, status, scope, label string) (*models.ActressSyncJob, models.ActressSyncTask) {
				job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: scope, CreatedAt: now}
				task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actressID, Label: label, DedupeKey: fmt.Sprintf("actress:%d", actressID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
				if status == models.ActressSyncTaskRunning {
					job.Status = models.ActressSyncJobRunning
					expires := now.Add(time.Hour)
					task.Stage, task.LeaseOwner, task.LeaseToken, task.LeaseExpiresAt = "resolving", "owner", uuid.NewString(), &expires
				}
				return job, task
			}
			sourceJob, sourceTask := makeJob(source.ID, tc.sourceStatus, tc.sourceScope, "source")
			targetJob, targetTask := makeJob(target.ID, tc.targetStatus, tc.targetScope, "target")
			require.NoError(t, db.Create(sourceJob).Error)
			require.NoError(t, db.Create(targetJob).Error)
			require.NoError(t, db.Create(&sourceTask).Error)
			require.NoError(t, db.Create(&targetTask).Error)
			_, err := actressRepo.MergeWithSource(context.Background(), target.ID, source.ID, nil, models.Actress{})
			require.NoError(t, err)
			var stored []models.ActressSyncTask
			require.NoError(t, db.Find(&stored).Error)
			require.Len(t, stored, 2)
			for _, task := range stored {
				require.NotNil(t, task.ActressID)
				require.Equal(t, target.ID, *task.ActressID)
			}
		})
	}
}

func TestActressSyncMergeIsLeaseFencedAndAtomic(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	canonical := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	duplicate := &models.Actress{JapaneseName: "高橋りこ"}
	require.NoError(t, actressRepo.Create(context.Background(), canonical))
	require.NoError(t, actressRepo.Create(context.Background(), duplicate))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &duplicate.ID, Label: "duplicate", DedupeKey: "actress:" + fmt.Sprint(duplicate.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	syncRepo := NewActressSyncRepository(db)
	require.NoError(t, syncRepo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := syncRepo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	_, err = actressRepo.MergeForSyncTask(context.Background(), canonical.ID, duplicate.ID, nil, claimed.ID, "stale-token")
	require.Error(t, err)
	_, err = actressRepo.FindByID(context.Background(), duplicate.ID)
	require.NoError(t, err)

	_, err = actressRepo.MergeForSyncTask(context.Background(), canonical.ID, duplicate.ID, nil, claimed.ID, claimed.LeaseToken)
	require.NoError(t, err)
	_, err = actressRepo.FindByID(context.Background(), duplicate.ID)
	require.Error(t, err)
	tasks, err := syncRepo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].ActressID)
	require.Equal(t, canonical.ID, *tasks[0].ActressID)
	require.Equal(t, fmt.Sprintf("actress:%d", canonical.ID), tasks[0].DedupeKey)
	require.Contains(t, tasks[0].UpdatedFields, "merged_duplicate")
}

func TestActressSyncMergeCoalescesPendingCanonicalTask(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	canonical := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	duplicate := &models.Actress{JapaneseName: "高橋りこ"}
	require.NoError(t, actressRepo.Create(context.Background(), canonical))
	require.NoError(t, actressRepo.Create(context.Background(), duplicate))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	duplicateTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &duplicate.ID, Label: "duplicate", DedupeKey: fmt.Sprintf("actress:%d", duplicate.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	canonicalTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &canonical.ID, Label: "canonical", DedupeKey: fmt.Sprintf("actress:%d", canonical.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
	syncRepo := NewActressSyncRepository(db)
	require.NoError(t, syncRepo.CreateJob(job, []models.ActressSyncTask{duplicateTask, canonicalTask}))
	claimed, err := syncRepo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, duplicateTask.ID, claimed.ID)
	_, err = actressRepo.MergeForSyncTask(context.Background(), canonical.ID, duplicate.ID, nil, claimed.ID, claimed.LeaseToken)
	require.NoError(t, err)
	tasks, err := syncRepo.ListTasks(job.ID)
	require.NoError(t, err)
	byID := map[string]models.ActressSyncTask{tasks[0].ID: tasks[0], tasks[1].ID: tasks[1]}
	require.Equal(t, models.ActressSyncTaskSkipped, byID[canonicalTask.ID].Status)
	require.Equal(t, fmt.Sprintf("actress:%d", canonical.ID), byID[duplicateTask.ID].DedupeKey)
}

func TestActressSyncMetadataMutationPersistsAcrossLeaseRecovery(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: fmt.Sprintf("actress:%d", actress.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	syncRepo := NewActressSyncRepository(db)
	require.NoError(t, syncRepo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := syncRepo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	fields, err := actressRepo.FillBlankMetadataForSyncTask(context.Background(), actress.ID, actress.DMMID, models.ActressInfo{DMMID: actress.DMMID, FirstName: "Eri", LastName: "Imai", ThumbURL: "https://example.test/eri.jpg"}, claimed.ID, claimed.LeaseToken)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"first_name", "last_name", "thumb_url"}, fields)
	require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", claimed.ID).Update("lease_expires_at", now.Add(-time.Minute)).Error)
	require.NoError(t, syncRepo.RecoverExpiredLeases(now))
	reclaimed, err := syncRepo.ClaimNext("owner-2", now.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, reclaimed)
	require.ElementsMatch(t, fields, reclaimed.UpdatedFields)
}
func TestActressSyncCompletionPreservesJournaledMutationAfterError(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: fmt.Sprintf("actress:%d", actress.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	syncRepo := NewActressSyncRepository(db)
	require.NoError(t, syncRepo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := syncRepo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	_, err = actressRepo.FillBlankMetadataForSyncTask(context.Background(), actress.ID, actress.DMMID, models.ActressInfo{DMMID: actress.DMMID, FirstName: "Eri"}, claimed.ID, claimed.LeaseToken)
	require.NoError(t, err)
	claimed.Status, claimed.Outcome, claimed.ErrorMessage = models.ActressSyncTaskFailed, "failed", "later resolver failed"
	require.NoError(t, syncRepo.CompleteTask(claimed, claimed.LeaseToken))
	tasks, err := syncRepo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncTaskCompleted, tasks[0].Status)
	require.Equal(t, "updated_with_warning", tasks[0].Outcome)
	require.Equal(t, "partial_sync_error", tasks[0].Warning)
	require.Contains(t, tasks[0].UpdatedFields, "first_name")
	fresh, err := syncRepo.FindJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, 1, fresh.Updated)
	require.Equal(t, 1, fresh.Warnings)
}

func TestActressSyncStaleLeaseCannotMutateMetadata(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: fmt.Sprintf("actress:%d", actress.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	syncRepo := NewActressSyncRepository(db)
	require.NoError(t, syncRepo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := syncRepo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", claimed.ID).Update("lease_expires_at", now.Add(-time.Minute)).Error)
	_, err = actressRepo.FillBlankMetadataForSyncTask(context.Background(), actress.ID, actress.DMMID, models.ActressInfo{DMMID: actress.DMMID, FirstName: "stale"}, claimed.ID, claimed.LeaseToken)
	require.Error(t, err)
	updated, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	require.Empty(t, updated.FirstName)
}
func TestActressSyncReleaseOwnerLeasesCancelsCancelledJob(t *testing.T) {
	db := newDatabaseTestDB(t)
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, Label: "test", DedupeKey: "actress:cancelled", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, db.Model(&models.ActressSyncJob{}).Where("id = ?", job.ID).Update("cancel_requested", true).Error)
	require.NoError(t, repo.ReleaseOwnerLeases("owner"))

	tasks, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskCancelled, tasks[0].Status)
	require.Equal(t, "cancelled", tasks[0].Outcome)
	fresh, err := repo.FindJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncJobCancelled, fresh.Status)
	require.Equal(t, 1, fresh.Cancelled)
}

func TestActressFiltersReturnExpectedSubsets(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 100, FirstName: "A", LastName: "B", JapaneseName: "名前", ThumbURL: "https://example.test/a.jpg"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 0, JapaneseName: "無名"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 200, JapaneseName: "サムネ", ThumbURL: "https://example.com/custom_thumb.jpg"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 0, FirstName: "Romaji", LastName: "Only"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 300, FirstName: "Query", JapaneseName: "照会", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/iseya_takami?cache=1.0#v2.0"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 400, FirstName: "Nested", JapaneseName: "入子", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/archive.v1/image"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 500, FirstName: "Lookalike", JapaneseName: "類似", ThumbURL: "https://pics.dmm.co.jp.evil.test/mono/actjpgs/image"}))

	missingDMM, err := repo.ListFiltered(context.Background(), "missing_dmm", 100, 0, "id", "asc")
	require.NoError(t, err)
	require.Len(t, missingDMM, 2)

	hasDMM, err := repo.ListFiltered(context.Background(), "has_dmm", 100, 0, "id", "asc")
	require.NoError(t, err)
	require.Len(t, hasDMM, 5)

	missingThumb, err := repo.ListFiltered(context.Background(), "missing_thumbnail", 100, 0, "id", "asc")
	require.NoError(t, err)
	require.Len(t, missingThumb, 4)

	jpOnly, err := repo.ListFiltered(context.Background(), "japanese_name_only", 100, 0, "id", "asc")
	require.NoError(t, err)
	require.Len(t, jpOnly, 2)
	assert.Equal(t, "無名", jpOnly[0].JapaneseName)

	count, err := repo.CountFiltered(context.Background(), "missing_dmm")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	searchFiltered, err := repo.SearchFiltered(context.Background(), "名前", "has_dmm", 100, 0, "id", "asc")
	require.NoError(t, err)
	require.Len(t, searchFiltered, 1)

	searchCount, err := repo.CountSearchFiltered(context.Background(), "名前", "has_dmm")
	require.NoError(t, err)
	assert.Equal(t, int64(1), searchCount)
}

func TestActressRepositoryWrappersWorkWithoutTaskContext(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	syncRepo := NewActressSyncRepository(db)
	actress := &models.Actress{DMMID: 777, JapaneseName: "テスト", ThumbURL: "https://example.test/old.jpg"}
	require.NoError(t, repo.Create(context.Background(), actress))

	replaced, err := repo.ReplaceThumbnail(context.Background(), actress.ID, 777, "https://example.test/old.jpg", "https://example.test/new.jpg")
	require.NoError(t, err)
	assert.True(t, replaced)

	assigned, err := repo.AssignDMMIDIfMissing(context.Background(), actress.ID, 888)
	require.NoError(t, err)
	assert.False(t, assigned)

	noDMM := &models.Actress{JapaneseName: "ノーDMM"}
	require.NoError(t, repo.Create(context.Background(), noDMM))
	assigned, err = repo.AssignDMMIDIfMissing(context.Background(), noDMM.ID, 999)
	require.NoError(t, err)
	assert.True(t, assigned)

	_, err = syncRepo.ListActiveJobs()
	require.NoError(t, err)
}

func TestActressSyncCancelJob(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, Label: "test", DedupeKey: "actress:cancel-test", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	require.NoError(t, repo.CancelJob(job.ID))
	fresh, err := repo.FindJob(job.ID)
	require.NoError(t, err)
	assert.True(t, fresh.CancelRequested)
	assert.Equal(t, models.ActressSyncJobCancelled, fresh.Status)
	tasks, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, models.ActressSyncTaskCancelled, tasks[0].Status)
}

func TestListSyncCandidatesIncludesNamedMissingDMMActresses(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	recoverable := &models.Actress{JapaneseName: "未解決女優"}
	romanized := &models.Actress{FirstName: "Romanized", LastName: "Only"}
	firstOnly := &models.Actress{FirstName: "FirstOnly"}
	lastOnly := &models.Actress{LastName: "LastOnly"}
	unnamed := &models.Actress{}
	for _, actress := range []*models.Actress{recoverable, romanized, firstOnly, lastOnly, unnamed} {
		require.NoError(t, repo.Create(context.Background(), actress))
	}

	candidates, err := repo.ListSyncCandidates(context.Background())
	require.NoError(t, err)
	ids := make([]uint, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	require.Contains(t, ids, recoverable.ID)
	require.Contains(t, ids, romanized.ID)
	require.Contains(t, ids, firstOnly.ID)
	require.Contains(t, ids, lastOnly.ID)
	require.NotContains(t, ids, unnamed.ID)
}

func TestFindAllByJapaneseNameReturnsEveryDMMBackedMatch(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	first := &models.Actress{DMMID: 1001, JapaneseName: "同名女優"}
	second := &models.Actress{DMMID: 1002, JapaneseName: "同名女優"}
	missing := &models.Actress{JapaneseName: "同名女優"}
	require.NoError(t, repo.Create(context.Background(), first))
	require.NoError(t, repo.Create(context.Background(), second))
	require.NoError(t, repo.Create(context.Background(), missing))

	matches, err := repo.FindAllByJapaneseName(context.Background(), "同名女優")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	require.Equal(t, second.DMMID, matches[0].DMMID)
	require.Equal(t, first.DMMID, matches[1].DMMID)
}
