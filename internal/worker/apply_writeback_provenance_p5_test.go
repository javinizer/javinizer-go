package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func frozenApplyProvenance() *resultstore.ProvenanceData {
	return &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"title": "scraper", "director": "scraper"},
		ScraperResults: []*models.ScraperResult{{Source: "dmm", Title: "phase source"}},
	}
}

func seedApplyProvenanceStore(t *testing.T) (resultstore.Store, *models.Movie, *resultstore.ProvenanceData) {
	t.Helper()
	store := resultstore.New(1, []string{"/f/p5.mp4"})
	movie := &models.Movie{ID: "P5-001", Title: "phase title", Director: "phase director"}
	store.UpdateFileResult("/f/p5.mp4", &resultstore.MovieResult{
		ResultID: "res-p5", Revision: 1, Status: models.JobStatusRunning,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/p5.mp4", MovieID: movie.ID},
		Movie:         movie.Clone(),
	})
	frozen := frozenApplyProvenance()
	store.SetProvenance("/f/p5.mp4", &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"title": "user"},
		ScraperResults: frozen.ScraperResults,
	})
	return store, movie, frozen
}

func assertFrozenApplyProvenance(t *testing.T, store resultstore.Store) {
	t.Helper()
	got := store.GetProvenance("/f/p5.mp4")
	require.NotNil(t, got)
	assert.Equal(t, map[string]string{"title": "user", "director": "scraper"}, got.FieldSources)
	require.Len(t, got.ScraperResults, 1)
	assert.Equal(t, "dmm", got.ScraperResults[0].Source)
}

// TestApplyWriteBack_FrozenProvenanceSurvivesAllPaths verifies that apply
// success, failure, and panic recovery all preserve untouched frozen
// attribution while retaining live per-key edits and the global raw set.
func TestApplyWriteBack_FrozenProvenanceSurvivesAllPaths(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store, movie, frozen := seedApplyProvenanceStore(t)
		inputs := minimalApplyInputs(t, store, false)
		inputs.Provenance = map[string]*resultstore.ProvenanceData{"/f/p5.mp4": frozen}
		outcome := interpretApplyResult("/f/p5.mp4", movie, time.Now(), 0, inputs, ApplyPhaseConfig{}, context.Background(),
			&ApplyFileContext{FilePath: "/f/p5.mp4", Match: models.FileMatchInfo{Path: "/f/p5.mp4", MovieID: movie.ID}},
			&workflow.ApplyResult{Movie: &models.Movie{ID: movie.ID, Title: "apply title"}}, nil)
		require.True(t, outcome.Success)
		assertFrozenApplyProvenance(t, store)
	})

	t.Run("failure", func(t *testing.T) {
		store, movie, frozen := seedApplyProvenanceStore(t)
		inputs := minimalApplyInputs(t, store, false)
		inputs.Provenance = map[string]*resultstore.ProvenanceData{"/f/p5.mp4": frozen}
		outcome := interpretApplyResult("/f/p5.mp4", movie, time.Now(), 0, inputs, ApplyPhaseConfig{}, context.Background(),
			&ApplyFileContext{FilePath: "/f/p5.mp4", Match: models.FileMatchInfo{Path: "/f/p5.mp4", MovieID: movie.ID}}, nil, errors.New("apply failed"))
		require.True(t, outcome.Failed)
		assertFrozenApplyProvenance(t, store)
	})

	t.Run("panic recovery", func(t *testing.T) {
		store, movie, frozen := seedApplyProvenanceStore(t)
		outcome := &applyFileOutcome{}
		rc := recoveryContext{
			filePath: "/f/p5.mp4", fmi: models.FileMatchInfo{Path: "/f/p5.mp4", MovieID: movie.ID},
			movie: movie, provenance: frozen, updater: store,
		}
		func() {
			defer withFileRecovery(rc, outcome)()
			panic("apply panic")
		}()
		require.True(t, outcome.Panic)
		assertFrozenApplyProvenance(t, store)
	})
}

func TestApplyWriteBack_MissingRowFallbackPreservesProvenance(t *testing.T) {
	store := resultstore.New(1, []string{"/f/p5.mp4"})
	movie := &models.Movie{ID: "P5-MISSING", Title: "phase title"}
	frozen := frozenApplyProvenance()
	frozen.FieldSources["title"] = "user"
	inputs := minimalApplyInputs(t, store, false)
	inputs.Provenance = map[string]*resultstore.ProvenanceData{"/f/p5.mp4": frozen}

	outcome := interpretApplyResult("/f/p5.mp4", movie, time.Now(), 0, inputs, ApplyPhaseConfig{}, context.Background(),
		&ApplyFileContext{FilePath: "/f/p5.mp4", Match: models.FileMatchInfo{Path: "/f/p5.mp4", MovieID: movie.ID}}, nil, errors.New("apply failed before result creation"))
	require.True(t, outcome.Failed)
	assertFrozenApplyProvenance(t, store)
}

func TestRecovery_MissingRowFallbackPreservesProvenance(t *testing.T) {
	store := resultstore.New(1, []string{"/f/p5.mp4"})
	movie := &models.Movie{ID: "P5-RECOVERY-MISSING", Title: "phase title"}
	frozen := frozenApplyProvenance()
	frozen.FieldSources["title"] = "user"
	outcome := &applyFileOutcome{}
	rc := recoveryContext{
		filePath: "/f/p5.mp4", fmi: models.FileMatchInfo{Path: "/f/p5.mp4", MovieID: movie.ID},
		movie: movie, provenance: frozen, updater: store,
	}
	func() {
		defer withFileRecovery(rc, outcome)()
		panic("apply panic before result creation")
	}()

	require.True(t, outcome.Panic)
	assertFrozenApplyProvenance(t, store)
}

func TestUpsertWriteBackResultWithProvenance_LegacyUpdaterFallback(t *testing.T) {
	legacy := &callbackOnlyUpdater{inner: resultstore.New(1, []string{"/f/legacy.mp4"})}
	upsertWriteBackResultWithProvenance(legacy, "/f/legacy.mp4", &resultstore.MovieResult{Status: models.JobStatusFailed}, frozenApplyProvenance())
}
