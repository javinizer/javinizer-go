package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func workerEvidence(digest string) *scrapeevidence.Report {
	return &scrapeevidence.Report{Outcome: scrapeevidence.Available, SnapshotDigest: digest, AdmittedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Observations: []scrapeevidence.Observation{{Source: "r18dev:derived_actress", SourceID: "7", Outcome: scrapeevidence.Available, JapaneseName: "Source evidence", Aliases: []string{"Whole alias"}}, {Source: "r18dev:derived_actress", SourceID: "007", Outcome: scrapeevidence.Unmatched}}}
}

type evidencePublicationSpy struct {
	resultstore.ResultUpdater
	atomicCalls, updateCalls, provenanceCalls int
	fail                                      bool
}

func (s *evidencePublicationSpy) UpdateFileResult(path string, r *resultstore.MovieResult) {
	s.updateCalls++
	s.ResultUpdater.UpdateFileResult(path, r)
}
func (s *evidencePublicationSpy) SetProvenance(path string, p *resultstore.ProvenanceData) {
	s.provenanceCalls++
	s.ResultUpdater.SetProvenance(path, p)
}
func (s *evidencePublicationSpy) AtomicUpdateFileResultWithProvenance(path string, fn func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	s.atomicCalls++
	if s.fail {
		return errors.New("atomic unavailable")
	}
	return s.ResultUpdater.AtomicUpdateFileResultWithProvenance(path, fn)
}

func TestEvidenceInitialPublicationAtomicOwnedAndDefaultOffGate(t *testing.T) {
	for _, mode := range []string{"on", "off", "atomic failure"} {
		t.Run(mode, func(t *testing.T) {
			store := resultstore.New(1, []string{"file"})
			store.UpdateFileResult("file", &resultstore.MovieResult{ResultID: "stable", Status: models.JobStatusRunning})
			before, _ := store.GetMovieResult("file")
			spy := &evidencePublicationSpy{ResultUpdater: store, fail: mode == "atomic failure"}
			candidate := &resultstore.MovieResult{ResultID: "candidate", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ID", Title: "canonical"}}
			prov := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "r18dev"}}
			if mode != "off" {
				prov.OfflineEvidence = workerEvidence("A")
			}
			publishInitialScrapeResult(spy, "file", candidate, prov)
			got, _ := store.GetMovieResult("file")
			require.Equal(t, "stable", got.ResultID)
			require.Equal(t, before.Revision+1, got.Revision)
			if mode == "on" {
				require.Equal(t, 1, spy.atomicCalls)
				require.Zero(t, spy.updateCalls)
				require.Zero(t, spy.provenanceCalls)
				candidate.Movie.Title = "mutated"
				prov.OfflineEvidence.SnapshotDigest = "mutated"
				got, _ = store.GetMovieResult("file")
				require.Equal(t, "canonical", got.Movie.Title)
				require.Equal(t, "A", store.GetProvenance("file").OfflineEvidence.SnapshotDigest)
			} else {
				require.Nil(t, store.GetProvenance("file").OfflineEvidence)
				require.Equal(t, 1, spy.updateCalls)
				require.Equal(t, 1, spy.provenanceCalls)
				if mode == "off" {
					require.Zero(t, spy.atomicCalls)
				}
			}
		})
	}
}

