package worker

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// organizerBackedWorkflow drives the apply phase with a REAL organizer (#241
// P2): apply commands route into organizer.Organize so the batch's shared
// duplicate tracker exercises its terminal-gated observation for real.
// vanishBeforeApply deletes a source right before its worker runs (the
// codex P2 vanish-between-prime-and-execute window); panicBeforeApply
// panics before the organizer is even reached (the owner-never-finishes
// window the apply phase's recovery boundary must close out).
type organizerBackedWorkflow struct {
	org               *organizer.Organizer
	fs                afero.Fs
	vanishBeforeApply map[string]bool
	panicBeforeApply  map[string]bool
	// sleepBeforeApply delays one file's worker apply leg (codex P2, PR #241
	// F1 resident-gating tests): with workers ≥ 2 a mover reaches the
	// resident's pending parked claim WHILE the resident still validates.
	sleepBeforeApply map[string]time.Duration
}

func (w *organizerBackedWorkflow) Scrape(context.Context, scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}

func (w *organizerBackedWorkflow) organizeCmd(cmd workflow.ApplyCmd) organizer.OrganizeCmd {
	return organizer.OrganizeCmd{
		Match:            cmd.Match,
		Movie:            cmd.Movie,
		DestDir:          cmd.DestPath,
		ForceUpdate:      cmd.Organize.ForceUpdate,
		MoveFiles:        cmd.Organize.MoveFiles,
		LinkMode:         cmd.Organize.LinkMode,
		DryRun:           cmd.DryRun,
		OperationMode:    cmd.OperationMode,
		ForceRenameFile:  cmd.Organize.ForceRenameFile,
		DuplicateTracker: cmd.Organize.DuplicateTracker,
	}
}

func (w *organizerBackedWorkflow) Apply(ctx context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	if d := w.sleepBeforeApply[cmd.Match.Path]; d > 0 {
		time.Sleep(d)
	}
	if w.panicBeforeApply[cmd.Match.Path] {
		panic("owner apply boom")
	}
	if w.vanishBeforeApply[cmd.Match.Path] {
		_ = w.fs.Remove(cmd.Match.Path)
	}
	res, err := w.org.Organize(ctx, w.organizeCmd(cmd))
	if err != nil {
		return nil, err
	}
	return &workflow.ApplyResult{Movie: cmd.Movie, OrganizeResult: res}, nil
}

func (w *organizerBackedWorkflow) Preview(context.Context, workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (w *organizerBackedWorkflow) Compare(context.Context, workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (w *organizerBackedWorkflow) ScanAndMatch(context.Context, workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

// PlanDuplicatePriming mirrors applyOrchImpl.planDuplicatePriming: the
// read-only plan plus the source-existence gate, so a claimant — mover or
// stationary resident alike (codex P2, PR #241 F1) — whose source is gone
// at priming time registers nothing.
func (w *organizerBackedWorkflow) PlanDuplicatePriming(ctx context.Context, cmd workflow.ApplyCmd) (organizer.DuplicatePriming, error) {
	if cmd.Organize.Skip {
		return organizer.DuplicatePriming{}, nil
	}
	plan, err := w.org.PlanOrganize(ctx, w.organizeCmd(cmd))
	if err != nil {
		return organizer.DuplicatePriming{}, err
	}
	if plan.TargetPath != "" && !w.org.PlanSourceExists(plan) {
		return organizer.DuplicatePriming{}, nil
	}
	return organizer.DuplicatePriming{
		SourcePath: plan.SourcePath,
		TargetPath: plan.TargetPath,
		WillMove:   plan.WillMove,
	}, nil
}

func w241DupPhaseFixture(t *testing.T, wf *organizerBackedWorkflow, maxWorkers int, force bool) (applyPhaseInputs, ApplyPhaseConfig, afero.Fs, string, *sync.Map) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	wf.fs = fs
	wf.org = organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)

	dest := filepath.Join(t.TempDir(), "dest")
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: maxWorkers, WorkerTimeout: 0}
	inputs.Destination = dest
	for _, p := range []string{"/in/A.mkv", "/in/B.mkv"} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, Name: filepath.Base(p), Extension: ".mkv", MovieID: "ABC-123"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-123", Title: "Shared Destination"},
		}
	}
	failed := &sync.Map{}
	cfg := ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true, ForceUpdate: force},
		Destination:     dest,
		OnFileFailed:    func(filePath, errMsg string) { failed.Store(filePath, errMsg) },
	}
	return inputs, cfg, fs, dest, failed
}

// runW241PhaseWithDeadline runs the apply phase on a helper goroutine so a
// claim-handling regression surfaces as a bounded deadline failure instead
// of a stuck test process (#241 P2 no-deadlock pin).
func runW241PhaseWithDeadline(t *testing.T, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewApplyPhase().Run(context.Background(), inputs, cfg)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("apply phase deadlocked waiting on duplicate claims (#241 P2)")
	}
}

