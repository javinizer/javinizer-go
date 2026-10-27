package resultstore

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/stretchr/testify/require"
)

func reportFixture(digest string) *scrapeevidence.Report {
	observed := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	return &scrapeevidence.Report{Outcome: scrapeevidence.Available, SnapshotDigest: digest, AdmittedAt: observed, Observations: []scrapeevidence.Observation{{Source: "r18dev:derived_actress", SourceID: "007", Outcome: scrapeevidence.Available, JapaneseName: "Source name", Aliases: []string{"Whole alias"}, ObservedAt: &observed}}}
}

func TestEvidenceCommitOwnsBothPayloadsAndCASPreservesHistory(t *testing.T) {
	s := New(1, []string{"a"})
	s.UpdateFileResult("a", &MovieResult{ResultID: "stable", Movie: &models.Movie{ID: "ID", Title: "before"}})
	before, _ := s.GetMovieResult("a")
	c := s.(AtomicResultCommitter)
	candidate := &MovieResult{ResultID: "new", Movie: &models.Movie{ID: "ID", Title: "after"}}
	prov := &ProvenanceData{FieldSources: map[string]string{"title": "r18dev"}, OfflineEvidence: reportFixture("A")}
	require.NoError(t, c.CommitResultAndProvenance("a", candidate, before.Revision, prov))
	require.Equal(t, before.Revision+1, candidate.Revision)
	require.Equal(t, "stable", candidate.ResultID)
	candidate.Movie.Title = "mutated"
	prov.OfflineEvidence.Observations[0].Aliases[0] = "mutated"
	*prov.OfflineEvidence.Observations[0].ObservedAt = time.Time{}
	snapshot := s.SnapshotData()
	require.Equal(t, "after", snapshot.Results["a"].Movie.Title)
	require.Equal(t, "Whole alias", snapshot.Provenance["a"].OfflineEvidence.Observations[0].Aliases[0])
	require.False(t, snapshot.Provenance["a"].OfflineEvidence.Observations[0].ObservedAt.IsZero())
	stale := &MovieResult{Movie: &models.Movie{Title: "stale"}}
	require.ErrorContains(t, c.CommitResultAndProvenance("a", stale, before.Revision, &ProvenanceData{OfflineEvidence: reportFixture("B")}), "conflict:")
	require.Equal(t, uint64(0), stale.Revision)
	require.Equal(t, snapshot, s.SnapshotData())
	snapshot.Provenance["a"].OfflineEvidence.SnapshotDigest = "outside"
	require.Equal(t, "A", s.GetProvenance("a").OfflineEvidence.SnapshotDigest)
}

func TestEvidenceCommitOptOutClearsReportPreservingLegacyAttribution(t *testing.T) {
	for _, incoming := range []*ProvenanceData{nil, {}, {FieldSources: map[string]string{"title": "new"}}} {
		s := New(1, []string{"a"})
		s.UpdateFileResult("a", &MovieResult{ResultID: "stable"})
		s.SetProvenance("a", &ProvenanceData{FieldSources: map[string]string{"title": "old"}, OfflineEvidence: reportFixture("A")})
		before, _ := s.GetMovieResult("a")
		require.NoError(t, s.(AtomicResultCommitter).CommitResultAndProvenance("a", &MovieResult{}, before.Revision, incoming))
		got := s.GetProvenance("a")
		require.Nil(t, got.OfflineEvidence)
		want := "old"
		if incoming != nil && incoming.FieldSources != nil {
			want = "new"
		}
		require.Equal(t, want, got.FieldSources["title"])
	}
	s := New(1, []string{"a"})
	require.NoError(t, s.(AtomicResultCommitter).CommitResultAndProvenance("a", &MovieResult{}, 0, &ProvenanceData{}))
	require.Nil(t, s.GetProvenance("a"))
}

