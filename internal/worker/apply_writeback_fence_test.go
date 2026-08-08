package worker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasUnresolvedPromoteWitness(t *testing.T) {
	pe := newEditorForStore(resultstore.New(1, []string{"/f/a.mp4"}))
	assert.False(t, pe.hasUnresolvedPromoteWitness("PI-1"), "nil env")

	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	assert.False(t, pe.hasUnresolvedPromoteWitness("PI-1"), "no witness on disk")
	assert.False(t, pe.hasUnresolvedPromoteWitness(""), "empty poster id")

	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), []byte("{}"), 0o644))
	assert.True(t, pe.hasUnresolvedPromoteWitness("PI-1"))
}

// codex P2: the apply success write-back advances Revision — while a promote
// witness is unresolved that bump would flip startup arbitration of the
// pending promote to "committed". The write-back must be skipped.
func TestInterpretApplyResultSkipsWritebackWhenPromoteWitnessUnresolved(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-fwd", Status: models.JobStatusRunning, Revision: 5,
		Movie:         &models.Movie{ID: "OK-1", Title: "live"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-1"},
	})
	before, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	inputs := minimalApplyInputs(t, store, true)
	inputs.PromoteWitnessFn = func(posterID string) bool { return posterID == "OK-1" }
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-1"}}
	applyMovie := &models.Movie{ID: "OK-1", Title: "scraped"}
	result := &workflow.ApplyResult{Movie: &models.Movie{ID: "OK-1", Title: "scraped"}}
	outcome := interpretApplyResult("/f/a.mp4", applyMovie, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, result, nil)
	assert.True(t, outcome.Success)
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, before.Revision, final.Revision, "write-back skipped — revision untouched so arbitration stays truthful")
	assert.Equal(t, "live", final.Movie.Title)
}
