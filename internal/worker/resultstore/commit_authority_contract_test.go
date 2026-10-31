package resultstore_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestCommitResultPreservesAuthorityAndIdentityAcrossConflict(t *testing.T) {
	const path = "/owned/first.mp4"
	const sibling = "/owned/second.mp4"
	store := resultstore.New(2, []string{path, sibling})
	authoritative := models.FileMatchInfo{
		Path: path, MovieID: "SCAN-1", IsMultiPart: true, PartNumber: 2,
		PartSuffix: "-part2", Name: "first", Extension: ".mp4", Size: 1234,
	}
	store.SetFileMatchInfoMap(map[string]models.FileMatchInfo{path: authoritative})
	store.UpdateFileResult(path, &resultstore.MovieResult{
		ResultID: "stable-first", FileMatchInfo: authoritative,
		Status: models.JobStatusRunning, Movie: &models.Movie{ID: "OLD-1", Title: "Before"},
	})
	store.UpdateFileResult(sibling, &resultstore.MovieResult{
		ResultID: "stable-second", Status: models.JobStatusFailed,
		FileMatchInfo: models.FileMatchInfo{Path: sibling, MovieID: "SIBLING-1"},
	})
	store.SetProvenance(path, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "original-source"}})
	before, progressBefore := store.SnapshotForStatus()
	revision := store.GetRevision(path)
	require.Equal(t, uint64(1), revision)
	require.Equal(t, resultstore.ProgressSnapshot{TotalFiles: 2, Failed: 1, Progress: 50}, progressBefore)

	candidate := &resultstore.MovieResult{
		ResultID: "replacement-id", Revision: 99, Status: models.JobStatusCompleted,
		FileMatchInfo: models.FileMatchInfo{Path: "/wrong/path.mp4", MovieID: "WRONG-1"},
		Movie:         &models.Movie{ID: "NEW-1", Title: "After"},
	}
	callerBefore := candidate.Clone()
	err := store.CommitResult(path, candidate, revision-1)
	require.EqualError(t, err, "conflict: expected revision 0, got 1")
	require.Equal(t, callerBefore, candidate, "a rejected CAS must not rewrite the proposed result")
	afterConflict, progressAfterConflict := store.SnapshotForStatus()
	require.Equal(t, before, afterConflict)
	require.Equal(t, progressBefore, progressAfterConflict)
	require.Equal(t, []string{path}, store.FindFilePathsForMovieID("OLD-1"))
	require.Empty(t, store.FindFilePathsForMovieID("NEW-1"))

	// Retry through the same public CAS API. Scanned match metadata and the
	// established result identity belong to the store, not the replacement.
	require.NoError(t, store.CommitResult(path, candidate, revision))
	want := callerBefore.Clone()
	want.FileMatchInfo = authoritative
	want.ResultID = "stable-first"
	want.Revision = revision + 1
	require.Equal(t, want, candidate, "the accepted caller receives the authoritative identity and revision")
	expected := before
	expected.Results[path] = want
	after, progressAfter := store.SnapshotForStatus()
	require.Equal(t, expected, after, "preserve sibling, provenance, match map and stable result-ID index")
	require.Equal(t, resultstore.ProgressSnapshot{TotalFiles: 2, Completed: 1, Failed: 1, Progress: 100}, progressAfter)
	require.Empty(t, store.FindFilePathsForMovieID("OLD-1"))
	require.Empty(t, store.FindFilePathsForMovieID("WRONG-1"))
	require.Equal(t, []string{path}, store.FindFilePathsForMovieID("new-1"))
	require.Equal(t, []string{path}, store.FindFilePathsForMovieID("SCAN-1"))
	found, foundPath, ok := store.GetFileResultByResultID("stable-first")
	require.True(t, ok)
	require.Equal(t, path, foundPath)
	require.Equal(t, want, found)
	missing, missingPath, ok := store.GetFileResultByResultID("replacement-id")
	require.False(t, ok)
	require.Nil(t, missing)
	require.Empty(t, missingPath)
}
