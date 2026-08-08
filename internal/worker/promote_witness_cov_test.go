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

// --- staged promotion witnesses (codex r48/r49 P2) ---

func witnessJobRowPosterRev(t *testing.T, posterURL string, rev uint64) *models.Job {
	t.Helper()
	res := map[string]*resultstore.MovieResult{
		"/f/a.mp4": {
			ResultID:      "res-1",
			Revision:      rev,
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
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://new.example/p.jpg", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"crop": shaContentHex([]byte("old-bytes"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old.example/p.jpg", 0), nil)
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

// Crash AFTER commit: the durable row already moved past the captured
// revision with the new URL — only the witness is swept.
func TestReconcilePromoteWitnessCommittedKeeps(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://new.example/p.jpg", ResultID: "res-1", PrevRevision: 1})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://new.example/p.jpg", 2), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nothing reversed")
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "new", string(content))
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr)
}

// r49 the crux: SAME-URL refresh crash. The pre-op row already carried the
// URL — identity+URL alone cannot tell commit-landed apart. The revision
// token is the arbiter: revision unchanged ⇒ reverse.
func TestReconcilePromoteWitnessSameURLRefreshReverses(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new-uncommitted"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.bak"), []byte("old-bytes"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://same.example/p.jpg", ResultID: "res-1", PrevRevision: 5, OldSHA: map[string]string{"crop": shaContentHex([]byte("old-bytes"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	// Revision STILL 5 and URL ALREADY the same: the commit never landed.
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://same.example/p.jpg", 5), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "unchanged revision ⇒ reverse despite identical URL")
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "old-bytes", string(content))
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr)
}

// Target-scope guard: SAME URL on a DIFFERENT row must not read as committed.
func TestReconcilePromoteWitnessSameURLOtherResult(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.bak"), []byte("old"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://shared.example/p.jpg", ResultID: "res-target", PrevRevision: 0, OldSHA: map[string]string{"crop": shaContentHex([]byte("old"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))

	res := map[string]*resultstore.MovieResult{
		"/f/a.mp4": {ResultID: "res-other", Revision: 9, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ZZ-9", Poster: models.PosterState{PosterURL: "https://shared.example/p.jpg"}}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "ZZ-9"}},
		"/f/b.mp4": {ResultID: "res-target", Revision: 0, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "PI-1", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg"}}, FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "PI-1"}},
	}
	payload, merr := json.Marshal(res)
	require.NoError(t, merr)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: string(payload)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "URL on ANOTHER result must not sweep the witness un-reversed")
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "old", string(content))
}

// Retry-bearing: an already-restored canon is hash-matched and NEVER dropped
// even though its .bak was consumed by the earlier startup.
func TestReconcilePromoteWitnessRestoredLegsIdempotent(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1-full.jpg"), []byte("restored-old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new-crop-uncommitted"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.bak"), []byte("old-crop"), 0o644))
	witness, _ := json.Marshal(promoteWitness{
		PosterID: "PI-1", URL: "https://new.example/p.jpg", ResultID: "res-1", PrevRevision: 0,
		OldSHA: map[string]string{"full": shaContentHex([]byte("restored-old-full")), "crop": shaContentHex([]byte("old-crop"))},
	})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old.example/p.jpg", 0), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the unfinished crop leg reverses")
	full, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1-full.jpg"))
	assert.Equal(t, "restored-old-full", string(full), "hash-matched canon preserved across startups")
	crop, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "old-crop", string(crop), "crop restored from bak")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept once every leg reconciled")
}

// Cancel-on-terminal keeps no pinned cancelled flag (codex r48 P2): an apply
// launch is admissible after a /cancel lost the race with MarkCompleted.
func TestCancelTerminalRaceLeavesNoPinnedFlag(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusCompleted, done: make(chan struct{})}
	lc.Cancel()
	assert.False(t, lc.cancelled, "terminal-lost cancel must not pin the flag")
}
