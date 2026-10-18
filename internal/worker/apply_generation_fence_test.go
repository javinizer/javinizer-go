package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type generationFenceApplyPhase func(context.Context, applyPhaseInputs, ApplyPhaseConfig)

func (f generationFenceApplyPhase) Run(ctx context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
	f(ctx, inputs, cfg)
}

func TestStartApplyGenerationFence_StaleDoesNotMutate(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusCompleted, models.JobStatusRunning} {
		for _, stale := range []uint64{0, 18, 20} {
			t.Run(string(status)+"/"+fmt.Sprint(stale), func(t *testing.T) {
				dir := t.TempDir()
				video, nfo := filepath.Join(dir, "source.mp4"), filepath.Join(dir, "source.nfo")
				for _, path := range []string{video, nfo} {
					require.NoError(t, os.WriteFile(path, []byte("untouched sentinel"), 0600))
				}
				job := newBatchJob([]string{video})
				job.Controller().SetWorkflow(&stubApplyWorkflow{})
				job.lifecycle.Status = status
				job.lifecycle.applyGeneration = 19
				job.cfg = jobConfig{destination: "original", tempDir: "original-temp", operationMode: operationmode.OperationModeInPlace}
				beforeLifecycle := job.lifecycle.StatusSnapshot()
				beforeConfig, beforeStarted := job.cfg, job.StartedAt
				beforeDone, beforePhaseDone := job.lifecycle.done, job.lifecycle.phaseDone
				var phaseCalls, persists atomic.Int32
				job.deps.PersistFn = func() error { persists.Add(1); return nil }
				job.applyPhase = generationFenceApplyPhase(func(_ context.Context, inputs applyPhaseInputs, _ ApplyPhaseConfig) {
					phaseCalls.Add(1)
					_ = os.WriteFile(video, []byte("unwanted video write"), 0600)
					_ = os.WriteFile(nfo, []byte("unwanted NFO write"), 0600)
					inputs.Lifecycle.MarkCompleted()
				})
				expected, receipt, update := stale, uint64(0), true
				err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
					ExpectedApplyGeneration: &expected, ApplyGenerationRef: &receipt,
					Destination: "unwanted", TempDir: "unwanted-temp", Update: &update,
					OperationModeOverride: operationmode.OperationModeOrganize,
					GenerateNFO:           true, Download: true, RetryFilePaths: []string{video},
				})
				if err == nil {
					require.NoError(t, job.Controller().Wait())
				}
				require.ErrorIs(t, err, ErrApplyGenerationMismatch)
				assert.Equal(t, beforeLifecycle, job.lifecycle.StatusSnapshot())
				assert.Equal(t, beforeConfig, job.cfg)
				assert.Equal(t, beforeStarted, job.StartedAt)
				assert.Equal(t, beforeDone, job.lifecycle.done)
				assert.Equal(t, beforePhaseDone, job.lifecycle.phaseDone)
				assert.Nil(t, job.lifecycle.CancelFunc)
				assert.Zero(t, atomic.LoadUint64(&receipt))
				assert.Zero(t, phaseCalls.Load())
				assert.Zero(t, persists.Load())
				assert.Zero(t, job.admission.pendingPhase)
				for _, path := range []string{video, nfo} {
					contents, readErr := os.ReadFile(path)
					require.NoError(t, readErr)
					assert.Equal(t, "untouched sentinel", string(contents))
				}
			})
		}
	}
}

func TestStartApplyGenerationFence_MatchedAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		name            string
		generation      uint64
		withExpectation bool
	}{
		{"matched retry", 17, true},
		{"explicit initial zero", 0, true},
		{"legacy omitted", 17, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := newBatchJob(nil)
			job.Controller().SetWorkflow(&stubApplyWorkflow{})
			job.lifecycle.Status = models.JobStatusCompleted
			job.lifecycle.applyGeneration = tc.generation
			var calls atomic.Int32
			job.applyPhase = generationFenceApplyPhase(func(_ context.Context, inputs applyPhaseInputs, _ ApplyPhaseConfig) {
				calls.Add(1)
				inputs.Lifecycle.MarkCompleted()
			})
			var receipt uint64
			cfg := ApplyPhaseConfig{ApplyGenerationRef: &receipt, Destination: "admitted-destination"}
			if tc.withExpectation {
				cfg.ExpectedApplyGeneration = &tc.generation
			}
			require.NoError(t, job.Controller().StartApply(context.Background(), cfg))
			require.NoError(t, job.Controller().Wait())
			assert.Equal(t, tc.generation+1, atomic.LoadUint64(&receipt))
			assert.Equal(t, tc.generation+1, job.lifecycle.StatusSnapshot().ApplyGeneration)
			assert.Equal(t, int32(1), calls.Load())
			assert.Equal(t, "admitted-destination", job.cfg.destination)
		})
	}
}

