package worker

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

const skippedFencePath = "/source/IPX-777.mp4"

type skippedFenceFixture struct {
	store                      resultstore.Store
	movie                      *models.Movie
	afc                        *ApplyFileContext
	inputs                     applyPhaseInputs
	cfg                        ApplyPhaseConfig
	skipped, failed, organized int
	failureMessage             string
}

func newSkippedFenceFixture(t *testing.T) *skippedFenceFixture {
	t.Helper()
	movie := &models.Movie{ID: "IPX-777", Title: "Reviewed title", Poster: models.PosterState{PosterURL: "https://same.example/p.jpg"}}
	store := resultstore.New(1, []string{skippedFencePath})
	store.UpdateFileResult(skippedFencePath, &resultstore.MovieResult{
		ResultID:      "res-378",
		FileMatchInfo: models.FileMatchInfo{Path: skippedFencePath, MovieID: movie.ID},
		Movie:         movie, Status: models.JobStatusFailed,
		Error: "prior collision", ErrorCode: "VIDEO_COLLISION_STALE",
		VideoCollision: &videocollision.ConflictError{Code: "VIDEO_COLLISION_STALE"},
	})
	store.SetProvenance(skippedFencePath, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "user"}})
	before, err := store.GetMovieResult(skippedFencePath)
	require.NoError(t, err)
	f := &skippedFenceFixture{store: store, movie: movie, inputs: makeApplyInputs(nil)}
	f.inputs.Updater = store
	f.inputs.HistoryRepo = mocks.NewMockHistoryRepositoryInterface(t)
	f.afc = &ApplyFileContext{FilePath: skippedFencePath, Movie: movie, MovieResult: before, Match: before.FileMatchInfo, reviewBaseline: movie.Clone()}
	f.cfg = ApplyPhaseConfig{
		OnFileSkipped:   func(string) { f.skipped++ },
		OnFileOrganized: func(string) { f.organized++ },
		OnFileFailed:    func(_ string, message string) { f.failed++; f.failureMessage = message },
	}
	return f
}

func (f *skippedFenceFixture) run(t *testing.T) applyFileOutcome {
	t.Helper()
	return interpretApplyResult(skippedFencePath, f.movie, time.Now(), time.Minute, f.inputs, f.cfg, t.Context(), f.afc, &workflow.ApplyResult{Movie: f.movie, VideoOutcome: "skipped"}, nil)
}

func (f *skippedFenceFixture) assertFenced(t *testing.T, outcome applyFileOutcome, before map[string]*resultstore.MovieResult, provenance *resultstore.ProvenanceData) {
	t.Helper()
	require.True(t, outcome.Failed)
	require.False(t, outcome.Success)
	require.False(t, outcome.Cancelled)
	require.Contains(t, outcome.ErrorMsg, "persist skipped Apply receipt: ")
	if f.cfg.OnFileFailed != nil {
		require.Equal(t, 1, f.failed)
		require.Equal(t, outcome.ErrorMsg, f.failureMessage)
	} else {
		require.Zero(t, f.failed)
	}
	require.Zero(t, f.skipped)
	require.Zero(t, f.organized)
	require.Equal(t, before, f.store.CloneResults(), "fencing must preserve result identity, revision, and all receipt fields")
	require.Equal(t, provenance, f.store.GetProvenance(skippedFencePath))
	require.Empty(t, f.inputs.Broadcaster.(*stubBroadcaster).events)
}

type skippedFenceUpdater struct {
	resultstore.Store
	atomicCalls int
	beforeWrite func()
}

func (u *skippedFenceUpdater) AtomicUpdateFileResultWithProvenance(path string, update func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	u.atomicCalls++
	if u.beforeWrite != nil {
		u.beforeWrite()
	}
	return u.Store.AtomicUpdateFileResultWithProvenance(path, update)
}

