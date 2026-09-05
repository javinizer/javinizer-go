package organizer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// parkedEntry asserts the canonical key of target is owned by a pre-settled
// parked resident (codex P1, PR #241) and returns its (immutable-after-
// settle) entry: settled-success, done already closed — a moving claimant
// must never block behind a resident's claim.
func parkedEntry(t *testing.T, tracker *DuplicateTracker, target string) *claimEntry {
	t.Helper()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	entry, ok := tracker.claims[tracker.keyLocked(target)]
	require.True(t, ok, "no claim registered for %s", target)
	require.True(t, entry.settled, "a resident's claim is born settled")
	require.True(t, entry.success, "a resident parks settled-successfully")
	require.True(t, entry.parked, "a born-settled resident claim carries the parked discriminant (codex P2, PR #241 F1)")
	select {
	case <-entry.done:
	default:
		t.Fatalf("a parked resident claim closes done at priming — movers must resolve instantly")
	}
	return entry
}

// TestDuplicateTracker_ResidentParking pins codex P1 (PR #241): a stationary
// batch input (WillMove=false — already sitting at its computed destination)
// owns its canonical key at priming time, so a mover computing the same
// destination loses to the resident's bytes INSTEAD of becoming the key's
// sole claimant and replacing them under ForceUpdate.
func TestDuplicateTracker_ResidentParking(t *testing.T) {
	residentPriming := DuplicatePriming{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false}
	moverPriming := DuplicatePriming{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true}

	t.Run("the resident owns its key regardless of priming order", func(t *testing.T) {
		for _, primings := range [][]DuplicatePriming{
			{residentPriming, moverPriming}, // resident sorted first
			{moverPriming, residentPriming}, // mover sorted before the resident
		} {
			forceCasePosture(t, true)
			tracker := NewDuplicateTracker(false)
			tracker.PrimeBatch(primings)
			entry := parkedEntry(t, tracker, "/dest/lib/x.mkv")
			assert.Equal(t, "/dest/lib/x.mkv", entry.claim.source,
				"ownership must not be priming-arrival order: the resident wins even sorted last")

			// The resident's own worker never conflicts with its own
			// destination — a WillMove=false plan registers nothing at observe.
			_, dup := tracker.observe(context.Background(), &OrganizePlan{
				SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false,
			})
			assert.False(t, dup, "the resident reports a normal no-op, never a conflict")

			// The primed mover takes the resident-claimed duplicate verdict,
			// instantly (the parked claim is terminal).
			prior, dup := tracker.observe(context.Background(), moverPrimingPlan())
			require.True(t, dup)
			assert.Equal(t, "/dest/lib/x.mkv", prior.source)

			// An unprimed mover on the same key loses identically.
			prior, dup = tracker.observe(context.Background(), dupPlanFor("/in/D.mkv", "/dest/lib/x.mkv"))
			require.True(t, dup)
			assert.Equal(t, "/dest/lib/x.mkv", prior.source)
		}
	})

	t.Run("parked claims are terminal and never release or re-settle", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{residentPriming, moverPriming})
		parkedEntry(t, tracker, "/dest/lib/x.mkv")

		// Every terminal close-out aimed at the resident — even through a
		// WillMove=true plan spelling that passes the register-nothing
		// guards — is a no-op against the settled parked claim.
		stillSpelling := &OrganizePlan{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true}
		tracker.release(stillSpelling)
		tracker.settle(stillSpelling)
		tracker.ReleaseAbandonedBy("/dest/lib/x.mkv")
		tracker.ReleaseAbandonedBy("/in/B.mkv") // the mover's inert standby scrubs too

		prior, dup := tracker.observe(context.Background(), moverPrimingPlan())
		require.True(t, dup, "the parked key survives every close-out attempt — resident bytes stay claimed")
		assert.Equal(t, "/dest/lib/x.mkv", prior.source)
	})

	t.Run("a second resident retires to the parked key's inert standby", func(t *testing.T) {
		forceCasePosture(t, false) // case-insensitive root: X/x fold into one canonical key
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false},
			{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false}, // verbatim re-priming: no-op
			{SourcePath: "/dest/lib/X.mkv", TargetPath: "/dest/lib/X.mkv", WillMove: false},
		})
		entry := parkedEntry(t, tracker, "/dest/lib/x.mkv")
		tracker.mu.Lock()
		assert.Equal(t, []string{"/dest/lib/X.mkv"}, entry.standby,
			"the second spelling of the resident retires inertly; parked claims never promote it")
		tracker.mu.Unlock()

		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/dest/lib/x.mkv", prior.source, "the first sorted resident owns the folded key")
	})

	t.Run("empty-target resident primings register nothing", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "", WillMove: false},
		})
		tracker.mu.Lock()
		assert.Empty(t, tracker.claims, "register-nothing still guards every targetless priming")
		tracker.mu.Unlock()
	})

	t.Run("concurrent movers all resolve instantly against a parked resident", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{residentPriming})
		var wg sync.WaitGroup
		dups := make(chan bool, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, dup := tracker.observe(context.Background(), dupPlanFor(
					fmt.Sprintf("/in/mover-%d.mkv", i), "/dest/lib/x.mkv"))
				dups <- dup
			}(i)
		}
		wg.Wait()
		close(dups)
		for dup := range dups {
			assert.True(t, dup, "every mover resolves to the duplicate verdict — none block and none win")
		}
	})

	t.Run("claims stay batch-scoped across run boundaries", func(t *testing.T) {
		forceCasePosture(t, true)
		runOne := NewDuplicateTracker(false)
		runOne.PrimeBatch([]DuplicatePriming{residentPriming})
		parkedEntry(t, runOne, "/dest/lib/x.mkv")

		// The next run gets a FRESH tracker: a re-processed resident parks
		// its key anew and never conflicts with itself or its previous run.
		runTwo := NewDuplicateTracker(false)
		runTwo.PrimeBatch([]DuplicatePriming{residentPriming})
		_, dup := runTwo.observe(context.Background(), &OrganizePlan{
			SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false,
		})
		assert.False(t, dup, "the re-processed resident's own no-op is never flagged")

		// And with nothing else primed, an unrelated destination stays free.
		_, dup = runTwo.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/y.mkv"))
		assert.False(t, dup, "no claim bleeds across the run boundary")
	})
}

