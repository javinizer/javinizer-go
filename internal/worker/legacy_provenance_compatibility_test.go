package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// Store deliberately promises no AtomicResultCommitter capability. This is the
// same legacy-store boundary used by evidenceLegacyStore: implementations of the
// published Store contract may publish ordinary attribution in a separate call,
// but must not publish or clear an offline report without the optional atomic
// capability. Delegate storage/CAS to a real store, recording only call order.
type legacyPublicationStore struct {
	resultstore.Store
	calls []string
}

func (s *legacyPublicationStore) CommitResult(path string, result *resultstore.MovieResult, revision uint64) error {
	s.calls = append(s.calls, "commit")
	return s.Store.CommitResult(path, result, revision)
}

func (s *legacyPublicationStore) SetProvenance(path string, prov *resultstore.ProvenanceData) {
	s.calls = append(s.calls, "provenance")
	s.Store.SetProvenance(path, prov)
}

var _ resultstore.Store = (*legacyPublicationStore)(nil)

func TestLegacyProvenanceRescrapeCompatibility(t *testing.T) {
	for _, family := range []bool{false, true} {
		name := "direct"
		if family {
			name = "family_locked"
		}
		t.Run(name, func(t *testing.T) {
			for _, stale := range []bool{false, true} {
				t.Run(map[bool]string{false: "publish_after_commit", true: "stale_publishes_neither"}[stale], func(t *testing.T) {
					const path = "movie.mp4"
					store := resultstore.New(1, []string{path})
					seedFamilyResult(store, path, "stable-result", "MOV-1", "")
					oldProv := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "old-source"}}
					store.SetProvenance(path, oldProv)
					before := store.SnapshotData()
					legacy := &legacyPublicationStore{Store: store}
					_, atomic := any(legacy).(resultstore.AtomicResultCommitter)
					require.False(t, atomic, "fixture must model the published legacy Store contract")
					var accessor resultstore.ResultMapAccessor = legacy
					if family {
						accessor = &familyKeyedResultMap{ResultMapAccessor: legacy, updater: legacy, registry: newKeyedMutexRegistry()}
					}
					incoming := &resultstore.MovieResult{ResultID: "stable-result", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "MOV-1", Title: "refreshed"}}
					prov := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "new-source"}}
					revision := before.Results[path].Revision
					if stale {
						revision++
					}
					outcome, err := NewRescrapePhase().CompleteRescrape(rescrapePhaseInputs{ResultMap: accessor}, path, incoming, revision, "MOV-1", "MOV-1", prov)
					require.NoError(t, err)
					if stale {
						require.Equal(t, models.RescrapeStatusConflict, outcome.Status)
						require.Equal(t, []string{"commit"}, legacy.calls)
						require.Equal(t, before, store.SnapshotData(), "failed CAS cannot publish attribution or advance the result")
						return
					}
					require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
					require.Equal(t, []string{"commit", "provenance"}, legacy.calls)
					got, err := store.GetMovieResult(path)
					require.NoError(t, err)
					require.Equal(t, "refreshed", got.Movie.Title)
					require.Equal(t, "stable-result", got.ResultID)
					require.Greater(t, got.Revision, before.Results[path].Revision)
					require.Equal(t, got.Revision, *outcome.Revision)
					require.Equal(t, map[string]uint64{"stable-result": got.Revision}, outcome.Revisions)
					require.Equal(t, prov, store.GetProvenance(path))
					prov.FieldSources["title"] = "caller mutation"
					require.Equal(t, "new-source", store.GetProvenance(path).FieldSources["title"], "published attribution remains owned by the store")
				})
			}
		})
	}
}

func TestLegacyFamilyProvenanceNoPublisherAndEmptyAttribution(t *testing.T) {
	for _, test := range []struct {
		name        string
		withUpdater bool
		prov        *resultstore.ProvenanceData
	}{
		{name: "nil attribution", withUpdater: true},
		{name: "empty attribution", withUpdater: true, prov: &resultstore.ProvenanceData{}},
		{name: "no publisher", prov: &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "new"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			const path = "movie.mp4"
			store := resultstore.New(1, []string{path})
			seedFamilyResult(store, path, "stable-result", "MOV-1", "")
			prior := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "retained"}}
			store.SetProvenance(path, prior)
			legacy := &legacyPublicationStore{Store: store}
			wrapped := &familyKeyedResultMap{ResultMapAccessor: legacy, registry: newKeyedMutexRegistry()}
			if test.withUpdater {
				wrapped.updater = legacy
			}
			before, err := store.GetMovieResult(path)
			require.NoError(t, err)
			incoming := before.Clone()
			incoming.Movie.Title = "updated"
			revisions, err := wrapped.CommitResultWithProvenanceAndRevisions(path, incoming, before.Revision, test.prov)
			require.NoError(t, err)
			require.Equal(t, []string{"commit"}, legacy.calls)
			require.Equal(t, prior, store.GetProvenance(path), "absence of attribution must not erase historical ordinary sources")
			got, err := store.GetMovieResult(path)
			require.NoError(t, err)
			require.Equal(t, "updated", got.Movie.Title)
			require.Equal(t, map[string]uint64{"stable-result": got.Revision}, revisions)
		})
	}
}

func TestLegacyFamilyEvidencePublicationRefusedBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name        string
		withUpdater bool
		historical  bool
		errText     string
	}{
		{name: "incoming without publisher", errText: "no provenance publisher"},
		{name: "incoming without atomic capability", withUpdater: true, errText: "does not support atomic publication"},
		{name: "historical report cannot be cleared", withUpdater: true, historical: true, errText: "does not support atomic publication"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const path = "movie.mp4"
			store := resultstore.New(1, []string{path})
			seedFamilyResult(store, path, "stable-result", "MOV-1", "")
			prov := &resultstore.ProvenanceData{OfflineEvidence: workerEvidence("incoming")}
			if test.historical {
				store.SetProvenance(path, &resultstore.ProvenanceData{OfflineEvidence: workerEvidence("historical")})
				prov = nil
			}
			before := store.SnapshotData()
			legacy := &legacyPublicationStore{Store: store}
			wrapped := &familyKeyedResultMap{ResultMapAccessor: legacy, registry: newKeyedMutexRegistry()}
			if test.withUpdater {
				wrapped.updater = legacy
			}
			incoming := before.Results[path].Clone()
			incoming.Movie.Title = "must not publish"
			revisions, err := wrapped.CommitResultWithProvenanceAndRevisions(path, incoming, incoming.Revision, prov)
			require.ErrorContains(t, err, test.errText)
			require.Nil(t, revisions)
			require.Empty(t, legacy.calls, "refusal must precede either legacy write")
			require.Equal(t, before, store.SnapshotData(), "result, revisions, and historical evidence remain unchanged")
		})
	}
}
