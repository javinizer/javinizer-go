package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// codex P3 R2-2: NewJobStore's EAGERLY built temp cleaner must carry the
// admission probe — otherwise production stores never respect edit leases.
func TestNewJobStore_CleanerCarriesAdmissionProbe(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/tmp"
	jobID := "JOB-EAGER-1"
	dir := tempDir + "/posters/" + jobID
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dir+"/poster.jpg", []byte("staged"), 0o644))

	old := time.Now().Add(-48 * time.Hour)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, jobID).
		Return(&models.Job{ID: jobID, Status: models.JobStatusCompleted, CompletedAt: &old}, nil).Maybe()
	repo.EXPECT().List(mock.Anything).Return([]models.Job{}, nil).Maybe()

	store := NewJobStore(repo, nil, nil, tempDir, nil, fs)
	barrier := newAdmissionBarrier()
	store.jobs[models.JobID(jobID)] = &BatchJob{admission: barrier}

	release, err := barrier.AdmitShared()
	require.NoError(t, err)
	n, err := store.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, n, "eagerly-constructed cleaner must respect the edit lease")

	release()
	n, err = store.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
