package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P2 zero-legs: an ID-changing PATCH with NO canonical poster legs
// still writes the rekey witness; when the commit then fails, the witness
// must be swept (nothing was relocated, so the reconciler has nothing to
// arbitrate) — otherwise every retry is rejected as an unresolved rekey
// until restart.
func TestRekeyCommitFailZeroLegsSweepsWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755)) // dir exists; NO pair legs
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-9", newKeyedMutexRegistry())
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9", committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "tx wedged")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-SSNI-R1.json"))
	assert.Error(t, wErr, "zero-legs commit failure must sweep the witness")

	err = m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "tx wedged", "retry must reach the committer again — not be poisoned by the lingered witness")
}

// The sweep's warn branch: a wedged Remove keeps the commit error intact and
// the witness file in place for the startup reconciler.
func TestRekeyCommitFailZeroLegsWarnOnRemoveFailure(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-9", newKeyedMutexRegistry())
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: removeFailFS{Fs: base}, tempDir: "/tmp", jobID: "JOB-9", committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "tx wedged")
	_, wErr := base.Stat(filepath.Join(dir, ".rekey-SSNI-R1.json"))
	assert.NoError(t, wErr, "wedged sweep leaves the witness for the reconciler")
}