// moverPrimingPlan is the mover half of the resident/mover collision plan.
func moverPrimingPlan() *OrganizePlan {
	return dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv")
}

// TestApplyDuplicatePreflight_ResidentOwner pins the resident-claimed verdict
// shape at the preflight boundary (codex P1, PR #241): identical to the
// mover-owned pipeline — unauthorized joins the ordinary duplicate conflict,
// authorized demotes to the persisted warning + skip.
func TestApplyDuplicatePreflight_ResidentOwner(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"
	tracker := NewDuplicateTracker(false)
	tracker.PrimeBatch([]DuplicatePriming{
		{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true}, // mover primed FIRST
		{SourcePath: target, TargetPath: target, WillMove: false},     // resident still owns
	})

	t.Run("unauthorized mover joins the ordinary duplicate conflict", func(t *testing.T) {
		plan := dupPlanFor("/in/B.mkv", target)
		warnings, skip := applyDuplicatePreflight(context.Background(), plan, tracker, false)
		assert.Nil(t, warnings)
		assert.False(t, skip)
		require.Len(t, plan.Conflicts, 1)
		assert.Equal(t, PlanConflict{Path: target, Kind: ConflictDuplicate}, plan.Conflicts[0])
	})

	t.Run("authorized mover warns and skips — the resident's bytes stay claimed", func(t *testing.T) {
		plan := dupPlanFor("/in/B.mkv", target)
		warnings, skip := applyDuplicatePreflight(context.Background(), plan, tracker, true)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "overwrite authorized")
		assert.Contains(t, warnings[0], target)
		assert.True(t, skip)
		assert.Empty(t, plan.Conflicts, "authorization demotes the duplicate out of the conflict pipeline")
	})
}

// residentFixture builds an organizer whose batch already CONTAINS file A
// parked at its computed destination, plus mover file B whose plan computes
// the same destination — the codex P1 (PR #241) collision shape.
func residentFixture(t *testing.T) (*Organizer, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/dest/ABC-123/ABC-123.mkv", []byte("resident-bytes"), 0o644))
	return org, fs
}

const residentTarget = "/dest/ABC-123/ABC-123.mkv"

