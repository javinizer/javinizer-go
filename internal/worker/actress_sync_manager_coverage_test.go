package worker

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerCov_NoCandidatesErrorMethods(t *testing.T) {
	e := &NoCandidatesError{SkippedIDs: []uint{}}
	assert.Contains(t, e.Error(), "no actresses require metadata sync")
	e2 := &NoCandidatesError{SkippedIDs: []uint{1, 2}}
	assert.Contains(t, e2.Error(), "2 already merged away")
	assert.True(t, errors.Is(e, database.ErrActressSyncNoCandidates))
	assert.Equal(t, database.ErrActressSyncNoCandidates, e.Unwrap())
}

func TestManagerCov_IsTransientNetErrorText(t *testing.T) {
	assert.False(t, isTransientNetErrorText(nil))
	var timeoutErr net.Error = &mockTimeoutErr{}
	assert.True(t, isTransientNetErrorText(timeoutErr))
	assert.True(t, isTransientNetErrorText(errors.New("connection reset")))
	assert.True(t, isTransientNetErrorText(errors.New("connection refused")))
	assert.True(t, isTransientNetErrorText(errors.New("no such host")))
	assert.True(t, isTransientNetErrorText(errors.New("timeout")))
	assert.True(t, isTransientNetErrorText(errors.New("temporary failure")))
	assert.True(t, isTransientNetErrorText(errors.New("EOF")))
	assert.False(t, isTransientNetErrorText(errors.New("some other error")))
}

type mockTimeoutErr struct{}

func (e *mockTimeoutErr) Error() string   { return "timeout" }
func (e *mockTimeoutErr) Timeout() bool   { return true }
func (e *mockTimeoutErr) Temporary() bool { return true }

func TestManagerCov_ShutdownNilManager(t *testing.T) {
	var m *ActressSyncManager
	m.Shutdown()
}

func TestManagerCov_StopNilManager(t *testing.T) {
	var m *ActressSyncManager
	m.Stop()
}

func TestManagerCov_CountTasksRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.CountTasks("nonexistent", "")
	require.Error(t, err)
}

func TestManagerCov_ListRunningTasksRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.ListRunningTasks("nonexistent")
	require.Error(t, err)
}

func TestManagerCov_ListDiagnosticTasksRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.ListDiagnosticTasks("nonexistent", 10)
	require.Error(t, err)
}

func TestManagerCov_GetJobRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.GetJob("nonexistent")
	require.Error(t, err)
}

func TestManagerCov_ListActiveJobsRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	require.NoError(t, db.Close())
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	_, err := manager.ListActiveJobs()
	require.Error(t, err)
}

func TestManagerCov_CancelJobNotFound(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	err := manager.CancelJob("nonexistent")
	require.Error(t, err)
}

func TestManagerCov_ClaimNextBackoff(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	manager.claimBackoffUntil = time.Now().Add(10 * time.Second)
	task, err := manager.claimAndTrack(context.Background(), 30*time.Second)
	require.NoError(t, err)
	assert.Nil(t, task)
}

func TestManagerCov_RequeueStaleTaskLeaseLost(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo}
	task := &models.ActressSyncTask{ID: "stale-task", JobID: "job1", LeaseToken: "wrong-token", DedupeKey: "dk", Stage: "queued", Messages: []string{}, UpdatedFields: []string{}}
	require.NoError(t, db.Create(task).Error)
	result := manager.requeueStaleTask(task, errors.New("identity changed"))
	assert.False(t, result)
}

func TestManagerCov_IsRetryableActressSyncError(t *testing.T) {
	assert.False(t, isRetryableActressSyncError(nil))
	assert.False(t, isRetryableActressSyncError(errors.New("not retryable")))
}

func TestManagerCov_CreateJobUnavailable(t *testing.T) {
	var m *ActressSyncManager
	_, _, err := m.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
	require.ErrorIs(t, err, ErrActressSyncManagerUnavailable)
}