func TestInterpretSkippedVideoPreservesUnresolvedPromoteWitnessRevision(t *testing.T) {
	for _, failureCallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-failure-callback", true: "failure-callback"}[failureCallback], func(t *testing.T) {
			f := newSkippedFenceFixture(t)
			before := f.store.CloneResults()
			provenance := f.store.GetProvenance(skippedFencePath)
			fs, dir := witnessFixture(t)
			witness := promoteWitness{PosterID: f.movie.ID, URL: f.movie.Poster.PosterURL, ResultID: before[skippedFencePath].ResultID, PrevRevision: before[skippedFencePath].Revision, OldSHA: map[string]string{"crop": shaContentHex([]byte("old-bytes"))}}
			payload, err := json.Marshal(witness)
			require.NoError(t, err)
			require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-"+f.movie.ID+".json"), payload, 0o644))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, f.movie.ID+".jpg"), []byte("uncommitted-refresh"), 0o644))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, f.movie.ID+".jpg.bak"), []byte("old-bytes"), 0o644))
			editor := newEditorForStore(f.store)
			editor.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-W1"})
			require.True(t, editor.hasUnresolvedPromoteWitness(f.movie.ID))
			f.inputs.PromoteWitnessFn = editor.hasUnresolvedPromoteWitness
			f.inputs.EditLockFn = func(ids ...string) func() { return editor.lockRegistry().AcquireMany(ids) }
			updater := &skippedFenceUpdater{Store: f.store}
			f.inputs.Updater = updater
			if !failureCallback {
				f.cfg.OnFileFailed = nil
			}
			outcome := f.run(t)
			f.assertFenced(t, outcome, before, provenance)
			require.Contains(t, outcome.ErrorMsg, "promote witness for IPX-777 unresolved")
			require.Zero(t, updater.atomicCalls)
			require.True(t, editor.hasUnresolvedPromoteWitness(f.movie.ID))

			payload, err = json.Marshal(f.store.CloneResults())
			require.NoError(t, err)
			repo := mocks.NewMockJobRepositoryInterface(t)
			repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: string(payload)}, nil)
			cleaner := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
			n, err := cleaner.ReconcileRekeyWitnesses(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, n)
			content, err := afero.ReadFile(fs, filepath.Join(dir, f.movie.ID+".jpg"))
			require.NoError(t, err)
			require.Equal(t, "old-bytes", string(content), "skipped failure must not make startup discard the same-URL refresh backup")
			require.False(t, editor.hasUnresolvedPromoteWitness(f.movie.ID))
		})
	}
}

func TestInterpretSkippedVideoFencesRekeyedIdentity(t *testing.T) {
	for _, callbackOnly := range []bool{false, true} {
		for _, missingMovie := range []bool{false, true} {
			name := "settled"
			if callbackOnly {
				name = "callback-only"
			}
			if missingMovie {
				name += "/match-fallback"
			} else {
				name += "/canonical"
			}
			t.Run(name, func(t *testing.T) {
				f := newSkippedFenceFixture(t)
				rekeyed := f.store.CloneResults()[skippedFencePath]
				rekeyed.Movie.ID = "NEW-378"
				if missingMovie {
					rekeyed.Movie = nil
					rekeyed.FileMatchInfo.MovieID = "NEW-378"
				}
				f.store.UpdateFileResult(skippedFencePath, rekeyed)
				before, provenance := f.store.CloneResults(), f.store.GetProvenance(skippedFencePath)
				updater := &skippedFenceUpdater{Store: f.store}
				invisible := &callbackOnlyUpdater{inner: f.store}
				f.inputs.Updater = updater
				if callbackOnly {
					f.inputs.Updater = invisible
				}
				outcome := f.run(t)
				f.assertFenced(t, outcome, before, provenance)
				require.Contains(t, outcome.ErrorMsg, "result identity changed during Apply")
				require.Zero(t, updater.atomicCalls)
				if callbackOnly {
					require.Equal(t, 1, invisible.calls)
				}
			})
		}
	}
}

func TestInterpretSkippedVideoCallbackFencesConcurrentRekey(t *testing.T) {
	f := newSkippedFenceFixture(t)
	entered, rekeyed := make(chan struct{}), make(chan struct{})
	var before map[string]*resultstore.MovieResult
	var provenance *resultstore.ProvenanceData
	go func() {
		<-entered
		current := f.store.CloneResults()[skippedFencePath]
		current.Movie.ID, current.Movie.Title = "NEW-378", "Concurrent review"
		f.store.UpdateFileResult(skippedFencePath, current)
		f.store.SetProvenance(skippedFencePath, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "concurrent-user"}})
		before, provenance = f.store.CloneResults(), f.store.GetProvenance(skippedFencePath)
		close(rekeyed)
	}()
	updater := &skippedFenceUpdater{Store: f.store, beforeWrite: func() {
		close(entered)
		select {
		case <-rekeyed:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent rekey did not finish")
		}
	}}
	f.inputs.Updater = updater
	outcome := f.run(t)
	f.assertFenced(t, outcome, before, provenance)
	require.Equal(t, 1, updater.atomicCalls)
	require.Contains(t, outcome.ErrorMsg, "result identity changed during Apply")
}

