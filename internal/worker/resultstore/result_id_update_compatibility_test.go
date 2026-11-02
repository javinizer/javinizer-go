package resultstore_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestResultUpdateIdentityNormalizationCompatibility(t *testing.T) {
	for _, tc := range []struct{ name, initial, incoming, want string }{
		{"first assigned ID", "", "new-id", "new-id"},
		{"matching stable ID", "stable-id", "stable-id", "stable-id"},
		{"omitted IDs remain unassigned", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const path = "file.mp4"
			s := resultstore.New(1, []string{path})
			s.UpdateFileResult(path, &resultstore.MovieResult{ResultID: tc.initial, Status: models.JobStatusRunning})
			s.UpdateFileResult(path, &resultstore.MovieResult{ResultID: tc.incoming, Status: models.JobStatusCompleted})
			r := s.GetResults()
			require.Len(t, r, 1)
			require.Equal(t, tc.want, r[0].ResultID)
			require.Equal(t, uint64(2), r[0].Revision)
			resolved, actualPath, found := s.GetFileResultByResultID(tc.want)
			if tc.want == "" {
				require.False(t, found, "updates must not invent identity for legacy unassigned records")
			} else {
				require.True(t, found)
				require.Equal(t, path, actualPath)
				require.Equal(t, r[0], *resolved)
			}
			_, progress := s.SnapshotForStatus()
			require.Equal(t, 1, progress.Completed)
			require.Zero(t, progress.Failed)
			require.Equal(t, float64(100), progress.Progress)
		})
	}
}

func TestResultUpdateDiscardedIDCannotHijackSiblingAndKeepsExclusion(t *testing.T) {
	const first, sibling = "first.mp4", "sibling.mp4"
	s := resultstore.New(2, []string{first, sibling})
	s.UpdateFileResult(first, &resultstore.MovieResult{ResultID: "first-id", FileMatchInfo: models.FileMatchInfo{MovieID: "OLD-001"}, Status: models.JobStatusFailed})
	s.UpdateFileResult(sibling, &resultstore.MovieResult{ResultID: "sibling-id", FileMatchInfo: models.FileMatchInfo{MovieID: "OTHER-002"}, Status: models.JobStatusRunning})
	s.SetProvenance(first, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "kept"}})
	s.MarkExcluded(first)
	before, siblingPath, found := s.GetFileResultByResultID("sibling-id")
	require.True(t, found)
	require.Equal(t, sibling, siblingPath)
	s.UpdateFileResult(first, &resultstore.MovieResult{ResultID: "sibling-id", FileMatchInfo: models.FileMatchInfo{MovieID: "NEW-003"}, Status: models.JobStatusCompleted})
	current, path, found := s.GetFileResultByResultID("first-id")
	require.True(t, found)
	require.Equal(t, first, path)
	require.Equal(t, "first-id", current.ResultID)
	require.Equal(t, uint64(2), current.Revision)
	after, path, found := s.GetFileResultByResultID("sibling-id")
	require.True(t, found)
	require.Equal(t, sibling, path)
	require.Equal(t, before, after, "discarded incoming alias must not steal an unrelated result's index")
	require.Empty(t, s.FindFilePathsForMovieID("OLD-001"))
	require.Equal(t, []string{first}, s.FindFilePathsForMovieID("new-003"))
	require.Equal(t, []string{sibling}, s.FindFilePathsForMovieID("other-002"))
	state, progress := s.SnapshotForStatus()
	require.True(t, state.Excluded[first])
	require.Zero(t, progress.Completed)
	require.Zero(t, progress.Failed)
	require.Zero(t, progress.Progress)
	require.Equal(t, "kept", s.GetProvenance(first).FieldSources["title"])
}