func TestEvidenceInitialBatchMultipartDoesNotMutateMovie(t *testing.T) {
	result := makeScrapeResult("TEST-001")
	result.Movie.Title = "Canonical"
	result.Movie.Actresses = []models.Actress{{JapaneseName: "Canonical actor"}}
	result.OfflineEvidence = workerEvidence("A")
	original := result.Movie.Clone()
	wf := &stubWorkflow{scrapeResult: result}
	inputs := makeInputs(wf)
	store := resultstore.New(2, []string{"part1.mp4", "part2.mp4"})
	inputs.Updater = store
	NewScrapePhase().Run(context.Background(), inputs, []string{"part1.mp4", "part2.mp4"}, ScrapePhaseConfig{})
	for _, path := range []string{"part1.mp4", "part2.mp4"} {
		got, _ := store.GetMovieResult(path)
		require.Equal(t, models.JobStatusCompleted, got.Status)
		require.Equal(t, original.Actresses, got.Movie.Actresses)
		require.Equal(t, "Canonical", got.Movie.Title)
		require.Equal(t, "A", store.GetProvenance(path).OfflineEvidence.SnapshotDigest)
	}
	require.Equal(t, original, result.Movie)
	result.OfflineEvidence.Observations[0].Aliases[0] = "mutated"
	require.Equal(t, "Whole alias", store.GetProvenance("part1.mp4").OfflineEvidence.Observations[0].Aliases[0])
}

func TestEvidenceInitialFailureAndCancellationNeverSucceed(t *testing.T) {
	for _, mode := range []string{"scrape failure", "cancel", "persist failure"} {
		t.Run(mode, func(t *testing.T) {
			result := makeScrapeResult("TEST-001")
			result.OfflineEvidence = workerEvidence("A")
			wf := &stubWorkflow{scrapeResult: result}
			inputs := makeInputs(wf)
			store := resultstore.New(1, []string{"file.mp4"})
			inputs.Updater = store
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "scrape failure" {
				wf.scrapeErr = errors.New("scrape failed")
			}
			if mode == "cancel" {
				cancel()
			}
			if mode == "persist failure" {
				inputs.MovieRepo = failingPersistRepo{}
			}
			NewScrapePhase().Run(ctx, inputs, []string{"file.mp4"}, ScrapePhaseConfig{})
			got, err := store.GetMovieResult("file.mp4")
			if mode == "cancel" && err != nil {
				return
			}
			require.NoError(t, err)
			require.NotEqual(t, models.JobStatusCompleted, got.Status)
			if prov := store.GetProvenance("file.mp4"); prov != nil {
				require.Nil(t, prov.OfflineEvidence)
			}
		})
	}
}

type evidenceInterruptWorkflow struct {
	stubRescrapeWorkflow
	cancel     context.CancelFunc
	panicValue bool
}

func (w *evidenceInterruptWorkflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	if w.panicValue {
		panic("fixture panic")
	}
	if w.cancel != nil {
		w.cancel()
	}
	return w.stubRescrapeWorkflow.Scrape(ctx, cmd)
}

func TestEvidenceRescrapeFailureCancelPanicPreserveCommittedReport(t *testing.T) {
	for _, mode := range []string{"error", "cancel", "panic", "success off", "success on"} {
		t.Run(mode, func(t *testing.T) {
			store := resultstore.New(1, []string{"file.mp4"})
			store.UpdateFileResult("file.mp4", &resultstore.MovieResult{ResultID: "stable", Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "file.mp4", MovieID: "ID"}, Movie: &models.Movie{ID: "ID", Title: "old"}})
			store.SetProvenance("file.mp4", &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "old"}, OfflineEvidence: workerEvidence("A")})
			before := store.SnapshotData()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wf := &evidenceInterruptWorkflow{stubRescrapeWorkflow: stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "ID", Title: "new"}}}}
			switch mode {
			case "error":
				wf.scrapeErr = errors.New("fixture error")
			case "cancel":
				wf.cancel = cancel
			case "panic":
				wf.panicValue = true
			case "success on":
				wf.scrapeResult.OfflineEvidence = workerEvidence("B")
			}
			wrapped := &familyKeyedResultMap{ResultMapAccessor: store, updater: store, registry: newKeyedMutexRegistry()}
			inputs := rescrapePhaseInputs{WF: wf, ResultMap: wrapped, Finder: store, JobID: models.NewJobID()}
			outcome, err := NewRescrapePhase().Rescrape(ctx, inputs, RescrapeCmd{MovieID: "ID", FilePath: "file.mp4"})
			if mode == "success on" || mode == "success off" {
				require.NoError(t, err)
				require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
				got, _ := store.GetMovieResult("file.mp4")
				require.Equal(t, "stable", got.ResultID)
				if mode == "success off" {
					require.Nil(t, store.GetProvenance("file.mp4").OfflineEvidence)
					require.Equal(t, "old", store.GetProvenance("file.mp4").FieldSources["title"])
					require.Nil(t, outcome.OfflineEvidence)
				} else {
					require.Equal(t, "B", outcome.OfflineEvidence.SnapshotDigest)
					outcome.OfflineEvidence.SnapshotDigest = "response mutation"
					require.Equal(t, "B", store.GetProvenance("file.mp4").OfflineEvidence.SnapshotDigest)
				}
			} else {
				require.Error(t, err)
				require.Equal(t, before.Provenance, store.SnapshotData().Provenance)
				got, _ := store.GetMovieResult("file.mp4")
				require.Equal(t, "old", got.Movie.Title)
			}
		})
	}
}

