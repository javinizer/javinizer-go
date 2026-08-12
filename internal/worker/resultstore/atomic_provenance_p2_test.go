package resultstore

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING P2 (R10-6): state and provenance commit through ONE
// atomic primitive. Two separate acquisitions (AtomicUpdateFileResult +
// SetProvenance) let a persist snapshot observe new state with old
// provenance; the dual-write primitive takes the lock once, bumps the
// revision once, and publishes both.
func TestAtomicUpdateFileResultWithProvenance_DualWriteOneRevision(t *testing.T) {
	s := New(1, []string{"/f/a.mp4"})
	s.UpdateFileResult("/f/a.mp4", &MovieResult{
		ResultID: "res-1", Status: models.JobStatusRunning,
		Movie: &models.Movie{ID: "P-1", Title: "before"},
	})
	s.SetProvenance("/f/a.mp4", &ProvenanceData{FieldSources: map[string]string{"title": "scraper-a"}})
	before, err := s.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)

	var seenMovie *models.Movie
	var seenProv *ProvenanceData
	err = s.AtomicUpdateFileResultWithProvenance("/f/a.mp4", func(cur *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		seenMovie = cur.Movie
		seenProv = prov
		cur.Status = models.JobStatusCompleted
		return cur, &ProvenanceData{FieldSources: map[string]string{"title": "user"}}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, seenMovie, "callback observes the current result")
	require.NotNil(t, seenProv, "callback observes the current provenance in the same acquisition")
	assert.Equal(t, "scraper-a", seenProv.FieldSources["title"])

	after, err := s.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, before.Revision+1, after.Revision, "one revision bump for the dual write")
	assert.Equal(t, models.JobStatusCompleted, after.Status)
	gotProv := s.GetProvenance("/f/a.mp4")
	require.NotNil(t, gotProv)
	assert.Equal(t, "user", gotProv.FieldSources["title"])
}

// The callback erroring must publish NEITHER half — state and provenance stay
// at their pre-call values with no revision bump.
func TestAtomicUpdateFileResultWithProvenance_ErrorPublishesNothing(t *testing.T) {
	s := New(1, []string{"/f/b.mp4"})
	s.UpdateFileResult("/f/b.mp4", &MovieResult{ResultID: "res-2", Status: models.JobStatusRunning})
	s.SetProvenance("/f/b.mp4", &ProvenanceData{FieldSources: map[string]string{"title": "scraper"}})
	before, err := s.GetMovieResult("/f/b.mp4")
	require.NoError(t, err)

	err = s.AtomicUpdateFileResultWithProvenance("/f/b.mp4", func(cur *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		cur.Status = models.JobStatusFailed
		return cur, &ProvenanceData{FieldSources: map[string]string{"title": "user"}}, errSimulated
	})
	require.ErrorIs(t, err, errSimulated)
	after, aerr := s.GetMovieResult("/f/b.mp4")
	require.NoError(t, aerr)
	assert.Equal(t, before.Revision, after.Revision, "no bump on callback failure")
	assert.Equal(t, models.JobStatusRunning, after.Status)
	assert.Equal(t, "scraper", s.GetProvenance("/f/b.mp4").FieldSources["title"])
}

// A missing row must surface the not-found error BEFORE any write — the
// dual primitive follows AtomicUpdateFileResult's contract exactly.
func TestAtomicUpdateFileResultWithProvenance_NotFound(t *testing.T) {
	s := New(1, []string{"/f/missing.mp4"})
	called := false
	err := s.AtomicUpdateFileResultWithProvenance("/f/missing.mp4", func(cur *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		called = true
		return cur, prov, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file result not found")
	assert.False(t, called, "callback never runs without an existing row")
}

// Terminal-status counter migration: Completed→Failed transitions the
// counters one-for-one under the single revision bump.
func TestAtomicUpdateFileResultWithProvenance_TerminalStatusCounters(t *testing.T) {
	s := New(1, []string{"/f/t.mp4"})
	s.UpdateFileResult("/f/t.mp4", &MovieResult{ResultID: "res-t", Status: models.JobStatusCompleted})
	_, before := s.SnapshotForStatus()
	require.Equal(t, 1, before.Completed)

	err := s.AtomicUpdateFileResultWithProvenance("/f/t.mp4", func(cur *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		cur.Status = models.JobStatusFailed
		return cur, prov, nil
	})
	require.NoError(t, err)
	_, after := s.SnapshotForStatus()
	assert.Equal(t, 0, after.Completed)
	assert.Equal(t, 1, after.Failed, "completed counter moves to failed exactly once")

	// And back: Failed→Completed re-covers the increment arms.
	err = s.AtomicUpdateFileResultWithProvenance("/f/t.mp4", func(cur *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		cur.Status = models.JobStatusCompleted
		return cur, prov, nil
	})
	require.NoError(t, err)
	_, again := s.SnapshotForStatus()
	assert.Equal(t, 1, again.Completed)
	assert.Equal(t, 0, again.Failed)
}

// Excluded files take the full-recalc path instead of the counter fast path.
func TestAtomicUpdateFileResultWithProvenance_ExcludedFileRecalculates(t *testing.T) {
	s := New(1, []string{"/f/x.mp4"})
	s.UpdateFileResult("/f/x.mp4", &MovieResult{ResultID: "res-x", Status: models.JobStatusRunning})
	s.MarkExcluded("/f/x.mp4")
	_, before := s.SnapshotForStatus()

	err := s.AtomicUpdateFileResultWithProvenance("/f/x.mp4", func(cur *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		cur.Status = models.JobStatusCompleted
		return cur, &ProvenanceData{FieldSources: map[string]string{"title": "scraper"}}, nil
	})
	require.NoError(t, err)
	_, after := s.SnapshotForStatus()
	assert.Equal(t, before.Completed, after.Completed, "excluded rows never shift the counters")
	assert.Equal(t, "scraper", s.GetProvenance("/f/x.mp4").FieldSources["title"])
}

// Bare states (no constructor) must not panic on the provenance map — the
// lazy-init leg keeps reconstructed/minimal states safe.
func TestAtomicUpdateFileResultWithProvenance_NilProvenanceMapInitialized(t *testing.T) {
	state := &resultTrackerState{
		Results:      map[string]*MovieResult{"/f/bare.mp4": {Status: models.JobStatusRunning}},
		Excluded:     map[string]bool{},
		movieIDIndex: map[string][]string{},
	}
	ru := &resultUpdater{resultTrackerState: state}
	err := ru.AtomicUpdateFileResultWithProvenance("/f/bare.mp4", func(cur *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		require.Nil(t, prov)
		return cur, &ProvenanceData{FieldSources: map[string]string{"a": "b"}}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, state.Provenance)
	assert.Equal(t, "b", state.Provenance["/f/bare.mp4"].FieldSources["a"])
}

var errSimulated = errSimulatedType{}

type errSimulatedType struct{}

func (errSimulatedType) Error() string { return "simulated callback failure" }
