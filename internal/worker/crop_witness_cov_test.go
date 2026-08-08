package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- staged crop witnesses (codex r51) ---

func cropWitnessJobRow(t *testing.T, posterURL string, rev uint64) *models.Job {
	t.Helper()
	res := map[string]*resultstore.MovieResult{
		"/f/a.mp4": {
			ResultID:      "res-c1",
			Revision:      rev,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "CP-1", Poster: models.PosterState{CroppedPosterURL: posterURL}},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "CP-1"},
		},
	}
	payload, err := json.Marshal(res)
	require.NoError(t, err)
	return &models.Job{Results: string(payload)}
}

const cropWitnessFixtureURL = "/api/v1/temp/posters/JOB-W1/CP-1.jpg?v=777"

func seedCropWitness(t *testing.T, fs afero.Fs, dir string, rev uint64) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "stage-c1.jpg"), []byte("staged-crop"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "stage-c1-full.jpg"), []byte("staged-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "CP-1.jpg"), []byte("old-crop"), 0o644))
	w, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c1", StageID: "stage-c1", CroppedURL: cropWitnessFixtureURL, PrevRevision: rev})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-c1.json"), w, 0o644))
}

// Commit landed but the process died before the promote — startup finishes
// the rename.
func TestReconcileCropWitnessCommittedPromotesStaged(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedCropWitness(t, fs, dir, 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, cropWitnessFixtureURL, 5), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "staged crop promoted over canonical")
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "CP-1.jpg"))
	assert.Equal(t, "staged-crop", string(content))
	_, stErr := fs.Stat(filepath.Join(dir, "stage-c1.jpg"))
	assert.Error(t, stErr, "staged file consumed")
	_, stFullErr := fs.Stat(filepath.Join(dir, "stage-c1-full.jpg"))
	assert.Error(t, stFullErr, "staged full-size copy is swept too (r52 P2b)")
	_, wErr := fs.Stat(filepath.Join(dir, ".crop-stage-c1.json"))
	assert.Error(t, wErr, "witness swept")
}

// Commit never landed: canonical untouched, staged bytes + witness dropped.
func TestReconcileCropWitnessUncommittedDropsStaged(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedCropWitness(t, fs, dir, 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, "/api/v1/temp/posters/JOB-W1/CP-1.jpg?v=111", 4), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "CP-1.jpg"))
	assert.Equal(t, "old-crop", string(content), "canonical NEVER touched pre-commit")
	_, stErr := fs.Stat(filepath.Join(dir, "stage-c1.jpg"))
	assert.Error(t, stErr, "staged fleet dropped")
}

// r52 P2a: a transient Stat error on the staged crop leg must KEEP the
// witness — bytes may still be there; IsNotExist is the only sweep-able one.
type statFailNameFS struct {
	afero.Fs
	name string
}

func (f statFailNameFS) Stat(name string) (os.FileInfo, error) {
	if strings.HasSuffix(name, f.name) {
		return nil, os.ErrPermission
	}
	return f.Fs.Stat(name)
}

func TestReconcileCropWitnessStatErrorRetains(t *testing.T) {
	base, dir := witnessFixture(t)
	seedCropWitness(t, base, dir, 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, cropWitnessFixtureURL, 5), nil)
	fs := statFailNameFS{Fs: base, name: "stage-c1.jpg"}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	_, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	_, wErr := base.Stat(filepath.Join(dir, ".crop-stage-c1.json"))
	assert.NoError(t, wErr, "witness retained on transient stat error")
}

// A transient rename failure during reconcile keeps the witness (and the
// staged bytes) for the next startup retry.
func TestReconcileCropWitnessRenameFailureRetains(t *testing.T) {
	base, dir := witnessFixture(t)
	seedCropWitness(t, base, dir, 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, cropWitnessFixtureURL, 5), nil)
	fs := alwaysFailRenameFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := base.Stat(filepath.Join(dir, ".crop-stage-c1.json"))
	assert.NoError(t, wErr, "witness retained for retry")
	_, stErr := base.Stat(filepath.Join(dir, "stage-c1.jpg"))
	assert.NoError(t, stErr, "staged bytes retained too")
}