func TestEvidenceRescrapeLegacyStoreFailsClosed(t *testing.T) {
	rm := newStubResultMap()
	_, err := commitResultWithProvenanceAndRevisions(rm, "file", &resultstore.MovieResult{}, 0, &resultstore.ProvenanceData{OfflineEvidence: workerEvidence("A")})
	require.ErrorContains(t, err, "atomic publication")
	require.Empty(t, rm.getCommitted())
	_, err = commitResultWithProvenanceAndRevisions(rm, "file", &resultstore.MovieResult{}, 0, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"file"}, rm.getCommitted())
}

func TestEvidenceApplyWritebackUsesLiveWholeReportIncludingClear(t *testing.T) {
	frozen := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "frozen"}, OfflineEvidence: workerEvidence("A")}
	for _, live := range []*resultstore.ProvenanceData{nil, {}, {OfflineEvidence: workerEvidence("B")}} {
		got := mergeWriteBackProvenance(frozen, live)
		require.Equal(t, "frozen", got.FieldSources["title"])
		if live == nil || live.OfflineEvidence == nil {
			require.Nil(t, got.OfflineEvidence)
		} else {
			require.Equal(t, "B", got.OfflineEvidence.SnapshotDigest)
			got.OfflineEvidence.SnapshotDigest = "mutated"
			require.Equal(t, "B", live.OfflineEvidence.SnapshotDigest)
		}
	}
	require.Equal(t, "A", frozen.OfflineEvidence.SnapshotDigest)
}

type evidencePosterCancel struct{ cancel context.CancelFunc }

func (p evidencePosterCancel) GeneratePoster(ctx context.Context, _ string, _ *models.Movie) error {
	p.cancel()
	return ctx.Err()
}
func TestEvidenceInitialPostPosterCancellationPublishesNeitherSuccessNorReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := makeScrapeResult("TEST-001")
	result.OfflineEvidence = workerEvidence("A")
	inputs := makeInputs(&stubWorkflow{scrapeResult: result})
	store := resultstore.New(1, []string{"file.mp4"})
	inputs.Updater = store
	inputs.PosterGen = evidencePosterCancel{cancel}
	NewScrapePhase().Run(ctx, inputs, []string{"file.mp4"}, ScrapePhaseConfig{})
	got, err := store.GetMovieResult("file.mp4")
	require.NoError(t, err)
	require.Equal(t, models.JobStatusCancelled, got.Status)
	require.Nil(t, store.GetProvenance("file.mp4"))
	for _, event := range inputs.Broadcaster.(*stubBroadcaster).events {
		require.NotEqual(t, StepComplete, event.Step)
	}
}

type evidencePersistRepo struct {
	failingPersistRepo
	write func(context.Context, *models.Movie) (*models.Movie, error)
}

