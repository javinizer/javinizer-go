package batch

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCaseInsensitiveFS(t *testing.T) {
	tmpDir := t.TempDir()

	result := isCaseInsensitiveFS(tmpDir)

	t.Logf("Filesystem case-insensitive: %v", result)
}

func TestIsCaseInsensitiveFSCached_CachesResult(t *testing.T) {
	clearFSCaseCache()

	tmpDir := t.TempDir()

	// Pass the directory itself, which is how callers actually use this function.
	// The cache key should be the exact path passed, not its parent.
	result1 := isCaseInsensitiveFSCached(tmpDir)
	result2 := isCaseInsensitiveFSCached(tmpDir)

	assert.Equal(t, result1, result2, "cached results should match for same directory")

	// Verify cache hit occurred
	fsCaseCacheMu.RLock()
	cached, exists := fsCaseCache[tmpDir]
	fsCaseCacheMu.RUnlock()
	assert.True(t, exists, "result should be cached under exact directory path")
	assert.Equal(t, result1, cached, "cached value should match returned value")
}

func TestIsCaseInsensitiveFS_HandlesNonexistentDir(t *testing.T) {
	clearFSCaseCache()

	result := isCaseInsensitiveFS("/nonexistent/path/that/does/not/exist")

	assert.False(t, result, "nonexistent directory should default to case-sensitive (safe)")
}

func clearFSCaseCache() {
	fsCaseCacheMu.Lock()
	fsCaseCache = make(map[string]bool)
	fsCaseCacheMu.Unlock()
}

type mockJobRepo struct {
	deleteErr error
}

func (m *mockJobRepo) Create(job *models.Job) error {
	return nil
}

func (m *mockJobRepo) Update(job *models.Job) error {
	return nil
}

func (m *mockJobRepo) Upsert(job *models.Job) error {
	return nil
}

func (m *mockJobRepo) Delete(id string) error {
	return m.deleteErr
}

func (m *mockJobRepo) FindByID(id string) (*models.Job, error) {
	return nil, nil
}

func (m *mockJobRepo) List() ([]models.Job, error) {
	return nil, nil
}

func (m *mockJobRepo) DeleteOrganizedOlderThan(date time.Time) error {
	return nil
}

var _ database.JobRepositoryInterface = (*mockJobRepo)(nil)

func TestDeleteJob_ReturnsErrorWhenDatabaseFails(t *testing.T) {
	mockRepo := &mockJobRepo{deleteErr: errors.New("database connection lost")}

	jq := worker.NewJobQueue(mockRepo, "", nil)

	job := jq.CreateJob([]string{"/path/to/test.mp4"})
	job.MarkCompleted()

	tmpDir := t.TempDir()

	err := jq.DeleteJob(job.ID, tmpDir)

	require.Error(t, err, "expected error when database deletion fails")
	assert.True(t, strings.Contains(err.Error(), "database deletion failed"),
		"error should mention database failure, got: %v", err)
}

func TestDeleteJob_ReturnsErrorWhenJobNotFound(t *testing.T) {
	jq := worker.NewJobQueue(nil, "", nil)

	tmpDir := t.TempDir()

	err := jq.DeleteJob("nonexistent-job", tmpDir)

	require.Error(t, err, "expected error when job not found")
	assert.True(t, strings.Contains(err.Error(), "not found"),
		"error should mention job not found, got: %v", err)
}

func TestDeleteJob_ReturnsErrorWhenJobRunning(t *testing.T) {
	jq := worker.NewJobQueue(nil, "", nil)

	job := jq.CreateJob([]string{"/path/to/test.mp4"})
	job.MarkStarted()

	tmpDir := t.TempDir()

	err := jq.DeleteJob(job.ID, tmpDir)

	require.Error(t, err, "expected error when deleting running job")
	assert.True(t, strings.Contains(err.Error(), "running"),
		"error should mention running job, got: %v", err)
}
