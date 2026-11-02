package resultstore

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func minimalStateProvenance() *ProvenanceData {
	return &ProvenanceData{
		FieldSources:    map[string]string{"title": "source"},
		OfflineEvidence: reportFixture("minimal"),
	}
}

func TestProvenanceHelpersInitializeAndOwnMinimalState(t *testing.T) {
	// Private read/write helpers explicitly tolerate an absent provenance map.
	// Public New/NewFromSnapshot initialize it; this is not a claim that the
	// exported ResultTracker's zero value is a usable public store.
	state := &resultTrackerState{}
	reader := &resultReadStore{resultTrackerState: state}
	writer := &resultUpdater{resultTrackerState: state}
	require.Nil(t, reader.GetProvenance("file"))
	input := minimalStateProvenance()
	want := input.Clone()
	writer.SetProvenance("file", input)
	input.FieldSources["title"] = "caller mutation"
	input.OfflineEvidence.Observations[0].Aliases[0] = "caller alias"
	*input.OfflineEvidence.Observations[0].ObservedAt = input.OfflineEvidence.AdmittedAt.AddDate(1, 0, 0)
	got := reader.GetProvenance("file")
	require.Equal(t, want, got)
	got.FieldSources["title"] = "reader mutation"
	got.OfflineEvidence.Observations[0].Aliases[0] = "reader alias"
	require.Equal(t, want, reader.GetProvenance("file"))
	require.Nil(t, reader.GetProvenance("missing"))
}

func TestEvidenceCommitInitializesProvenanceWithoutBreakingOwnership(t *testing.T) {
	// Construct consistent empty state through the existing package test helper.
	// Results and indexes start empty together; no indexed row is removed and no
	// initialized production field is corrupted to manufacture this boundary.
	store := newPopulatedTracker(&resultTrackerState{TotalFiles: 1, Files: []string{"file"}})
	require.Nil(t, store.Provenance)
	candidate := &MovieResult{ResultID: "stable", Movie: &models.Movie{ID: "TEST-001", Title: "fresh"}, Status: models.JobStatusCompleted}
	input := minimalStateProvenance()
	want := input.Clone()
	require.NoError(t, store.CommitResultAndProvenance("file", candidate, 0, input))
	require.Equal(t, uint64(1), candidate.Revision)
	result, path, found := store.GetFileResultByResultID("stable")
	require.True(t, found)
	require.Equal(t, "file", path)
	require.Equal(t, "TEST-001", result.Movie.ID)
	lookup, err := store.FindFileForMovieID("TEST-001")
	require.NoError(t, err)
	require.Equal(t, "file", lookup.FilePath)
	require.Equal(t, uint64(1), lookup.CapturedRevision)
	_, progress := store.SnapshotForStatus()
	require.Equal(t, 1, progress.Completed)
	require.Zero(t, progress.Failed)
	require.Equal(t, float64(100), progress.Progress)
	candidate.Movie.Title = "candidate mutation"
	input.FieldSources["title"] = "input mutation"
	input.OfflineEvidence.Observations[0].Aliases[0] = "input alias"
	require.Equal(t, want, store.GetProvenance("file"))
	before := store.SnapshotData()
	require.Equal(t, "fresh", before.Results["file"].Movie.Title)
	stale := &MovieResult{ResultID: "replacement", Movie: &models.Movie{ID: "OTHER", Title: "stale"}}
	require.ErrorContains(t, store.CommitResultAndProvenance("file", stale, 0, minimalStateProvenance()), "conflict:")
	require.Zero(t, stale.Revision)
	require.Equal(t, before, store.SnapshotData(), "stale CAS cannot publish a result, provenance, or index")
	_, afterProgress := store.SnapshotForStatus()
	require.Equal(t, progress, afterProgress)
	before.Results["file"].Movie.Title = "snapshot mutation"
	before.Provenance["file"].OfflineEvidence.Observations[0].Aliases[0] = "snapshot alias"
	fresh, err := store.GetMovieResult("file")
	require.NoError(t, err)
	require.Equal(t, "fresh", fresh.Movie.Title)
	require.Equal(t, want, store.GetProvenance("file"))
}
