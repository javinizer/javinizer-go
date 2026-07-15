package resultstore

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPopulatedTracker returns a concrete *ResultTracker (so tests can read
// internal state fields) backed by the provided snapshot. This mirrors the
// former worker-package newResultTrackerFromState helper.
func newPopulatedTracker(s *resultTrackerState) *ResultTracker {
	if s.Results == nil {
		s.Results = make(map[string]*MovieResult)
	}
	if s.FileMatchInfo == nil {
		s.FileMatchInfo = make(map[string]models.FileMatchInfo)
	}
	if s.Excluded == nil {
		s.Excluded = make(map[string]bool)
	}
	if s.movieIDIndex == nil {
		s.movieIDIndex = make(map[string][]string)
	}
	return newResultTrackerFromState(s)
}

func TestResultTracker_GetResults_SkipsNil(t *testing.T) {
	rt := newPopulatedTracker(&resultTrackerState{
		Results: map[string]*MovieResult{
			"file1.mp4": {Status: models.JobStatusCompleted},
			"file2.mp4": nil,
		},
	})
	results := rt.GetResults()
	assert.Len(t, results, 1)
}

func TestResultTracker_SetFileMatchInfoMap(t *testing.T) {
	rt := newPopulatedTracker(&resultTrackerState{
		FileMatchInfo: map[string]models.FileMatchInfo{
			"file1.mp4": {MovieID: "OLD-001"},
		},
	})

	rt.SetFileMatchInfoMap(map[string]models.FileMatchInfo{
		"file2.mp4": {MovieID: "NEW-002"},
		"file1.mp4": {MovieID: "UPDATED-001"},
	})

	assert.Equal(t, "UPDATED-001", rt.FileMatchInfo["file1.mp4"].MovieID)
	assert.Equal(t, "NEW-002", rt.FileMatchInfo["file2.mp4"].MovieID)
}

func TestResultTracker_RecalculateProgress(t *testing.T) {
	t.Run("counts completed and failed from results", func(t *testing.T) {
		rt := newPopulatedTracker(&resultTrackerState{
			TotalFiles: 3,
			Results: map[string]*MovieResult{
				"f1": {Status: models.JobStatusCompleted},
				"f2": {Status: models.JobStatusFailed},
				"f3": {Status: models.JobStatusRunning},
			},
		})
		rt.RecalculateProgress()
		assert.Equal(t, 1, rt.Completed)
		assert.Equal(t, 1, rt.Failed)
		assert.InDelta(t, 66.67, rt.Progress, 0.1) // (1+1)/3 * 100
	})

	t.Run("sets 100% when TotalFiles is 0", func(t *testing.T) {
		rt := newPopulatedTracker(&resultTrackerState{TotalFiles: 0})
		rt.RecalculateProgress()
		assert.Equal(t, 100.0, rt.Progress)
	})
}

func TestResultTracker_RecalculateProgress_SkipsExcluded(t *testing.T) {
	rt := newPopulatedTracker(&resultTrackerState{
		TotalFiles: 5,
		Excluded:   map[string]bool{"f2": true, "f5": true},
		Results: map[string]*MovieResult{
			"f1": {Status: models.JobStatusCompleted},
			"f2": {Status: models.JobStatusCompleted}, // excluded
			"f3": {Status: models.JobStatusFailed},
			"f4": {Status: models.JobStatusCompleted},
			"f5": {Status: models.JobStatusFailed}, // excluded
		},
	})
	rt.RecalculateProgress()
	assert.Equal(t, 2, rt.Completed, "Completed should exclude excluded files")
	assert.Equal(t, 1, rt.Failed, "Failed should exclude excluded files")
	assert.InDelta(t, 100.0, rt.Progress, 0.1)
}

func TestResultTracker_CloneResults(t *testing.T) {
	rt := newPopulatedTracker(&resultTrackerState{
		Results: map[string]*MovieResult{
			"f1": {Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{MovieID: "ABC-001"}},
		},
	})
	cloned := rt.SnapshotData().Results
	assert.Len(t, cloned, 1)
	assert.NotSame(t, rt.Results["f1"], cloned["f1"])
}

func TestResultReadStore_CloneResults(t *testing.T) {
	rt := New(1, []string{"f1.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("f1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "f1.mp4", MovieID: "CR-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CR-001"},
	})

	cloned := rt.CloneResults()
	assert.Len(t, cloned, 1)
	assert.NotSame(t, rt.Results["f1.mp4"], cloned["f1.mp4"])

	cloned["f1.mp4"].Movie.ID = "MODIFIED"
	assert.Equal(t, "CR-001", rt.Results["f1.mp4"].Movie.ID, "mutation of clone should not affect original")
}

func TestResultReadStore_CloneResults_NilEntries(t *testing.T) {
	rt := newPopulatedTracker(&resultTrackerState{
		Results: map[string]*MovieResult{
			"f1": {Status: models.JobStatusCompleted},
			"f2": nil,
		},
	})
	cloned := rt.CloneResults()
	assert.Len(t, cloned, 1, "nil entries should be skipped")
	_, hasF1 := cloned["f1"]
	assert.True(t, hasF1)
}

func TestResultReadStore_SnapshotForStatus(t *testing.T) {
	rt := New(2, []string{"f1.mp4", "f2.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("f1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "f1.mp4", MovieID: "SFS-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SFS-001"},
	})

	resultSnap, progressSnap := rt.SnapshotForStatus()
	assert.Len(t, resultSnap.Results, 1)
	assert.Equal(t, 2, progressSnap.TotalFiles)
	assert.Equal(t, 1, progressSnap.Completed)
	assert.Greater(t, progressSnap.Progress, 0.0)
}

