package resultstore

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Direct unit tests for MovieLookup/FileFinder methods that previously had
// coverage only through BatchJob integration tests. These exercise the Store
// interface alone — no BatchJob, workflow, or worker package types required.

func TestStore_FindMovieResultForMovieID(t *testing.T) {
	rt := New(2, []string{"a.mp4", "b.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("a.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "a.mp4", MovieID: "MID-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})

	mr, err := rt.FindMovieResultForMovieID("MID-1")
	require.NoError(t, err)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "MID-1", mr.Movie.ID)

	// Case-insensitive lookup.
	mr2, err := rt.FindMovieResultForMovieID("mid-1")
	require.NoError(t, err)
	assert.Equal(t, "MID-1", mr2.Movie.ID)

	_, err = rt.FindMovieResultForMovieID("NOPE")
	assert.Error(t, err)
}

func TestStore_GetMovieResultsForMovieID(t *testing.T) {
	rt := New(2, []string{"a.mp4", "b.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("a.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "a.mp4", MovieID: "MID-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})
	rt.UpdateFileResult("b.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "b.mp4", MovieID: "MID-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})

	results := rt.GetMovieResultsForMovieID("MID-1")
	assert.Len(t, results, 2)
	assert.Empty(t, rt.GetMovieResultsForMovieID("none"))
}

func TestStore_GetFileMatchInfosForMovieID(t *testing.T) {
	rt := New(2, []string{"a.mp4", "b.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("a.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "a.mp4", MovieID: "MID-1", PartNumber: 1},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})
	rt.UpdateFileResult("b.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "b.mp4", MovieID: "MID-1", PartNumber: 2},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})

	infos := rt.GetFileMatchInfosForMovieID("MID-1")
	assert.Len(t, infos, 2)
}

func TestStore_OtherResultUsesMovieID(t *testing.T) {
	rt := New(2, []string{"a.mp4", "b.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("a.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "a.mp4", MovieID: "MID-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})
	rt.UpdateFileResult("b.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "b.mp4", MovieID: "MID-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})

	assert.True(t, rt.OtherResultUsesMovieID("a.mp4", "MID-1"), "b.mp4 also uses MID-1")
	assert.False(t, rt.OtherResultUsesMovieID("a.mp4", "NOPE"))
}

func TestStore_CommitResult_RevisionConflict(t *testing.T) {
	rt := New(1, []string{"a.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("a.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "a.mp4", MovieID: "MID-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})
	currentRev := rt.GetRevision("a.mp4")

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "a.mp4", MovieID: "MID-2"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-2"},
	}

	// Matching revision commits and bumps revision.
	require.NoError(t, rt.CommitResult("a.mp4", newResult, currentRev))
	assert.Equal(t, currentRev+1, rt.GetRevision("a.mp4"))

	// Stale revision conflicts.
	conflict := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "a.mp4", MovieID: "MID-3"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-3"},
	}
	err := rt.CommitResult("a.mp4", conflict, currentRev)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

// TestStore_IndependentOfWorker verifies the Store compiles and operates
// without importing internal/worker (task 8.12).
func TestStore_IndependentOfWorker(t *testing.T) {
	rt := New(5, []string{"a.mp4", "b.mp4", "c.mp4", "d.mp4", "e.mp4"})
	_, prog := rt.SnapshotForStatus()
	assert.Equal(t, 5, prog.TotalFiles)
	assert.Equal(t, 0, prog.Completed)
	assert.Equal(t, 0, prog.Failed)
	assert.InDelta(t, 0.0, prog.Progress, 0.001)
}

func TestStore_NewFromSnapshot_RebuildsIndexes(t *testing.T) {
	results := map[string]*MovieResult{
		"a.mp4": {ResultID: "rid-a", FileMatchInfo: models.FileMatchInfo{MovieID: "MID-1"}, Movie: &models.Movie{ID: "MID-1"}, Status: models.JobStatusCompleted},
	}
	rt := NewFromSnapshot(1, []string{"a.mp4"}, results, nil, nil, nil, 0, 0, 0)

	// movieIDIndex rebuilt.
	paths := rt.FindFilePathsForMovieID("MID-1")
	assert.Contains(t, paths, "a.mp4")

	// resultIDIndex rebuilt.
	mr, fp, ok := rt.GetFileResultByResultID("rid-a")
	require.True(t, ok)
	assert.Equal(t, "a.mp4", fp)
	require.NotNil(t, mr.Movie)
}

func TestStore_ReplaceResultRaw_RebuildsIndexes(t *testing.T) {
	rt := New(1, []string{"a.mp4"}).(*ResultTracker)
	rt.ReplaceResultRaw("a.mp4", &MovieResult{
		ResultID:      "rid-a",
		FileMatchInfo: models.FileMatchInfo{MovieID: "MID-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MID-1"},
	})
	// Indexes rebuilt despite no revision bump / progress recalculation.
	paths := rt.FindFilePathsForMovieID("MID-1")
	assert.Contains(t, paths, "a.mp4")
	mr, _, ok := rt.GetFileResultByResultID("rid-a")
	require.True(t, ok)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, uint64(0), rt.GetRevision("a.mp4"), "ReplaceResultRaw must not bump revision")
}

func TestStore_LoadResultsRaw_AndRebuildIndexes(t *testing.T) {
	rt := New(0, nil).(*ResultTracker)
	results := map[string]*MovieResult{
		"a.mp4": {ResultID: "rid-a", FileMatchInfo: models.FileMatchInfo{MovieID: "MID-1"}, Movie: &models.Movie{ID: "MID-1"}, Status: models.JobStatusCompleted},
	}
	fmi := map[string]models.FileMatchInfo{"a.mp4": {MovieID: "MID-1"}}
	rt.LoadResultsRaw(results, fmi)
	rt.RebuildIndexes()

	paths := rt.FindFilePathsForMovieID("MID-1")
	assert.Contains(t, paths, "a.mp4")
	info, ok := rt.GetFileMatchInfo("a.mp4")
	require.True(t, ok)
	assert.Equal(t, "MID-1", info.MovieID)
}

func TestStore_ForceCompleteProgress(t *testing.T) {
	rt := New(2, []string{"a.mp4", "b.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("a.mp4", &MovieResult{Status: models.JobStatusCompleted})
	// One of two completed → 50%.
	_, prog := rt.SnapshotForStatus()
	assert.InDelta(t, 50.0, prog.Progress, 0.1)

	rt.ForceCompleteProgress()
	_, prog = rt.SnapshotForStatus()
	assert.InDelta(t, 100.0, prog.Progress, 0.001)
}

func TestStore_RawResults_ReturnsMutablePointers(t *testing.T) {
	rt := New(1, []string{"a.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("a.mp4", &MovieResult{Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "MID-1"}})
	raw := rt.RawResults()
	assert.Len(t, raw, 1)
	_, ok := raw["a.mp4"]
	assert.True(t, ok)
}

func TestStore_SetGoneChecker(t *testing.T) {
	rt := New(0, nil)
	assert.False(t, rt.IsGone())
	rt.SetGoneChecker(func() bool { return true })
	assert.True(t, rt.IsGone())
}

func TestStore_ConcurrentSetGoneCheckerAndIsGone(t *testing.T) {
	store := New(3, []string{"a.mp4", "b.mp4", "c.mp4"})
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			store.SetGoneChecker(func() bool { return i%2 == 0 })
		}
	}()

	for i := 0; i < 1000; i++ {
		_ = store.IsGone()
	}

	<-done
}
