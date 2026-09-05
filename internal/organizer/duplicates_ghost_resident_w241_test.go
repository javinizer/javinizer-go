package organizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// residentPlan builds the RESIDENT half of a collision: a WillMove=false plan
// whose source already sits at its computed destination — the spelling the
// resident's own worker hands to settle/release on its Organize legs.
func residentPlan(src string) *OrganizePlan {
	return &OrganizePlan{SourcePath: src, TargetPath: src, WillMove: false}
}

// claimOwner returns the recorded owner of the canonical key of target.
func claimOwner(t *testing.T, tracker *DuplicateTracker, target string) (string, bool) {
	t.Helper()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	entry, ok := tracker.claims[tracker.keyLocked(target)]
	if !ok {
		return "", false
	}
	return entry.claim.source, true
}

// TestDuplicateTracker_GhostResidentRelease pins codex P2 (PR #241 F1) at the
// tracker boundary: a parked resident whose OWN worker fails (its source
// vanished between the pre-park verification and validation) releases its
// PENDING parked claim through the identical terminal-failure machinery as a
// mover owner — sorted-next standby promotes, or the drained key frees — so
// the destination is never sealed behind a ghost for the rest of the run.
func TestDuplicateTracker_GhostResidentRelease(t *testing.T) {
	t.Run("a failed resident promotes its primed standby mover", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		parkedEntry(t, tracker, "/dest/lib/x.mkv")

		// Foreign and non-owner close-outs still release nothing — a stranger's
		// WillMove=false release spares the PENDING parked claim.
		tracker.release(residentPlan("/in/stranger.mkv"))
		parkedEntry(t, tracker, "/dest/lib/x.mkv")

		// An unrelated mover gates on the resident's OWN terminal outcome: it
		// blocks while the resident still stands and is carried forward as a
		// waiter across the failure promotion below.
		type outcome struct {
			prior duplicateClaim
			dup   bool
		}
		moverDOut := make(chan outcome, 1)
		go func() {
			prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/D.mkv", "/dest/lib/x.mkv"))
			moverDOut <- outcome{prior, dup}
		}()
		waitForWaiter(t, tracker, "/dest/lib/x.mkv", "/in/D.mkv")

		tracker.release(residentPlan("/dest/lib/x.mkv"))

		// The primed standby mover owns the key now and proceeds.
		owner, ok := claimOwner(t, tracker, "/dest/lib/x.mkv")
		require.True(t, ok)
		assert.Equal(t, "/in/B.mkv", owner, "the standby mover promotes onto the released ghost key")
		_, dup := tracker.observe(context.Background(), moverPrimingPlan())
		assert.False(t, dup, "the promoted mover falls through as owner — it moves")
		settleClaim(tracker, "/in/B.mkv", "/dest/lib/x.mkv")

		// The blocked waiter wakes to the NEW owner's verdict, never the ghost's.
		resD := <-moverDOut
		require.True(t, resD.dup)
		assert.Equal(t, "/in/B.mkv", resD.prior.source)

		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/E.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup, "later movers dup against the promoted owner, not the ghost")
		assert.Equal(t, "/in/B.mkv", prior.source)
	})

	t.Run("a failed resident with a drained queue frees the key outright", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false},
		})
		parkedEntry(t, tracker, "/dest/lib/x.mkv")

		tracker.release(residentPlan("/dest/lib/x.mkv"))

		_, ok := claimOwner(t, tracker, "/dest/lib/x.mkv")
		assert.False(t, ok, "no standby and no waiters: the ghost key frees")
		_, dup := tracker.observe(context.Background(), moverPrimingPlan())
		assert.False(t, dup, "the next mover registers the freed key first-come")
	})

	t.Run("a folded second resident promotes, settles, then releases like any owner", func(t *testing.T) {
		forceCasePosture(t, false) // alias-folding root: x.mkv and X.mkv share one key
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false},
			{SourcePath: "/dest/lib/X.mkv", TargetPath: "/dest/lib/X.mkv", WillMove: false},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})

		// First resident fails: the folded second spelling promotes out of
		// standby into an OPEN claim (ahead of the mover).
		tracker.release(residentPlan("/dest/lib/x.mkv"))
		owner, ok := claimOwner(t, tracker, "/dest/lib/x.mkv")
		require.True(t, ok)
		assert.Equal(t, "/dest/lib/X.mkv", owner)

		// The promoted resident's own observe is its ordinary no-op, and the
		// primed mover still loses to it — after waiting for its terminal
		// outcome (the promoted claim is open, not born-settled).
		_, dup := tracker.observe(context.Background(), residentPlan("/dest/lib/X.mkv"))
		assert.False(t, dup, "the promoted resident never conflicts with itself")
		type outcome struct {
			prior duplicateClaim
			dup   bool
		}
		moverResult := make(chan outcome, 1)
		go func() {
			prior, dup := tracker.observe(context.Background(), moverPrimingPlan())
			moverResult <- outcome{prior, dup}
		}()
		waitForWaiter(t, tracker, "/dest/lib/x.mkv", "/in/B.mkv")

		// The promoted resident's successful no-op settles the open claim it
		// owns (settle is no longer guarded away by WillMove=false)…
		tracker.settle(residentPlan("/dest/lib/X.mkv"))
		result := <-moverResult
		require.True(t, result.dup, "…and the mover wakes to the promoted resident's verdict")
		assert.Equal(t, "/dest/lib/X.mkv", result.prior.source)
	})

	t.Run("a promoted resident's own failure hands the key onward to the mover", func(t *testing.T) {
		forceCasePosture(t, false)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/dest/lib/x.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false},
			{SourcePath: "/dest/lib/X.mkv", TargetPath: "/dest/lib/X.mkv", WillMove: false},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})

		tracker.release(residentPlan("/dest/lib/x.mkv")) // first resident fails
		tracker.observe(context.Background(), residentPlan("/dest/lib/X.mkv"))
		moverResult := make(chan bool, 1)
		go func() {
			_, dup := tracker.observe(context.Background(), moverPrimingPlan())
			moverResult <- dup
		}()
		waitForWaiter(t, tracker, "/dest/lib/x.mkv", "/in/B.mkv")

		// The promoted resident fails too: a WillMove=false plan owning an
		// OPEN claim releases it, promoting the standby mover out of its wait.
		tracker.release(residentPlan("/dest/lib/X.mkv"))
		assert.False(t, <-moverResult, "the mover promotes behind BOTH failed residents and proceeds")
		owner, ok := claimOwner(t, tracker, "/dest/lib/x.mkv")
		require.True(t, ok)
		assert.Equal(t, "/in/B.mkv", owner)
	})

	t.Run("a settled mover claim ignores a WillMove=false close-out", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")

		// The parked-resident exception reaches ONLY parked claims: a settled
		// mover's verdict stays final even under a WillMove=false spelling of
		// its own source path.
		tracker.release(&OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: false})
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup, "the settled mover keeps its key — the exception is parked-resident-only")
		assert.Equal(t, "/in/A.mkv", prior.source)
	})
}