func TestResultUpdater_SetFileMatchInfo(t *testing.T) {
	rt := New(1, []string{"f1.mp4"}).(*ResultTracker)
	info := models.FileMatchInfo{MovieID: "SFMI-001", IsMultiPart: true, PartNumber: 2}
	rt.SetFileMatchInfo("f1.mp4", info)

	retrieved, ok := rt.GetFileMatchInfo("f1.mp4")
	assert.True(t, ok)
	assert.Equal(t, "SFMI-001", retrieved.MovieID)
	assert.True(t, retrieved.IsMultiPart)
	assert.Equal(t, 2, retrieved.PartNumber)
}

func TestStateUpdateProgressFromCounters_ZeroTotal(t *testing.T) {
	s := &resultTrackerState{TotalFiles: 0, Completed: 0, Failed: 0}
	stateUpdateProgressFromCounters(s)
	assert.Equal(t, 100.0, s.Progress)
}

func TestStateUpdateProgressFromCounters_NonZeroTotal(t *testing.T) {
	s := &resultTrackerState{TotalFiles: 10, Completed: 3, Failed: 2}
	stateUpdateProgressFromCounters(s)
	assert.InDelta(t, 50.0, s.Progress, 0.1)
}

func TestStateLookupFilePathForResultIDLocked_NilIndex(t *testing.T) {
	s := &resultTrackerState{resultIDIndex: nil}
	fp, ok := stateLookupFilePathForResultIDLocked(s, "some-id")
	assert.False(t, ok)
	assert.Empty(t, fp)
}

func TestStateLookupFilePathForResultIDLocked_Found(t *testing.T) {
	s := &resultTrackerState{
		resultIDIndex: map[string]string{"result-1": "file1.mp4"},
	}
	fp, ok := stateLookupFilePathForResultIDLocked(s, "result-1")
	assert.True(t, ok)
	assert.Equal(t, "file1.mp4", fp)
}

func TestResultReadStore_IsGone_NoChecker(t *testing.T) {
	rt := New(0, nil)
	assert.False(t, rt.IsGone())
}

func TestResultReadStore_IsGone_WithChecker(t *testing.T) {
	rt := New(0, nil).(*ResultTracker)
	rt.goneChecker = func() bool { return true }
	assert.True(t, rt.IsGone())
}

func TestResultReadStore_GetFileResultByResultID_Found(t *testing.T) {
	rt := New(1, []string{"f1.mp4"}).(*ResultTracker)
	rt.UpdateFileResult("f1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "f1.mp4", MovieID: "GFR-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "GFR-001"},
	})

	var resultID string
	for _, r := range rt.Results {
		if r != nil && r.ResultID != "" {
			resultID = r.ResultID
			break
		}
	}
	if resultID == "" {
		t.Skip("ResultID not set by UpdateFileResult")
	}

	mr, filePath, ok := rt.GetFileResultByResultID(resultID)
	require.True(t, ok)
	assert.Equal(t, "f1.mp4", filePath)
	assert.Equal(t, "f1.mp4", mr.FileMatchInfo.Path)
}

func TestResultReadStore_GetFileResultByResultID_NotFound(t *testing.T) {
	rt := New(0, nil)
	_, _, ok := rt.GetFileResultByResultID("nonexistent-id")
	assert.False(t, ok)
}

func TestResultTracker_CloneProvenance(t *testing.T) {
	rt := newPopulatedTracker(&resultTrackerState{
		Provenance: map[string]*ProvenanceData{
			"f1": {FieldSources: map[string]string{"title": "src1"}},
		},
	})
	cloned := rt.SnapshotData().Provenance
	assert.Equal(t, "src1", cloned["f1"].FieldSources["title"])
	cloned["f1"].FieldSources["title"] = "modified"
	assert.Equal(t, "src1", rt.Provenance["f1"].FieldSources["title"], "clone should be independent")
}

func TestResultTracker_CloneFileMatchInfo(t *testing.T) {
	rt := newPopulatedTracker(&resultTrackerState{
		FileMatchInfo: map[string]models.FileMatchInfo{
			"f1": {MovieID: "ABC-001"},
		},
	})
	cloned := rt.CloneFileMatchInfo()
	assert.Equal(t, "ABC-001", cloned["f1"].MovieID)
}

func TestMovieIDsForResult_CaseInsensitiveDedup(t *testing.T) {
	t.Run("same ID different case deduplicates", func(t *testing.T) {
		r := &MovieResult{
			Movie: &models.Movie{ID: "ABC-001"},
			FileMatchInfo: models.FileMatchInfo{
				MovieID: "abc-001",
			},
		}
		ids := movieIDsForResult(r)
		assert.Len(t, ids, 1, "case-only variants should deduplicate to one ID")
		assert.Equal(t, "ABC-001", ids[0])
	})

	t.Run("different IDs both returned", func(t *testing.T) {
		r := &MovieResult{
			Movie: &models.Movie{ID: "ABC-001"},
			FileMatchInfo: models.FileMatchInfo{
				MovieID: "DEF-002",
			},
		}
		ids := movieIDsForResult(r)
		assert.Len(t, ids, 2)
	})

	t.Run("nil movie returns FileMatchInfo MovieID", func(t *testing.T) {
		r := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{
				MovieID: "ABC-001",
			},
		}
		ids := movieIDsForResult(r)
		assert.Len(t, ids, 1)
		assert.Equal(t, "ABC-001", ids[0])
	})

	t.Run("nil result returns nil", func(t *testing.T) {
		ids := movieIDsForResult(nil)
		assert.Nil(t, ids)
	})
}