func TestStartApplyGenerationFence_ConcurrentRetryHasOneWinner(t *testing.T) {
	job := newBatchJob(nil)
	job.Controller().SetWorkflow(&stubApplyWorkflow{})
	job.lifecycle.Status = models.JobStatusCompleted
	job.lifecycle.applyGeneration = 23
	expected := uint64(23)
	var calls atomic.Int32
	phaseEntered := make(chan string, 2)
	releasePhase := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(releasePhase) }) })
	job.applyPhase = generationFenceApplyPhase(func(_ context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
		calls.Add(1)
		phaseEntered <- cfg.Destination
		<-releasePhase
		inputs.Lifecycle.MarkCompleted()
	})
	type launchResult struct {
		index int
		err   error
	}
	results := make(chan launchResult, 2)
	start := make(chan struct{})
	destinations := []string{"first-destination", "second-destination"}
	receipts := [2]uint64{}
	for i := range destinations {
		go func() {
			<-start
			err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
				ExpectedApplyGeneration: &expected, ApplyGenerationRef: &receipts[i], Destination: destinations[i],
			})
			results <- launchResult{i, err}
		}()
	}
	close(start)
	winner := -1
	for range destinations {
		select {
		case result := <-results:
			if result.err == nil {
				require.Equal(t, -1, winner, "only one launch may claim the expected generation")
				winner = result.index
			} else {
				require.ErrorIs(t, result.err, ErrApplyGenerationMismatch)
				assert.Zero(t, atomic.LoadUint64(&receipts[result.index]))
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a competing retry queued behind the admitted phase")
		}
	}
	require.NotEqual(t, -1, winner)
	assert.Equal(t, uint64(24), atomic.LoadUint64(&receipts[winner]))
	select {
	case destination := <-phaseEntered:
		assert.Equal(t, destinations[winner], destination)
	case <-time.After(3 * time.Second):
		t.Fatal("winning phase never started")
	}
	job.mu.RLock()
	assert.Equal(t, destinations[winner], job.cfg.destination, "loser cannot overwrite winner's config")
	job.mu.RUnlock()
	release.Do(func() { close(releasePhase) })
	require.NoError(t, job.Controller().Wait())
	// Even after the newer apply finishes, an old receipt cannot launch again.
	require.ErrorIs(t, job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
		ExpectedApplyGeneration: &expected, Destination: "stale-after-completion",
	}), ErrApplyGenerationMismatch)
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, destinations[winner], job.cfg.destination)
	assert.Equal(t, uint64(24), job.lifecycle.StatusSnapshot().ApplyGeneration)
}

func TestStartApplyGenerationFence_ClaimChecksWhileHoldingLifecycleLock(t *testing.T) {
	job := newBatchJob(nil)
	job.lifecycle.Status = models.JobStatusCompleted
	job.lifecycle.applyGeneration = 30
	expected := uint64(30)
	job.lifecycle.mu.Lock()
	entered, result := make(chan struct{}), make(chan error, 1)
	go func() {
		close(entered)
		_, _, err := job.controller.markStarted(models.JobStatusCompleted, JobPhaseApply, func() {}, &expected)
		result <- err
	}()
	<-entered
	// A competing generation completes while this launch is waiting for the
	// claim lock. Checking only before acquiring this lock would miss it.
	job.lifecycle.applyGeneration = 31
	job.lifecycle.mu.Unlock()
	select {
	case err := <-result:
		require.ErrorIs(t, err, ErrApplyGenerationMismatch)
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not finish after lifecycle lock release")
	}
	assert.Equal(t, models.JobStatusCompleted, job.lifecycle.GetJobStatus())
	assert.Equal(t, uint64(31), job.lifecycle.StatusSnapshot().ApplyGeneration)
	assert.Nil(t, job.lifecycle.phaseDone)
}

// The generation is returned from the claim itself, including legacy callers
// that have no expected value. A later snapshot must not replace that receipt.
func TestStartApplyGenerationFence_ClaimReturnsExactGeneration(t *testing.T) {
	job := newBatchJob(nil)
	job.lifecycle.Status = models.JobStatusCompleted
	job.lifecycle.applyGeneration = 30
	job.lifecycle.mu.Lock()
	type claimResult struct {
		generation uint64
		err        error
	}
	entered, result := make(chan struct{}), make(chan claimResult, 1)
	go func() {
		close(entered)
		_, generation, err := job.controller.markStarted(models.JobStatusCompleted, JobPhaseApply, func() {})
		result <- claimResult{generation, err}
	}()
	<-entered
	// Simulate a newer completed apply before this caller acquires the claim.
	job.lifecycle.applyGeneration = 38
	job.lifecycle.mu.Unlock()
	select {
	case got := <-result:
		require.NoError(t, got.err)
		assert.Equal(t, uint64(39), got.generation)
		assert.Equal(t, got.generation, job.lifecycle.StatusSnapshot().ApplyGeneration)
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not finish after lifecycle lock release")
	}
}