func (r evidencePersistRepo) UpsertWithTranslations(ctx context.Context, m *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	return r.write(ctx, m)
}
func TestEvidenceInitialDeferredPersistenceBoundary(t *testing.T) {
	for _, mode := range []string{"success", "panic", "cancel error", "cancel success", "atomic unavailable"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := makeScrapeResult("TEST-001")
			result.OfflineEvidence = workerEvidence("A")
			inputs := makeInputs(&stubWorkflow{scrapeResult: result})
			store := resultstore.New(1, []string{"file.mp4"})
			inputs.Updater = store
			if mode == "atomic unavailable" {
				inputs.Updater = &evidencePublicationSpy{ResultUpdater: store, fail: true}
			}
			writes := 0
			var initialID string
			inputs.MovieRepo = evidencePersistRepo{write: func(_ context.Context, m *models.Movie) (*models.Movie, error) {
				writes++
				before := store.SnapshotData()
				initialID = before.Results["file.mp4"].ResultID
				require.NotEmpty(t, initialID)
				if p := before.Provenance["file.mp4"]; p != nil {
					require.Nil(t, p.OfflineEvidence, "no report before DB acceptance")
				}
				switch mode {
				case "panic":
					panic("DB fixture panic")
				case "cancel error":
					cancel()
					return nil, context.Canceled
				case "cancel success":
					cancel()
				}
				return m, nil
			}}
			NewScrapePhase().Run(ctx, inputs, []string{"file.mp4"}, ScrapePhaseConfig{})
			require.Equal(t, 1, writes, "evidence must not add DB writes")
			got, err := store.GetMovieResult("file.mp4")
			require.NoError(t, err)
			require.Equal(t, initialID, got.ResultID)
			if mode == "success" {
				require.True(t, got.Persisted)
				require.Equal(t, models.JobStatusCompleted, got.Status)
				require.Equal(t, "A", store.GetProvenance("file.mp4").OfflineEvidence.SnapshotDigest)
			} else {
				if p := store.GetProvenance("file.mp4"); p != nil {
					require.Nil(t, p.OfflineEvidence)
				}
				if mode != "atomic unavailable" {
					require.NotEqual(t, models.JobStatusCompleted, got.Status)
				}
			}
		})
	}
}

type evidenceLegacyStore struct{ resultstore.Store }

func TestEvidenceLegacyStoreCannotClearHistoricalReportNonAtomically(t *testing.T) {
	store := resultstore.New(1, []string{"file"})
	store.UpdateFileResult("file", &resultstore.MovieResult{Movie: &models.Movie{Title: "committed"}})
	store.SetProvenance("file", &resultstore.ProvenanceData{OfflineEvidence: workerEvidence("A")})
	before := store.SnapshotData()
	legacy := evidenceLegacyStore{store}
	_, err := commitResultWithProvenanceAndRevisions(legacy, "file", &resultstore.MovieResult{}, before.Results["file"].Revision, nil)
	require.ErrorContains(t, err, "atomic publication")
	require.Equal(t, before, store.SnapshotData())
}

func TestEvidenceFamilyCommitCASFailurePublishesNeitherHalf(t *testing.T) {
	store := resultstore.New(1, []string{"file"})
	store.UpdateFileResult("file", &resultstore.MovieResult{ResultID: "stable", Movie: &models.Movie{ID: "ID", Title: "committed"}})
	store.SetProvenance("file", &resultstore.ProvenanceData{OfflineEvidence: workerEvidence("A")})
	before := store.SnapshotData()
	wrapped := &familyKeyedResultMap{ResultMapAccessor: store, updater: store, registry: newKeyedMutexRegistry()}
	outcome, err := NewRescrapePhase().CompleteRescrape(rescrapePhaseInputs{ResultMap: wrapped}, "file", &resultstore.MovieResult{Movie: &models.Movie{ID: "ID", Title: "stale"}}, 0, "ID", "ID", &resultstore.ProvenanceData{OfflineEvidence: workerEvidence("B")})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusConflict, outcome.Status)
	require.Equal(t, before, store.SnapshotData())
}
