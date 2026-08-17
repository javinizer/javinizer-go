package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// P3: CleanupStaleTempDirs must respect the admission lease — a job whose
// temp poster dir is mid-surgery (in-flight edit, queued phase start,
// delete-drain) is skipped; when the operation completes OR is cancelled
// (lease released either way), the next sweep takes the dir.
func TestCleanupStaleTempDirs_RespectsAdmissionLease(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/tmp"
	jobID := "JOB-LEASE-1"
	dir := tempDir + "/posters/" + jobID
	mkDir := func() {
		require.NoError(t, fs.MkdirAll(dir, 0o755))
		require.NoError(t, afero.WriteFile(fs, dir+"/poster.jpg", []byte("staged"), 0o644))
	}

	old := time.Now().Add(-48 * time.Hour)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, jobID).
		Return(&models.Job{ID: jobID, Status: models.JobStatusCompleted, CompletedAt: &old}, nil).Maybe()

	barrier := newAdmissionBarrier()
	store := &JobStore{
		jobs:    map[models.JobID]*BatchJob{models.JobID(jobID): {admission: barrier}},
		jobRepo: repo,
		tempDir: tempDir,
		fs:      fs,
	}
	ctx := context.Background()

	// 1. In-flight edit (shared lease) blocks the sweep.
	mkDir()
	releaseEdit, err := barrier.AdmitShared()
	require.NoError(t, err)
	n, err := store.CleanupStaleTempDirs(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "sweep must skip a dir whose job holds an edit lease")
	exists, _ := afero.Exists(fs, dir+"/poster.jpg")
	require.True(t, exists)

	// 2. Cancelling/completing the in-flight edit orders the same way: the
	// release makes the dir sweepable again.
	releaseEdit()
	n, err = store.CleanupStaleTempDirs(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "released edit lease restores sweepability")
	exists, _ = afero.Exists(fs, dir)
	require.False(t, exists)

	// 3. A queued phase start (waits behind nothing here but holds
	// pendingPhase/exclusive) blocks the sweep until it downgrades.
	mkDir()
	entry, err := barrier.BeginPhase(ctx)
	require.NoError(t, err)
	n, err = store.CleanupStaleTempDirs(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "queued/held phase start must block the sweep")
	releasePhase := entry.Downgrade() // phase window committed → shared lease held for the phase
	n, err = store.CleanupStaleTempDirs(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "phase-hold (downgraded shared lease) still blocks")
	releasePhase()

	// 4. Exclusive delete-drain blocks; releasing it sweeps.
	releaseDel, err := barrier.AdmitExclusive()
	require.NoError(t, err)
	n, err = store.CleanupStaleTempDirs(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "exclusive delete-drain blocks the sweep")
	releaseDel()
	n, err = store.CleanupStaleTempDirs(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "all leases released — sweep proceeds")
}

// The failing-removal leg of processStaleJobDir: error → dir keeps existing.
func TestProcessStaleJobDir_RemoveFail(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &rmFailFs{Fs: base, victim: "/out/posters/STALEDIR"}
	require.NoError(t, fs.MkdirAll("/out/posters/STALEDIR", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/out/posters/STALEDIR/x", []byte("x"), 0o644))

	r := mocks.NewMockJobRepositoryInterface(t)
	r.EXPECT().FindByID(mock.Anything, "STALEDIR").Return(nil, database.ErrNotFound)

	e, ferr := fs.Stat("/out/posters/STALEDIR")
	require.NoError(t, ferr)
	cleaner := &TempDirCleaner{fs: fs, tempDir: "/out", jobRepo: r}
	res := cleaner.processStaleJobDir(context.Background(), "/out/posters", "STALEDIR", e)
	require.False(t, res, "removal failure must not report the dir as cleaned")
}

type rmFailFs struct {
	afero.Fs
	victim string
}

func (f *rmFailFs) Remove(name string) error {
	if strings.HasPrefix(strings.ReplaceAll(name, "\\", "/"), f.victim) {
		return errors.New("remove wedged")
	}
	return f.Fs.Remove(name)
}