func TestInterpretSkippedVideoSerializesFamilyWitnessCheckAndWrite(t *testing.T) {
	for _, heldKey := range []string{"Z", "_"} {
		t.Run(heldKey, func(t *testing.T) {
			f := newSkippedFenceFixture(t)
			f.movie.ID = "_"
			current := f.store.CloneResults()[skippedFencePath]
			current.Movie.ID, current.FileMatchInfo.MovieID = "_", "z"
			f.store.UpdateFileResult(skippedFencePath, current)
			f.afc.MovieResult = current.Clone()
			f.afc.Match.MovieID = "_"
			before, provenance := f.store.CloneResults(), f.store.GetProvenance(skippedFencePath)
			reg := newKeyedMutexRegistry()
			releaseEdit := reg.Acquire(heldKey)
			defer func() {
				if releaseEdit != nil {
					releaseEdit()
				}
			}()
			requested := make(chan []string, 1)
			probed := make(chan struct{}, 1)
			f.inputs.EditLockFn = func(ids ...string) func() {
				requested <- append([]string(nil), ids...)
				return reg.AcquireMany(ids)
			}
			witnessPending := false
			f.inputs.PromoteWitnessFn = func(string) bool { probed <- struct{}{}; return witnessPending }
			updater := &skippedFenceUpdater{Store: f.store}
			f.inputs.Updater = updater
			done := make(chan applyFileOutcome, 1)
			go func() { done <- f.run(t) }()
			select {
			case ids := <-requested:
				require.Equal(t, []string{"z", "_"}, ids)
			case <-time.After(2 * time.Second):
				t.Fatal("skip did not request the alias/canonical family acquisition")
			}
			select {
			case <-probed:
				t.Fatal("witness checked outside the family lock")
			case <-done:
				t.Fatal("skip wrote through a held family lock")
			case <-time.After(25 * time.Millisecond):
			}
			witnessPending = true
			releaseEdit()
			releaseEdit = nil
			select {
			case outcome := <-done:
				f.assertFenced(t, outcome, before, provenance)
			case <-time.After(2 * time.Second):
				t.Fatal("skip deadlocked after the review edit released its key")
			}
			require.Zero(t, updater.atomicCalls)
			unlocked := make(chan struct{})
			go func() { reg.AcquireMany([]string{"_", "z"})(); close(unlocked) }()
			select {
			case <-unlocked:
			case <-time.After(2 * time.Second):
				t.Fatal("skip leaked its family lock")
			}
		})
	}
}

func TestInterpretSkippedVideoHoldsFamilyLockThroughReceiptPublication(t *testing.T) {
	f := newSkippedFenceFixture(t)
	reg := newKeyedMutexRegistry()
	f.inputs.EditLockFn = func(ids ...string) func() { return reg.AcquireMany(ids) }
	f.inputs.PromoteWitnessFn = func(string) bool { return false }
	entered, publish := make(chan struct{}), make(chan struct{})
	updater := &skippedFenceUpdater{Store: f.store, beforeWrite: func() { close(entered); <-publish }}
	f.inputs.Updater = updater
	history := mocks.NewMockHistoryRepositoryInterface(t)
	history.EXPECT().Create(mock.Anything, mock.MatchedBy(func(h *models.History) bool { return h.Status == models.HistoryStatusSkipped })).Return(nil).Once()
	f.inputs.HistoryRepo = history
	done := make(chan applyFileOutcome, 1)
	go func() { done <- f.run(t) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("skip did not reach receipt publication")
	}
	editRequested, editDone := make(chan struct{}), make(chan struct{})
	go func() {
		close(editRequested)
		reg.AcquireMany([]string{f.movie.ID})()
		close(editDone)
	}()
	<-editRequested
	select {
	case <-editDone:
		t.Fatal("review edit entered between witness check and receipt publication")
	case <-time.After(25 * time.Millisecond):
	}
	close(publish)
	select {
	case outcome := <-done:
		require.True(t, outcome.Success)
		require.False(t, outcome.Failed)
	case <-time.After(2 * time.Second):
		t.Fatal("receipt publication did not finish")
	}
	select {
	case <-editDone:
	case <-time.After(2 * time.Second):
		t.Fatal("review edit could not resume after receipt publication")
	}
	require.Equal(t, 1, f.skipped)
	require.Zero(t, f.failed)
	require.Equal(t, 1, updater.atomicCalls)
}