func residentMatch() models.FileMatchInfo {
	return models.FileMatchInfo{MovieID: "ABC-123", Path: residentTarget, Name: "ABC-123.mkv", Extension: ".mkv"}
}

func moverMatch() models.FileMatchInfo {
	return models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}
}

// primeResidentFirst primes the resident/mover batch MOVER-FIRST on purpose
// (codex P1, PR #241 test (c)): sorted/arrival order must not decide
// ownership — the parked resident always beats the mover.
func primeResidentFirst(tracker *DuplicateTracker) {
	tracker.PrimeBatch([]DuplicatePriming{
		{SourcePath: "/in/B.mkv", TargetPath: residentTarget, WillMove: true},
		{SourcePath: residentTarget, TargetPath: residentTarget, WillMove: false},
	})
}

// TestOrganize_ResidentVsMover pins the finding end to end through the full
// plan → preflight → execute pipeline: the mover can never land its bytes on
// the resident's destination, in either authorization mode, and the resident
// reports its ordinary no-op.
func TestOrganize_ResidentVsMover(t *testing.T) {
	forceCasePosture(t, true)

	t.Run("normal mode: the mover's duplicate conflict leaves the resident untouched", func(t *testing.T) {
		org, fs := residentFixture(t)
		tracker := NewDuplicateTracker(false)
		primeResidentFirst(tracker)

		// The resident's own organize is the normal no-op: no error, no move.
		resA, err := org.Organize(context.Background(), dupBatchCmd(residentMatch(), tracker, false, false))
		require.NoError(t, err)
		require.NotNil(t, resA)
		assert.False(t, resA.Moved, "the resident moves nothing")

		// The mover's plan joins the duplicate-conflict pipeline and fails.
		_, err = org.Organize(context.Background(), dupBatchCmd(moverMatch(), tracker, false, false))
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "organization validation failed: ["), err.Error())
		assert.Contains(t, filepath.ToSlash(err.Error()), residentTarget)

		content, readErr := afero.ReadFile(fs, filepath.FromSlash(residentTarget))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content, "the resident's bytes are untouched")
		moverSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), moverSrc, "the rejected mover's source stays in place")
	})

	t.Run("force mode: authorized-skip journals the warning and no overwrite lands", func(t *testing.T) {
		org, fs := residentFixture(t)
		tracker := NewDuplicateTracker(false)
		primeResidentFirst(tracker)

		// The mover organizes FIRST (arrival order must not matter)…
		resB, err := org.Organize(context.Background(), dupBatchCmd(moverMatch(), tracker, true, false))
		require.NoError(t, err, "authorization demotes the duplicate to a warning, never a failure")
		require.NotNil(t, resB)
		assert.True(t, resB.DuplicateSkipped, "the mover skips execution against the parked resident")
		assert.False(t, resB.Moved)
		require.Len(t, resB.Warnings, 1)
		assert.Contains(t, resB.Warnings[0], "overwrite authorized")

		// …and the resident then reports its ordinary no-op.
		resA, err := org.Organize(context.Background(), dupBatchCmd(residentMatch(), tracker, true, false))
		require.NoError(t, err)
		assert.False(t, resA.Moved)

		content, readErr := afero.ReadFile(fs, filepath.FromSlash(residentTarget))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content,
			"codex P1 regression pin: ForceUpdate must NOT replace the resident's bytes with the mover's")
		moverSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), moverSrc)
	})

	t.Run("resident-only rerun: re-processing the organized library flags nothing", func(t *testing.T) {
		org, fs := residentFixture(t)
		for run := 1; run <= 2; run++ {
			tracker := NewDuplicateTracker(false) // claims are batch-scoped: one tracker per run
			tracker.PrimeBatch([]DuplicatePriming{
				{SourcePath: residentTarget, TargetPath: residentTarget, WillMove: false},
			})
			resA, err := org.Organize(context.Background(), dupBatchCmd(residentMatch(), tracker, run%2 == 0, false))
			require.NoError(t, err, "run %d: the lone resident is never flagged duplicate — not even by its previous run", run)
			assert.False(t, resA.Moved)
			assert.False(t, resA.DuplicateSkipped)
			assert.Empty(t, resA.Warnings)
		}
		content, readErr := afero.ReadFile(fs, filepath.FromSlash(residentTarget))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content)
	})
}