func TestEvidenceCommitConcurrentSnapshotsStayCoherent(t *testing.T) {
	s := New(1, []string{"a"})
	require.NoError(t, s.(AtomicResultCommitter).CommitResultAndProvenance("a", &MovieResult{ResultID: "stable", Movie: &models.Movie{Title: "0"}}, 0, &ProvenanceData{OfflineEvidence: reportFixture("0")}))
	var wg sync.WaitGroup
	done := make(chan struct{})
	bad := make(chan string, 1)
	wg.Go(func() {
		defer close(done)
		for i := 1; i <= 400; i++ {
			value := fmt.Sprint(i)
			if err := s.(AtomicResultCommitter).CommitResultAndProvenance("a", &MovieResult{Movie: &models.Movie{Title: value}}, uint64(i), &ProvenanceData{OfflineEvidence: reportFixture(value)}); err != nil {
				select {
				case bad <- err.Error():
				default:
				}
				return
			}
		}
	})
	wg.Go(func() {
		for {
			snap := s.SnapshotData()
			if snap.Results["a"].Movie.Title != snap.Provenance["a"].OfflineEvidence.SnapshotDigest {
				select {
				case bad <- "mixed snapshot":
				default:
				}
				return
			}
			select {
			case <-done:
				return
			default:
			}
		}
	})
	wg.Wait()
	select {
	case problem := <-bad:
		t.Fatal(problem)
	default:
	}
}

func TestEvidenceOnlyReplacementDoesNotChangeLegacyAttribution(t *testing.T) {
	current := &ProvenanceData{FieldSources: map[string]string{"title": "old"}, ActressSources: map[string]string{"actor": "old"}, ScraperResults: []*models.ScraperResult{{Source: "old"}}, OfflineEvidence: reportFixture("A")}
	incoming := &ProvenanceData{OfflineEvidence: reportFixture("B")}
	got := ScrapeReplacementProvenance(current, incoming)
	require.Equal(t, current.FieldSources, got.FieldSources)
	require.Equal(t, current.ActressSources, got.ActressSources)
	require.Equal(t, current.ScraperResults, got.ScraperResults)
	require.Equal(t, "B", got.OfflineEvidence.SnapshotDigest)
	got.FieldSources["title"] = "mutated"
	got.ScraperResults[0].Source = "mutated"
	got.OfflineEvidence.Observations[0].Aliases[0] = "mutated"
	require.Equal(t, "old", current.FieldSources["title"])
	require.Equal(t, "old", current.ScraperResults[0].Source)
	require.Equal(t, "Whole alias", incoming.OfflineEvidence.Observations[0].Aliases[0])
}

func TestEvidenceRestoreOwnsHistoricalReports(t *testing.T) {
	decoded := map[string]*ProvenanceData{"file": {OfflineEvidence: reportFixture("historical")}}
	store := NewFromSnapshot(1, []string{"file"}, map[string]*MovieResult{"file": {ResultID: "stable", Revision: 9}}, decoded, nil, nil, 1, 0, 100)
	decoded["file"].OfflineEvidence.SnapshotDigest = "latest"
	decoded["file"].OfflineEvidence.Observations[0].Aliases[0] = "changed"
	delete(decoded, "file")
	got := store.GetProvenance("file")
	require.Equal(t, "historical", got.OfflineEvidence.SnapshotDigest)
	require.Equal(t, "Whole alias", got.OfflineEvidence.Observations[0].Aliases[0])
	r, err := store.GetMovieResult("file")
	require.NoError(t, err)
	require.Equal(t, uint64(9), r.Revision)
}

func TestEvidenceNilCandidateCannotPublish(t *testing.T) {
	store := New(0, nil)
	require.ErrorContains(t, store.(AtomicResultCommitter).CommitResultAndProvenance("missing", nil, 0, &ProvenanceData{OfflineEvidence: reportFixture("A")}), "nil result")
	require.Empty(t, store.SnapshotData().Results)
	require.Empty(t, store.SnapshotData().Provenance)
}