// TestOrganize_GhostResident_MoverProceeds pins codex P2 (PR #241 F1) end to
// end through the full plan → preflight → validate → execute pipeline: a
// resident vanishing in the priming→worker gap fails its OWN validation,
// releasing the parked claim so the primed mover proceeds to move (normal
// mode); a resident already gone at priming is never parked by the gate, so
// under ForceUpdate the mover owns the key outright with no winner-skip
// inconsistency.
func TestOrganize_GhostResident_MoverProceeds(t *testing.T) {
	forceCasePosture(t, true)

	t.Run("normal mode: the resident's validation failure frees the ghost and the mover moves", func(t *testing.T) {
		org, fs := residentFixture(t)
		tracker := NewDuplicateTracker(false)
		primeResidentFirst(tracker)

		// The resident vanishes AFTER priming but BEFORE its own worker
		// validates it — and its worker organizes first.
		require.NoError(t, fs.Remove(residentTarget))
		_, err := org.Organize(context.Background(), dupBatchCmd(residentMatch(), tracker, false, false))
		require.Error(t, err, "the vanished resident fails validation like any inexecutable plan")
		assert.Contains(t, err.Error(), "organization validation failed")
		assert.Contains(t, filepath.ToSlash(err.Error()), "source file does not exist")

		// The parked ghost is gone: the primed standby mover proceeds.
		resB, err := org.Organize(context.Background(), dupBatchCmd(moverMatch(), tracker, false, false))
		require.NoError(t, err, "the mover must not die on a ghost resident's claim")
		require.NotNil(t, resB)
		assert.True(t, resB.Moved)
		assert.False(t, resB.DuplicateSkipped)
		assert.Empty(t, resB.Warnings)

		moved, readErr := afero.ReadFile(fs, filepath.FromSlash(residentTarget))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), moved, "the mover lands its bytes on the freed destination")
		_, statErr := fs.Stat("/in/B.mkv")
		assert.True(t, os.IsNotExist(statErr), "the mover's source moved away")
	})

	t.Run("force mode: a resident gone at priming never parks, so the mover is not winner-skipped", func(t *testing.T) {
		org, fs := residentFixture(t)
		// The resident is gone BEFORE priming: the fixed gate (verify before
		// parking) registers nothing for it, so only the mover primes.
		require.NoError(t, fs.Remove(residentTarget))
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/B.mkv", TargetPath: residentTarget, WillMove: true},
		})

		resB, err := org.Organize(context.Background(), dupBatchCmd(moverMatch(), tracker, true, false))
		require.NoError(t, err)
		require.NotNil(t, resB)
		assert.True(t, resB.Moved, "the mover wins the free key outright")
		assert.False(t, resB.DuplicateSkipped, "no authorized ghost-skip fires behind an absent resident")
		assert.Empty(t, resB.Warnings, "no duplicate warning — there was never a claim")

		moved, readErr := afero.ReadFile(fs, filepath.FromSlash(residentTarget))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), moved)
	})

	t.Run("resident present and verified: every byte of the current behavior holds", func(t *testing.T) {
		org, fs := residentFixture(t)
		tracker := NewDuplicateTracker(false)
		primeResidentFirst(tracker)

		// The resident's own organize is the ordinary no-op and does NOT free
		// its parked claim (settle/release legs only fire on ITS failures).
		resA, err := org.Organize(context.Background(), dupBatchCmd(residentMatch(), tracker, false, false))
		require.NoError(t, err)
		assert.False(t, resA.Moved)
		settledParkedEntry(t, tracker, residentTarget)

		// The mover still takes the resident-claimed duplicate verdict…
		_, err = org.Organize(context.Background(), dupBatchCmd(moverMatch(), tracker, false, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "organization validation failed")
		content, readErr := afero.ReadFile(fs, filepath.FromSlash(residentTarget))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content, "the resident's bytes stay put")
		moverSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), moverSrc, "the rejected mover's source stays in place")
	})
}
