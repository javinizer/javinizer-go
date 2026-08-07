package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
)

// codex r41 P2b: a failed reverse rename must NOT sweep the witness — it is
// the only recovery marker for the stranded new-ID bytes.

type alwaysFailRenameFS struct{ afero.Fs }

func (alwaysFailRenameFS) Rename(string, string) error { return errors.New("simulated rename failure") }

func TestReconcileRekeyWitnessesRetainsWitnessOnReversalFailure(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "NEW-9.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9"})
	wpath := filepath.Join(dir, ".rekey-OLD-9.json")
	require.NoError(t, afero.WriteFile(base, wpath, witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRow(t, "OLD-9"), nil)
	fs := alwaysFailRenameFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "no reversal happened (rename failed)")
	_, wErr := base.Stat(wpath)
	assert.NoError(t, wErr, "witness retained for the next startup retry")
}

// codex r41 P2a: the production bootstrap path never starts the periodic
// stale-cleanup goroutine, so witness reconciliation must run synchronously
// inside NewJobStore BEFORE job reconstruction (which would otherwise let
// ClearMissingTempPosters clear the valid-looking old crop URL).
func TestNewJobStoreReconcilesWitnessesAtStartup(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W2"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-22.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-22", NewID: "NEW-22"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-22.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().List(mock.Anything).Return([]models.Job{}, nil)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W2").Return(witnessJobRow(t, "OLD-22"), nil)

	s := NewJobStore(repo, nil, nil, "/tmp", nil, fs)
	require.NotNil(t, s)
	_, statErr := fs.Stat(filepath.Join(dir, "OLD-22.jpg"))
	assert.NoError(t, statErr, "startup reconcile reversed the stranded relocation")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-22.json"))
	assert.Error(t, wErr, "witness swept after startup reconciliation")
}
