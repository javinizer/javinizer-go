package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
)

// local codex review P1: witness-carried IDs feed filepath.Join inside the
// reconcilers — a hostile or corrupt witness must never steer Rename/Remove
// outside the job poster dir. The guards trip BEFORE the job lookup, so a
// mock repo with zero expectations doubles as a tripwire for "proceeded
// anyway" (an unexpected FindByID call panics the test).

func TestReconcileRekeyWitnessUnsafeIDsLeftInPlace(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/evil-full.jpg", []byte("outside"), 0o644))
	w, _ := json.Marshal(rekeyWitness{OldID: "../evil", NewID: "NEW-1"})
	require.NoError(t, afero.WriteFile(fs, dir+"/.rekey-x1.json", w, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(dir + "/.rekey-x1.json")
	assert.NoError(t, wErr, "unsafe witness retained (never trusted, never acted on)")
	out, oErr := afero.ReadFile(fs, "/tmp/posters/evil-full.jpg")
	require.NoError(t, oErr)
	assert.Equal(t, "outside", string(out), "traversal target outside the job dir untouched")
}

func TestReconcilePromoteWitnessUnsafePosterIDLeftInPlace(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/evil-full.jpg", []byte("outside-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/evil.jpg", []byte("outside-crop"), 0o644))
	w, _ := json.Marshal(promoteWitness{PosterID: "../evil", URL: "https://x/p.jpg", ResultID: "res-1"})
	require.NoError(t, afero.WriteFile(fs, dir+"/.promote-x1.json", w, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(dir + "/.promote-x1.json")
	assert.NoError(t, wErr, "unsafe witness retained")
	out, oErr := afero.ReadFile(fs, "/tmp/posters/evil-full.jpg")
	require.NoError(t, oErr)
	assert.Equal(t, "outside-full", string(out))
	out2, oErr2 := afero.ReadFile(fs, "/tmp/posters/evil.jpg")
	require.NoError(t, oErr2)
	assert.Equal(t, "outside-crop", string(out2))
}

func TestReconcileCropWitnessUnsafeStageIDLeftInPlace(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/evil.jpg", []byte("outside-staged"), 0o644))
	w, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c1", StageID: "../evil", CroppedURL: "https://x/c.jpg"})
	require.NoError(t, afero.WriteFile(fs, dir+"/.crop-x1.json", w, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(dir + "/.crop-x1.json")
	assert.NoError(t, wErr, "unsafe witness retained")
	out, oErr := afero.ReadFile(fs, "/tmp/posters/evil.jpg")
	require.NoError(t, oErr)
	assert.Equal(t, "outside-staged", string(out), "traversal remove target untouched")
}
