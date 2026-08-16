package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalGaps_PartialCandidatesError(t *testing.T) {
	inner := errors.New("partial")
	e := partialCandidatesError{err: inner}
	assert.Equal(t, "partial", e.Error())
	assert.Equal(t, inner, e.Unwrap())
}

func TestFinalGaps_CacheMatchesCanonicalMismatchDMMID(t *testing.T) {
	match := models.ActressInfo{DMMID: 99}
	actress := &models.Actress{DMMID: 42}
	assert.False(t, cacheMatchesCanonical(match, actress))
}

func TestFinalGaps_CacheMatchesCanonicalNameMatch(t *testing.T) {
	match := models.ActressInfo{DMMID: 42, JapaneseName: "test"}
	actress := &models.Actress{DMMID: 42, JapaneseName: "old", Aliases: " | test |"}
	assert.True(t, cacheMatchesCanonical(match, actress))
}

func TestFinalGaps_ActressInfoFieldsEmpty(t *testing.T) {
	info := models.ActressInfo{}
	assert.Empty(t, actressInfoFields(info))
}

func TestFinalGaps_ActressInfoFieldsAll(t *testing.T) {
	info := models.ActressInfo{DMMID: 1, FirstName: "F", LastName: "L", JapaneseName: "J", ThumbURL: "http://t"}
	assert.NotEmpty(t, actressInfoFields(info))
}

func TestFinalGaps_ManagerStopGraceExpiry(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{
		repo:             repo,
		owner:            "test",
		recoveryInterval: 15 * time.Second,
		retryDelay:       time.Second,
	}
	manager.ctx, manager.cancel = context.WithCancel(context.Background())
	manager.active.Store(1)
	manager.Stop()
}

func TestFinalGaps_ManagerShutdown(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{
		repo:             repo,
		owner:            "test",
		recoveryInterval: 15 * time.Second,
		retryDelay:       time.Second,
	}
	manager.ctx, manager.cancel = context.WithCancel(context.Background())
	manager.Start()
	manager.Shutdown()
	// Second shutdown should be idempotent
	manager.Shutdown()
}

func TestFinalGaps_ManagerRequeueStaleLeaseLost(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	task := &models.ActressSyncTask{ID: "stale-fg", JobID: "job1", LeaseToken: "wrong", DedupeKey: "dk", Stage: "queued", Messages: []string{}, UpdatedFields: []string{}}
	require.NoError(t, db.Create(task).Error)
	result := manager.requeueStaleTask(task, errors.New("identity changed"))
	assert.False(t, result)
}

func TestFinalGaps_ManagerCreateJobNilRepo(t *testing.T) {
	manager := &ActressSyncManager{repo: nil}
	_, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
	require.ErrorIs(t, err, ErrActressSyncManagerUnavailable)
}

func TestFinalGaps_ManagerGetJobRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.GetJob("nonexistent")
	require.Error(t, err)
}

func TestFinalGaps_ManagerCountTasksRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.CountTasks("nonexistent", "")
	require.Error(t, err)
}

func TestFinalGaps_ManagerListRunningTasksRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.ListRunningTasks("nonexistent")
	require.Error(t, err)
}

func TestFinalGaps_ManagerListDiagnosticTasksRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.ListDiagnosticTasks("nonexistent", 10)
	require.Error(t, err)
}

func TestFinalGaps_ManagerCancelJobNotFound(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	err := manager.CancelJob("nonexistent")
	require.Error(t, err)
}

func TestFinalGaps_ManagerListActiveJobsClosedDB(t *testing.T) {
	db := newActressEditTestDB(t)
	require.NoError(t, db.Close())
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.ListActiveJobs()
	require.Error(t, err)
}

func TestFinalGaps_ManagerClaimNextBackoff(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	manager.claimBackoffUntil = time.Now().Add(10 * time.Second)
	task, err := manager.claimAndTrack(context.Background(), 30*time.Second)
	require.NoError(t, err)
	assert.Nil(t, task)
}

func TestFinalGaps_NoCandidatesErrorEmpty(t *testing.T) {
	e := &NoCandidatesError{SkippedIDs: []uint{}}
	assert.Contains(t, e.Error(), "no actresses require metadata sync")
}

func TestFinalGaps_NoCandidatesErrorWithIDs(t *testing.T) {
	e := &NoCandidatesError{SkippedIDs: []uint{1, 2, 3}}
	assert.Contains(t, e.Error(), "3 already merged away")
}

func TestFinalGaps_NoCandidatesErrorIs(t *testing.T) {
	e := &NoCandidatesError{SkippedIDs: []uint{}}
	assert.True(t, errors.Is(e, database.ErrActressSyncNoCandidates))
}

func TestFinalGaps_NoCandidatesErrorUnwrap(t *testing.T) {
	e := &NoCandidatesError{SkippedIDs: []uint{}}
	assert.Equal(t, database.ErrActressSyncNoCandidates, e.Unwrap())
}

func TestFinalGaps_IsTransientNetErrorTextNil(t *testing.T) {
	assert.False(t, isTransientNetErrorText(nil))
}

func TestFinalGaps_IsTransientNetErrorTextStrings(t *testing.T) {
	assert.True(t, isTransientNetErrorText(errors.New("connection reset")))
	assert.True(t, isTransientNetErrorText(errors.New("connection refused")))
	assert.True(t, isTransientNetErrorText(errors.New("no such host")))
	assert.True(t, isTransientNetErrorText(errors.New("timeout")))
	assert.True(t, isTransientNetErrorText(errors.New("temporary failure")))
	assert.True(t, isTransientNetErrorText(errors.New("EOF")))
	assert.False(t, isTransientNetErrorText(errors.New("some other error")))
}

func TestFinalGaps_IsRetryableActressSyncErrorNil(t *testing.T) {
	assert.False(t, isRetryableActressSyncError(nil))
}

func TestFinalGaps_IsRetryableActressSyncErrorNotRetryable(t *testing.T) {
	assert.False(t, isRetryableActressSyncError(errors.New("not retryable")))
}

func TestFinalGaps_ShutdownNilManager(t *testing.T) {
	var m *ActressSyncManager
	m.Shutdown()
}

func TestFinalGaps_StopNilManager(t *testing.T) {
	var m *ActressSyncManager
	m.Stop()
}