// TestApplyPhase_OwnerVanishesMidBatch_LaterClaimantMoves is the #241 P2
// headline regression end to end: the sorted-first (primed) owner's source
// vanishes between priming and execute; the other claimant — possibly
// already blocked on the still-owned key — must be promoted and actually
// move the file, in both authorization modes.
func TestApplyPhase_OwnerVanishesMidBatch_LaterClaimantMoves(t *testing.T) {
	for _, force := range []bool{false, true} {
		mode := "normal mode"
		if force {
			mode = "force mode"
		}
		t.Run(mode, func(t *testing.T) {
			wf := &organizerBackedWorkflow{vanishBeforeApply: map[string]bool{"/in/A.mkv": true}}
			inputs, cfg, fs, dest, failed := w241DupPhaseFixture(t, wf, 2, force)

			runW241PhaseWithDeadline(t, inputs, cfg)

			content, err := afero.ReadFile(fs, filepath.Join(dest, "ABC-123", "ABC-123.mkv"))
			require.NoError(t, err, "the valid later claimant's bytes land on the shared destination")
			assert.Equal(t, []byte("b-bytes"), content)
			_, statErr := fs.Stat("/in/B.mkv")
			assert.Error(t, statErr, "the promoted claimant really moved out of its source")
			ownerSrc, readErr := afero.ReadFile(fs, "/in/A.mkv")
			require.Error(t, readErr, "the vanished owner's source stays gone")
			assert.Nil(t, ownerSrc)
			failMsg, failedA := failed.Load("/in/A.mkv")
			assert.True(t, failedA, "the vanished owner records its apply failure")
			_, failedB := failed.Load("/in/B.mkv")
			assert.False(t, failedB, "the later claimant succeeds in %s: %v", mode, failMsg)
		})
	}
}

// TestApplyPhase_OwnerPanicReleasesClaims is the #241 P2 owner-never-finishes
// pin: the primed owner's worker panics BEFORE the organizer runs, so only
// the apply phase's recovery-boundary claim close-out can free the key — the
// blocked claimant must still be promoted and move the file.
func TestApplyPhase_OwnerPanicReleasesClaims(t *testing.T) {
	wf := &organizerBackedWorkflow{panicBeforeApply: map[string]bool{"/in/A.mkv": true}}
	inputs, cfg, fs, dest, _ := w241DupPhaseFixture(t, wf, 2, false)

	runW241PhaseWithDeadline(t, inputs, cfg)

	content, err := afero.ReadFile(fs, filepath.Join(dest, "ABC-123", "ABC-123.mkv"))
	require.NoError(t, err, "the claimed destination is never left empty behind a panicked owner")
	assert.Equal(t, []byte("b-bytes"), content)
	ownerSrc, readErr := afero.ReadFile(fs, "/in/A.mkv")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("a-bytes"), ownerSrc, "the panicked owner never touched its source")
}

// TestApplyPhase_MaxWorkers1SequentialDuplicateSemantics pins the #241 P2
// compatibility clause: with one worker the sorted owner completes before
// the loser ever starts, so the terminal gate never blocks and today's
// outcomes stand byte for byte.
func TestApplyPhase_MaxWorkers1SequentialDuplicateSemantics(t *testing.T) {
	t.Run("normal mode: winner moves, loser duplicate-conflicts", func(t *testing.T) {
		wf := &organizerBackedWorkflow{}
		inputs, cfg, fs, dest, failed := w241DupPhaseFixture(t, wf, 1, false)

		runW241PhaseWithDeadline(t, inputs, cfg)

		content, err := afero.ReadFile(fs, filepath.Join(dest, "ABC-123", "ABC-123.mkv"))
		require.NoError(t, err)
		assert.Equal(t, []byte("a-bytes"), content, "the sorted winner moves as today")
		loserSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), loserSrc, "the losing duplicate's source is untouched")
		failMsg, failedB := failed.Load("/in/B.mkv")
		require.True(t, failedB, "the loser still duplicate-conflicts")
		assert.Contains(t, failMsg.(string), "ABC-123")
		_, failedA := failed.Load("/in/A.mkv")
		assert.False(t, failedA)
	})

	t.Run("force mode: winner moves, loser warns and skips", func(t *testing.T) {
		wf := &organizerBackedWorkflow{}
		inputs, cfg, fs, dest, failed := w241DupPhaseFixture(t, wf, 1, true)

		runW241PhaseWithDeadline(t, inputs, cfg)

		content, err := afero.ReadFile(fs, filepath.Join(dest, "ABC-123", "ABC-123.mkv"))
		require.NoError(t, err)
		assert.Equal(t, []byte("a-bytes"), content)
		loserSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), loserSrc)
		_, failedA := failed.Load("/in/A.mkv")
		_, failedB := failed.Load("/in/B.mkv")
		assert.False(t, failedA || failedB, "an authorized duplicate still warns instead of failing")
	})
}
