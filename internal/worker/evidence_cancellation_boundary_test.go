package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// The pool has two writers. Holding both after all three successful scrapes
// ensures one outcome is still queued when cancellation reaches persistence.
func TestEvidenceQueuedPersistenceCancellationDoesNotPublishReport(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	files := []string{"first.mp4", "second.mp4", "queued.mp4"}
	result := makeScrapeResult("TEST-001")
	result.OfflineEvidence = workerEvidence("queued")
	inputs := makeInputs(&stubWorkflow{scrapeResult: result})
	store := resultstore.New(len(files), files)
	inputs.Updater = store
	entered, resume := make(chan struct{}, 2), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	defer release()
	var writes atomic.Int32
	inputs.MovieRepo = evidencePersistRepo{write: func(ctx context.Context, _ *models.Movie) (*models.Movie, error) {
		writes.Add(1)
		entered <- struct{}{}
		<-resume
		return nil, ctx.Err()
	}}
	done := make(chan struct{})
	defer func() {
		cancel()
		release()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("scrape phase did not join during cleanup")
		}
	}()
	go func() { NewScrapePhase().Run(ctx, inputs, files, ScrapePhaseConfig{Force: true}); close(done) }()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("both persistence workers did not enter")
		}
	}
	before := store.SnapshotData()
	require.Len(t, before.Results, 3, "every scrape must complete before persistence starts")
	for _, file := range files {
		require.Equal(t, models.JobStatusCompleted, before.Results[file].Status)
		require.False(t, before.Results[file].Persisted)
		require.NotEmpty(t, before.Results[file].ResultID)
		if prov := before.Provenance[file]; prov != nil {
			require.Nil(t, prov.OfflineEvidence)
		}
	}
	cancel()
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled persistence pool did not join")
	}
	require.Equal(t, int32(2), writes.Load(), "queued outcome must never reach the repository")
	for _, file := range files {
		got, err := store.GetMovieResult(file)
		require.NoError(t, err)
		require.Equal(t, before.Results[file].ResultID, got.ResultID)
		require.Equal(t, models.JobStatusCancelled, got.Status)
		require.False(t, got.Persisted)
		require.Nil(t, got.Movie, "cancelled evidence must not become review/save input")
		if prov := store.GetProvenance(file); prov != nil {
			require.Nil(t, prov.OfflineEvidence)
		}
	}
	lifecycle := inputs.Lifecycle.(*stubLifecycle)
	require.True(t, lifecycle.cancelled)
	require.False(t, lifecycle.completed)
	require.False(t, lifecycle.failed)
	require.True(t, inputs.Broadcaster.(*stubBroadcaster).isClosed())
}

type evidenceCancelAtPublication struct {
	resultstore.ResultUpdater
	cancel          context.CancelFunc
	accepted        *atomic.Bool
	calls           atomic.Int32
	afterAcceptance atomic.Bool
}

func (u *evidenceCancelAtPublication) AtomicUpdateFileResultWithProvenance(path string, fn func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	if u.calls.Add(1) == 1 {
		u.afterAcceptance.Store(u.accepted.Load())
		u.cancel()
	}
	return u.ResultUpdater.AtomicUpdateFileResultWithProvenance(path, fn)
}

func TestEvidenceCancellationAtAtomicPublicationAfterDatabaseAcceptance(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := makeScrapeResult("TEST-001")
	result.OfflineEvidence = workerEvidence("accepted")
	inputs := makeInputs(&stubWorkflow{scrapeResult: result})
	store := resultstore.New(1, []string{"file.mp4"})
	var accepted atomic.Bool
	updater := &evidenceCancelAtPublication{ResultUpdater: store, cancel: cancel, accepted: &accepted}
	inputs.Updater = updater
	var writes atomic.Int32
	var initial *resultstore.MovieResult
	inputs.MovieRepo = evidencePersistRepo{write: func(_ context.Context, movie *models.Movie) (*models.Movie, error) {
		writes.Add(1)
		initial, _ = store.GetMovieResult("file.mp4")
		saved := movie.Clone()
		saved.Title = "database-accepted candidate"
		accepted.Store(true)
		return saved, nil
	}}
	NewScrapePhase().Run(ctx, inputs, []string{"file.mp4"}, ScrapePhaseConfig{Force: true})
	require.Equal(t, int32(1), writes.Load())
	require.Equal(t, int32(2), updater.calls.Load(), "rejected publication followed by atomic cancellation")
	require.True(t, updater.afterAcceptance.Load(), "cancel only at the deferred publisher, not the initial scrape publication")
	require.NotNil(t, initial)
	got, err := store.GetMovieResult("file.mp4")
	require.NoError(t, err)
	require.Equal(t, initial.ResultID, got.ResultID)
	require.Equal(t, initial.Revision+1, got.Revision, "only cancellation, not half a publication, may advance the revision")
	require.Equal(t, models.JobStatusCancelled, got.Status)
	require.False(t, got.Persisted)
	require.Nil(t, got.Movie)
	if prov := store.GetProvenance("file.mp4"); prov != nil {
		require.Nil(t, prov.OfflineEvidence)
	}
	lifecycle := inputs.Lifecycle.(*stubLifecycle)
	require.True(t, lifecycle.cancelled)
	require.False(t, lifecycle.completed)
}
