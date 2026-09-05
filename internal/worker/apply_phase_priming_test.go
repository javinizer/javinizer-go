package worker

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// primingStubWorkflow extends stubApplyWorkflow with the optional
// workflow.DuplicatePrimingPlanner seam, recording priming attempts in call
// order and any Apply dispatched before every priming completed.
type primingStubWorkflow struct {
	stubApplyWorkflow
	planErrOn map[string]bool

	mu         sync.Mutex
	primeCalls []string
	applyCmds  []workflow.ApplyCmd
	earlyApply []string
	total      int
}

func (s *primingStubWorkflow) PlanDuplicatePriming(_ context.Context, cmd workflow.ApplyCmd) (organizer.DuplicatePriming, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := cmd.Match.Path
	s.primeCalls = append(s.primeCalls, src)
	if s.planErrOn[src] {
		return organizer.DuplicatePriming{}, fmt.Errorf("plan boom for %s", src)
	}
	return organizer.DuplicatePriming{
		SourcePath: src,
		TargetPath: filepath.Join(cmd.DestPath, strings.ToLower(cmd.Match.MovieID)+".mkv"),
		WillMove:   true,
	}, nil
}

func (s *primingStubWorkflow) Apply(ctx context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	s.mu.Lock()
	if len(s.primeCalls) < s.total {
		s.earlyApply = append(s.earlyApply, cmd.Match.Path)
	}
	s.applyCmds = append(s.applyCmds, cmd)
	s.mu.Unlock()
	return s.stubApplyWorkflow.Apply(ctx, cmd)
}

func (s *primingStubWorkflow) snapshot() (primeCalls []string, applyCmds []workflow.ApplyCmd, early []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.primeCalls...), append([]workflow.ApplyCmd(nil), s.applyCmds...), append([]string(nil), s.earlyApply...)
}

// TestApplyPhase_Run_PrimesDuplicateOwnersInSortedOrder pins #240 finding A:
// the apply phase plans every sorted item exactly once BEFORE any worker
// starts and primes the batch's shared tracker in that sorted order.
func TestApplyPhase_Run_PrimesDuplicateOwnersInSortedOrder(t *testing.T) {
	wf := &primingStubWorkflow{total: 3}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 4, WorkerTimeout: 0}
	for _, p := range []string{"/source/m-3.mp4", "/source/a-1.mp4", "/source/z-2.mp4"} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100", Title: "Shared Destination"},
		}
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
	})

	primeCalls, applyCmds, early := wf.snapshot()
	assert.Equal(t, []string{"/source/a-1.mp4", "/source/m-3.mp4", "/source/z-2.mp4"}, primeCalls,
		"every item is planned exactly once per run, before fan-out, in sorted order")
	assert.Empty(t, early, "no worker Apply may start before every claim is primed")
	require.Len(t, applyCmds, 3)
	for _, cmd := range applyCmds {
		require.NotNil(t, cmd.Organize.DuplicateTracker)
		assert.Same(t, applyCmds[0].Organize.DuplicateTracker, cmd.Organize.DuplicateTracker,
			"primed tracker is shared by every file")
	}
}

// TestApplyPhase_Run_PrimingSkipsUnexecutableAndUnplannableItems pins the two
// priming skip legs: a PreApply-declined item registers no claim (and never
// applies), while a planning failure only forfeits priming — the item still
// applies and fails through its own identical plan error.
func TestApplyPhase_Run_PrimingSkipsUnexecutableAndUnplannableItems(t *testing.T) {
	wf := &primingStubWorkflow{total: 2, planErrOn: map[string]bool{"/source/bad.mp4": true}}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 2, WorkerTimeout: 0}
	for _, p := range []string{"/source/skip.mp4", "/source/bad.mp4", "/source/good.mp4"} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100"},
		}
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		PreApplyFunc: func(_ context.Context, afc *ApplyFileContext) error {
			if afc.FilePath == "/source/skip.mp4" {
				return fmt.Errorf("skip this file")
			}
			return nil
		},
	})

	primeCalls, applyCmds, _ := wf.snapshot()
	assert.Equal(t, []string{"/source/bad.mp4", "/source/good.mp4"}, primeCalls,
		"sorted priming attempts skip hook-declined items and continue past plan failures")
	appliedPaths := make([]string, 0, len(applyCmds))
	for _, cmd := range applyCmds {
		appliedPaths = append(appliedPaths, cmd.Match.Path)
	}
	assert.ElementsMatch(t, []string{"/source/bad.mp4", "/source/good.mp4"}, appliedPaths,
		"a plan failure forfeits priming but never drops the item from apply")
}

// TestApplyPhase_Run_DuplicatePreflightProbePolicy pins #240 finding B at the
// batch boundary: dry runs construct the non-probing tracker (zero probes,
// zero probe artifacts), while live runs probe exactly once per root, exactly
// as before.
func TestApplyPhase_Run_DuplicatePreflightProbePolicy(t *testing.T) {
	snapshotTree := func(t *testing.T, root string) map[string]bool {
		t.Helper()
		entries := map[string]bool{}
		require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			entries[path] = d.IsDir()
			return nil
		}))
		return entries
	}

	runBatch := func(t *testing.T, dryRun bool) (dest string, caseCalls, normCalls int) {
		t.Helper()
		fsutil.ResetCaseSensitivityCache()
		fsutil.ResetNormalizationCache()
		cc, nc := 0, 0
		prevCase, prevNorm := fsutil.CaseSensitiveProbe, fsutil.NormalizationProbe
		fsutil.CaseSensitiveProbe = func(string) (bool, error) { cc++; return false, nil }
		fsutil.NormalizationProbe = func(string) (bool, error) { nc++; return true, nil }
		defer func() {
			fsutil.CaseSensitiveProbe = prevCase
			fsutil.NormalizationProbe = prevNorm
			fsutil.ResetCaseSensitivityCache()
			fsutil.ResetNormalizationCache()
		}()

		dest = t.TempDir()
		before := snapshotTree(t, dest)

		wf := &primingStubWorkflow{total: 2}
		wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
		inputs := makeApplyInputs(wf)
		for _, p := range []string{"/source/a-1.mp4", "/source/z-2.mp4"} {
			inputs.Results[p] = &resultstore.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "M-100"},
			}
		}
		NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
			DryRun:          dryRun,
			OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
			Destination:     dest,
		})

		assert.Equal(t, before, snapshotTree(t, dest),
			"no probe artifacts may survive the run (the duplicate preflight adds no disk writes)")
		return dest, cc, nc
	}

	t.Run("dry run performs zero probe writes", func(t *testing.T) {
		_, cc, nc := runBatch(t, true)
		assert.Equal(t, 0, cc, "dry runs never run the writing case probe")
		assert.Equal(t, 0, nc, "dry runs never run the writing normalization probe")
	})

	t.Run("live run probing is unchanged", func(t *testing.T) {
		_, cc, nc := runBatch(t, false)
		assert.Equal(t, 1, cc, "live runs keep one case probe per root per process")
		assert.Equal(t, 1, nc, "live runs keep one normalization probe per root per process")
	})
}
