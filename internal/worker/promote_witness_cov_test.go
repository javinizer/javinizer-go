package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- staged promotion witnesses (codex r48 P2) ---

func witnessJobRowPoster(t *testing.T, posterURL string) *models.Job {
	t.Helper()
	res := map[string]*resultstore.MovieResult{
		"/f/a.mp4": {
			ResultID:      "res-1",
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "PI-1", Poster: models.PosterState{PosterURL: posterURL}},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PI-1"},
		},
	}
	payload, err := json.Marshal(res)
	require.NoError(t, err)
	return &models.Job{Results: string(payload)}
}

// Crash between promote and commit: the uncommitted canonical bytes are
// dropped and the parked .bak pair restored.
func TestReconcilePromoteWitnessRestoresBackup(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new-uncommitted"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.bak"), []byte("old-bytes"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://new.example/p.jpg"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPoster(t, "https://old.example/p.jpg"), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "old-bytes", string(content), "canonical restored from .bak")
	_, bakErr := fs.Stat(filepath.Join(dir, "PI-1.jpg.bak"))
	assert.Error(t, bakErr, "bak consumed by restoration")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept")
}

// Crash AFTER commit: the durable row already carries the new source URL —
// only the witness is swept; the promoted canonical bytes stay.
func TestReconcilePromoteWitnessCommittedKeeps(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://new.example/p.jpg"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPoster(t, "https://new.example/p.jpg"), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nothing reversed")
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "new", string(content))
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr)
}

// Cancel-on-terminal keeps no pinned cancelled flag (codex r48 P2): an apply
// launch is admissible after a /cancel lost the race with MarkCompleted.
func TestCancelTerminalRaceLeavesNoPinnedFlag(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusCompleted, done: make(chan struct{})}
	lc.Cancel()
	assert.False(t, lc.cancelled, "terminal-lost cancel must not pin the flag")
	select {
	case <-lc.done:
	default:
		// Completed lifecycle may have an open done from construction — that's
		// orthogonal; the flag assertion above is the contract under test.
	}
}
