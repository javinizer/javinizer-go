package worker

import (
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
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// w241ResidentPhaseFixture builds the codex P2 (PR #241 F1) resident-gate
// collision: a batch that CONTAINS the already-organized resident (its file
// sits at its computed destination) PLUS a mover whose plan computes the
// same destination. The destination root deliberately sorts AFTER the
// mover's /in path, so without the apply phase's residents-first scheduling
// a single-worker run would deadlock on the pending parked claim.
func w241ResidentPhaseFixture(t *testing.T, wf *organizerBackedWorkflow, maxWorkers int, force bool) (applyPhaseInputs, ApplyPhaseConfig, afero.Fs, string, string, *sync.Map) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	wf.fs = fs
	wf.org = organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)

	dest := filepath.Join(t.TempDir(), "dest")
	residentPath := filepath.Join(dest, "ABC-123", "ABC-123.mkv")
	require.NoError(t, fs.MkdirAll(filepath.Dir(residentPath), 0o755))
	require.NoError(t, afero.WriteFile(fs, residentPath, []byte("resident-bytes"), 0o644))

	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: maxWorkers, WorkerTimeout: 0}
	inputs.Destination = dest
	for _, p := range []string{"/in/B.mkv", residentPath} {
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
	return inputs, cfg, fs, dest, residentPath, failed
}

// TestApplyPhase_ResidentGate_ValidResidentKeepsItsBytes pins the F1
// resident-valid half end to end: with the resident deliberately SLOWER than
// the mover (multi-worker: the mover reaches observe while the resident
// still validates; single-worker: residents-first scheduling validates the
// resident before the mover starts), the mover's duplicate outcome resolves
// only AFTER the resident's own terminal success — the resident's bytes stay
// put and the mover duplicates (normal mode: conflict failure; force mode:
// warning + skip) identically at every worker count.
func TestApplyPhase_ResidentGate_ValidResidentKeepsItsBytes(t *testing.T) {
	for _, maxWorkers := range []int{1, 2} {
		for _, force := range []bool{false, true} {
			t.Run(workerForceName(maxWorkers, force), func(t *testing.T) {
				wf := &organizerBackedWorkflow{}
				inputs, cfg, fs, _, residentPath, failed := w241ResidentPhaseFixture(t, wf, maxWorkers, force)
				wf.sleepBeforeApply = map[string]time.Duration{residentPath: 200 * time.Millisecond}

				runW241PhaseWithDeadline(t, inputs, cfg)

				content, err := afero.ReadFile(fs, residentPath)
				require.NoError(t, err)
				assert.Equal(t, []byte("resident-bytes"), content,
					"the validated resident's bytes stay put — the mover never replaces them")
				moverSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
				require.NoError(t, readErr)
				assert.Equal(t, []byte("b-bytes"), moverSrc, "the duplicated mover's source is untouched")
				_, failedResident := failed.Load(residentPath)
				assert.False(t, failedResident, "the present resident validates and never fails")
				failMsg, failedMover := failed.Load("/in/B.mkv")
				if force {
					assert.False(t, failedMover, "force mode demotes the mover's duplicate to a warning + skip")
				} else {
					require.True(t, failedMover, "normal mode duplicate-conflicts the mover once the resident settles")
					assert.Contains(t, failMsg.(string), "ABC-123")
				}
			})
		}
	}
}

// TestApplyPhase_GhostResidentGate_MoverLandsBytes pins the F1 headline
// regression end to end: the resident's source vanishes between the pre-park
// verification and its own validation; the mover — which under a born-settled
// parked claim would have verdicted skip and FINISHED, leaving the
// destination empty — now gates on the resident's terminal failure, promotes
// onto the released key, and lands its bytes. Identical at every worker
// count and in both authorization modes (the resident path sorts AFTER the
// mover, additionally pinning the residents-first single-worker scheduling).
func TestApplyPhase_GhostResidentGate_MoverLandsBytes(t *testing.T) {
	for _, maxWorkers := range []int{1, 2} {
		for _, force := range []bool{false, true} {
			t.Run(workerForceName(maxWorkers, force), func(t *testing.T) {
				wf := &organizerBackedWorkflow{}
				inputs, cfg, fs, _, residentPath, failed := w241ResidentPhaseFixture(t, wf, maxWorkers, force)
				wf.vanishBeforeApply = map[string]bool{residentPath: true}

				runW241PhaseWithDeadline(t, inputs, cfg)

				content, err := afero.ReadFile(fs, residentPath)
				require.NoError(t, err, "the mover's bytes land on the released ghost destination")
				assert.Equal(t, []byte("b-bytes"), content)
				_, statErr := fs.Stat("/in/B.mkv")
				assert.Error(t, statErr, "the promoted mover really moved out of its source")
				_, failedResident := failed.Load(residentPath)
				assert.True(t, failedResident, "the vanished resident records its apply failure")
				_, failedMover := failed.Load("/in/B.mkv")
				assert.False(t, failedMover, "the mover executes instead of dying on the ghost's claim")
			})
		}
	}
}

// workerForceName labels the worker-count × authorization sub-buckets.
func workerForceName(maxWorkers int, force bool) string {
	name := "sequential"
	if maxWorkers > 1 {
		name = "concurrent"
	}
	if force {
		return name + "/force"
	}
	return name + "/normal"
}

// TestApplyPhase_ResidentGate_AbandonedGateFreesMovers pins the F1
// no-deadlock clause: a resident whose worker panics BEFORE the organizer
// settles anything is freed by the apply phase's recovery boundary — with the
// pending-parked lifecycle the claim is no longer pre-settled, so
// ReleaseAbandonedBy closes it out exactly like a dead mover owner and the
// gated mover resolves (never hangs on the dead resident's claim). The
// resident's bytes are still IN PLACE here — only its worker died — so the
// promoted mover takes the ordinary destination-occupation conflict instead
// of overwriting them.
func TestApplyPhase_ResidentGate_AbandonedGateFreesMovers(t *testing.T) {
	wf := &organizerBackedWorkflow{}
	inputs, cfg, fs, _, residentPath, _ := w241ResidentPhaseFixture(t, wf, 2, false)
	wf.panicBeforeApply = map[string]bool{residentPath: true}

	runW241PhaseWithDeadline(t, inputs, cfg)

	content, err := afero.ReadFile(fs, residentPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("resident-bytes"), content,
		"the panicked resident's in-place bytes are never overwritten by the promoted mover")
	moverSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("b-bytes"), moverSrc)
	// The deadline runner above completing IS the pin: with a born-settled
	// (F1 pre-fix) ghost the mover verdicted instantly against a claim nothing
	// executed behind; with a never-freed pending gate it would hang here.
}