func TestStartApplyGenerationFence_ReceiptWaitsForPersistence(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("fail=", fail), func(t *testing.T) {
			job := newBatchJob(nil)
			job.Controller().SetWorkflow(&stubApplyWorkflow{})
			job.lifecycle.Status = models.JobStatusCompleted
			job.lifecycle.applyGeneration = 62
			var receipt uint64
			var phaseCalls, persists atomic.Int32
			persistEntered, releasePersist := make(chan struct{}), make(chan struct{})
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(releasePersist) }) })
			job.deps.PersistFn = func() error {
				if persists.Add(1) == 1 {
					close(persistEntered)
					<-releasePersist
					if fail {
						return errors.New("entry persist refused")
					}
				}
				return nil
			}
			job.applyPhase = generationFenceApplyPhase(func(_ context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
				phaseCalls.Add(1)
				assert.Equal(t, uint64(63), atomic.LoadUint64(cfg.ApplyGenerationRef), "callbacks start with the exact request receipt")
				inputs.Lifecycle.MarkCompleted()
			})
			result := make(chan error, 1)
			go func() {
				result <- job.Controller().StartApply(context.Background(), ApplyPhaseConfig{ApplyGenerationRef: &receipt})
			}()
			select {
			case <-persistEntered:
			case <-time.After(3 * time.Second):
				t.Fatal("entry persistence never started")
			}
			assert.Zero(t, atomic.LoadUint64(&receipt), "an uncommitted admission cannot prove ownership")
			assert.Zero(t, phaseCalls.Load())
			assert.Equal(t, uint64(63), job.lifecycle.StatusSnapshot().ApplyGeneration)
			release.Do(func() { close(releasePersist) })
			var err error
			select {
			case err = <-result:
			case <-time.After(3 * time.Second):
				t.Fatal("StartApply did not return after persistence")
			}
			if fail {
				require.ErrorContains(t, err, "entry persist refused")
				assert.Zero(t, atomic.LoadUint64(&receipt))
				assert.Zero(t, phaseCalls.Load())
				assert.Equal(t, uint64(62), job.lifecycle.StatusSnapshot().ApplyGeneration, "failed claim rolls back its exact predecessor")
				require.ErrorContains(t, job.Controller().Wait(), "failed")
			} else {
				require.NoError(t, err)
				require.NoError(t, job.Controller().Wait())
				assert.Equal(t, uint64(63), atomic.LoadUint64(&receipt))
				assert.Equal(t, int32(1), phaseCalls.Load())
			}
		})
	}
}

func TestStartApplyGenerationFence_CanceledAdmissionHasNoReceipt(t *testing.T) {
	for _, kind := range []string{"before claim", "canceled context", "expired context", "queued cancel", "queued delete"} {
		t.Run(kind, func(t *testing.T) {
			job := newBatchJob(nil)
			job.Controller().SetWorkflow(&stubApplyWorkflow{})
			job.lifecycle.Status = models.JobStatusCompleted
			job.lifecycle.applyGeneration = 7
			var receipt uint64
			var phaseCalls atomic.Int32
			job.applyPhase = generationFenceApplyPhase(func(_ context.Context, inputs applyPhaseInputs, _ ApplyPhaseConfig) {
				phaseCalls.Add(1)
				inputs.Lifecycle.MarkCompleted()
			})
			ctx := context.Background()
			if kind == "before claim" {
				job.lifecycle.cancelled = true
			} else if kind == "canceled context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			} else if kind == "expired context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				defer cancel()
			}
			queued := kind == "queued cancel" || kind == "queued delete"
			if queued {
				release, err := job.admission.AdmitShared()
				require.NoError(t, err)
				defer release()
			}
			result := make(chan error, 1)
			go func() { result <- job.Controller().StartApply(ctx, ApplyPhaseConfig{ApplyGenerationRef: &receipt}) }()
			if queued {
				require.Eventually(t, func() bool {
					job.admission.mu.Lock()
					defer job.admission.mu.Unlock()
					return job.admission.pendingPhase == 1
				}, 3*time.Second, time.Millisecond)
				if kind == "queued cancel" {
					job.lifecycle.Cancel()
				} else {
					job.admission.MarkGone()
				}
			}
			select {
			case err := <-result:
				if kind == "queued delete" {
					require.ErrorIs(t, err, ErrJobGone)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("aborted admission did not return")
			}
			assert.Zero(t, atomic.LoadUint64(&receipt))
			assert.Zero(t, phaseCalls.Load())
			assert.Equal(t, uint64(7), job.lifecycle.StatusSnapshot().ApplyGeneration)
		})
	}
}
