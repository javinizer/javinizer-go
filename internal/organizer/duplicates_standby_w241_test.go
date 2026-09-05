package organizer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// claimQueueState snapshots the live entry for target's canonical key —
// owner plus the ordered standby and waiter queues — so promotion-order
// tests assert the tracker's bookkeeping directly under mu.
func claimQueueState(t *testing.T, tracker *DuplicateTracker, target string) (owner string, standby, waiters []string, present bool) {
	t.Helper()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	entry, ok := tracker.claims[tracker.keyLocked(target)]
	if !ok {
		return "", nil, nil, false
	}
	return entry.claim.source,
		append([]string(nil), entry.standby...),
		append([]string(nil), entry.waiters...), true
}

// waitForClaimOwner polls until the canonical key of target is owned by src,
// so promotion assertions proceed only once the hand-off provably landed.
func waitForClaimOwner(t *testing.T, tracker *DuplicateTracker, target, src string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		owner, _, _, ok := claimQueueState(t, tracker, target)
		if ok && filepath.Clean(owner) == filepath.Clean(src) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("claim for %s never promoted to %s", target, src)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestDuplicateTracker_StandbyPromotion pins codex P2 (PR #241, F1): every
// primed claimant beyond the owner is retained as an ORDERED standby, and an
// owner failing before the other workers reach observe promotes the
// sorted-next primed claimant — never an ad-hoc racer, never a deleted key.
func TestDuplicateTracker_StandbyPromotion(t *testing.T) {
	bg := context.Background()
	const key = "/dest/lib/x.mkv"

	t.Run("owner fails before any sibling observes: sorted-next promotes", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/C.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/D.mkv", TargetPath: key, WillMove: true},
		})
		_, dup := tracker.observe(bg, dupPlanFor("/in/A.mkv", key))
		require.False(t, dup)
		owner, standby, waiters, present := claimQueueState(t, tracker, key)
		require.True(t, present)
		assert.Equal(t, "/in/A.mkv", owner)
		assert.Equal(t, []string{"/in/B.mkv", "/in/C.mkv", "/in/D.mkv"}, standby,
			"every primed claimant beyond the owner is retained in priming (= sorted) order")
		assert.Empty(t, waiters)

		// The owner fails before B, C, or D ever reach observe — the claim is
		// NOT deleted; the sorted-next standby owns it immediately.
		tracker.release(dupPlanFor("/in/A.mkv", key))
		owner, standby, waiters, present = claimQueueState(t, tracker, key)
		require.True(t, present, "the claim survives the early owner failure (no waiter race)")
		assert.Equal(t, "/in/B.mkv", owner, "the sorted-next primed claimant promotes without having observed")
		assert.Equal(t, []string{"/in/C.mkv", "/in/D.mkv"}, standby)
		assert.Empty(t, waiters)

		prior, dup := tracker.observe(bg, dupPlanFor("/in/B.mkv", key))
		require.False(t, dup, "the promoted standby's observation falls through as owner")

		type outcome struct {
			prior duplicateClaim
			dup   bool
		}
		cDone := make(chan outcome, 1)
		go func() {
			prior, dup := tracker.observe(bg, dupPlanFor("/in/C.mkv", key))
			cDone <- outcome{prior, dup}
		}()
		waitForWaiter(t, tracker, key, "/in/C.mkv")

		settleClaim(tracker, "/in/B.mkv", key)
		out := <-cDone
		require.True(t, out.dup)
		assert.Equal(t, "/in/B.mkv", out.prior.source, "the carried waiter resolves against the promoted owner")

		prior, dup = tracker.observe(bg, dupPlanFor("/in/D.mkv", key))
		require.True(t, dup)
		assert.Equal(t, "/in/B.mkv", prior.source, "the settled chain keeps the deterministic verdict")
	})

	t.Run("standby promotion evicts the promoted claimant's waiter slot across spellings", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		// The standby claimant is primed with a NON-CANONICAL spelling. Every
		// other tracker identity decision clean-compares both spellings; on
		// Windows Clean("/in/B.mkv") is `\in\B.mkv`, so a one-sided clean in
		// the promotion path misses there — interior redundant separators
		// surface the identical divergence on every platform.
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in//B.mkv", TargetPath: key, WillMove: true},
		})
		_, dup := tracker.observe(bg, dupPlanFor("/in/A.mkv", key))
		require.False(t, dup)

		// B observes (canonical spelling) while A still owns mid-flight: it
		// parks as a waiter while still holding its standby slot.
		bDone := make(chan bool, 1)
		go func() {
			_, dup := tracker.observe(bg, dupPlanFor("/in/B.mkv", key))
			bDone <- dup
		}()
		waitForWaiter(t, tracker, key, "/in/B.mkv")

		tracker.release(dupPlanFor("/in/A.mkv", key))
		require.False(t, <-bDone, "the promoted standby's observation falls through as owner")
		owner, standby, waiters, present := claimQueueState(t, tracker, key)
		require.True(t, present)
		assert.Equal(t, "/in//B.mkv", owner, "promotion keeps the primed claim spelling")
		assert.Empty(t, standby)
		assert.Empty(t, waiters,
			"the promoted claimant vacates its own waiter slot — a stale slot re-promotes the corpse")

		// The promoted owner's terminal failure frees the key outright: with
		// its waiter slot evicted at promotion nobody is left to (re-)promote,
		// so no never-closing done can park every later observer.
		tracker.release(dupPlanFor("/in/B.mkv", key))
		_, _, _, present = claimQueueState(t, tracker, key)
		assert.False(t, present,
			"no corpse re-promotion: the failed owner's key frees outright")

		ctxC, cancelC := context.WithCancel(bg)
		defer cancelC()
		_, dup = tracker.observe(ctxC, dupPlanFor("/in/C.mkv", key))
		assert.False(t, dup, "a later claimant registers first-come on the freed key")
		settleClaim(tracker, "/in/C.mkv", key)
	})

	t.Run("ad-hoc waiter never outraces the unstarted primed standby", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: key, WillMove: true},
		})
		_, dup := tracker.observe(bg, dupPlanFor("/in/A.mkv", key))
		require.False(t, dup)

		// C is NOT primed: an ad-hoc racer that reaches observe (and blocks)
		// while B has not started yet.
		type outcome struct {
			prior duplicateClaim
			dup   bool
		}
		cDone := make(chan outcome, 1)
		go func() {
			prior, dup := tracker.observe(bg, dupPlanFor("/in/C.mkv", key))
			cDone <- outcome{prior, dup}
		}()
		waitForWaiter(t, tracker, key, "/in/C.mkv")

		tracker.release(dupPlanFor("/in/A.mkv", key))
		owner, standby, waiters, present := claimQueueState(t, tracker, key)
		require.True(t, present)
		assert.Equal(t, "/in/B.mkv", owner,
			"determinism: the ordered standby wins over ad-hoc racers even when the racer waited longer")
		assert.Empty(t, standby)
		assert.Equal(t, []string{"/in/C.mkv"}, waiters, "the racer carries forward behind the promoted standby")

		_, dup = tracker.observe(bg, dupPlanFor("/in/B.mkv", key))
		require.False(t, dup)
		settleClaim(tracker, "/in/B.mkv", key)
		out := <-cDone
		require.True(t, out.dup)
		assert.Equal(t, "/in/B.mkv", out.prior.source)
	})

	t.Run("re-primings register once and a drained chain frees the key", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/A.mkv", TargetPath: key, WillMove: true}, // owner re-priming: no self-standby
			{SourcePath: "/in/B.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: key, WillMove: true}, // duplicate standby: deduped
		})
		owner, standby, _, present := claimQueueState(t, tracker, key)
		require.True(t, present)
		assert.Equal(t, "/in/A.mkv", owner)
		assert.Equal(t, []string{"/in/B.mkv"}, standby, "re-primings never grow the standby queue")

		tracker.release(dupPlanFor("/in/A.mkv", key))
		waitForClaimOwner(t, tracker, key, "/in/B.mkv")
		tracker.release(dupPlanFor("/in/B.mkv", key))
		_, _, _, present = claimQueueState(t, tracker, key)
		assert.False(t, present, "with the standby drained and no waiters the key frees outright")

		_, dup := tracker.observe(bg, dupPlanFor("/in/C.mkv", key))
		assert.False(t, dup, "the freed key re-registers first-come")
		settleClaim(tracker, "/in/C.mkv", key)
	})

	t.Run("drained standby falls back to sorted waiters (unprimed first-come preserved)", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		_, dup := tracker.observe(bg, dupPlanFor("/in/A.mkv", key))
		require.False(t, dup)

		type outcome struct {
			src   string
			prior duplicateClaim
			dup   bool
		}
		results := make(chan outcome, 2)
		for _, src := range []string{"/in/B.mkv", "/in/C.mkv"} {
			go func(src string) {
				prior, dup := tracker.observe(bg, dupPlanFor(src, key))
				results <- outcome{src: src, prior: prior, dup: dup}
			}(src)
		}
		waitForWaiter(t, tracker, key, "/in/B.mkv")
		waitForWaiter(t, tracker, key, "/in/C.mkv")

		tracker.release(dupPlanFor("/in/A.mkv", key))
		promoted := <-results
		assert.Equal(t, "/in/B.mkv", promoted.src, "with no priming the sorted-first waiter still promotes")
		assert.False(t, promoted.dup)
		settleClaim(tracker, "/in/B.mkv", key)
		last := <-results
		assert.Equal(t, "/in/C.mkv", last.src)
		require.True(t, last.dup)
		assert.Equal(t, "/in/B.mkv", last.prior.source)
	})
}

// dupStandbyFixture builds the three-source collision fixture: A, B, and C
// all plan onto ONE destination file — the shape an early owner failure
// must arbitrate deterministically.
func dupStandbyFixture(t *testing.T) (*Organizer, afero.Fs) {
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
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/C.mkv", []byte("c-bytes"), 0o644))
	return org, fs
}

// TestOrganize_StandbyPromotionDeterministic pins codex P2 (PR #241, F1) end
// to end in both authorization modes: the primed owner fails before the
// other workers reach observe, and the sorted-next PRIMED claimant lands its
// bytes — an interleaved loser observes mid-flight, waits, and resolves to
// the unchanged duplicate verdict against the promoted owner.
func TestOrganize_StandbyPromotionDeterministic(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"
	cmdFor := func(src string, tracker *DuplicateTracker, force bool) OrganizeCmd {
		name := filepath.Base(src)
		return dupBatchCmd(models.FileMatchInfo{
			MovieID: "ABC-123", Path: src, Name: name, Extension: ".mkv",
		}, tracker, force, false)
	}

	for _, force := range []bool{false, true} {
		mode := "normal mode"
		if force {
			mode = "force mode"
		}
		t.Run(mode, func(t *testing.T) {
			org, fs := dupStandbyFixture(t)
			tracker := NewDuplicateTracker(false)
			tracker.PrimeBatch([]DuplicatePriming{
				{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
				{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
				{SourcePath: "/in/C.mkv", TargetPath: target, WillMove: true},
			})
			require.NoError(t, fs.Remove("/in/A.mkv"),
				"the primed owner's source vanishes before any worker applies")

			// The owner fails FIRST — before B or C start observing. The
			// ordered standby, not scheduler timing, picks the next owner.
			_, err := org.Organize(context.Background(), cmdFor("/in/A.mkv", tracker, force))
			require.Error(t, err)
			waitForClaimOwner(t, tracker, target, "/in/B.mkv")

			// C reaches observe while B has not started organizing, blocks on
			// the promoted owner's mid-flight claim, and must keep its
			// duplicate verdict once B settles.
			type applyOutcome struct {
				res *OrganizeResult
				err error
			}
			cDone := make(chan applyOutcome, 1)
			go func() {
				res, err := org.Organize(context.Background(), cmdFor("/in/C.mkv", tracker, force))
				cDone <- applyOutcome{res, err}
			}()
			waitForWaiter(t, tracker, target, "/in/C.mkv")

			resB, err := org.Organize(context.Background(), cmdFor("/in/B.mkv", tracker, force))
			require.NoError(t, err, "the sorted-next primed claimant organizes as owner")
			require.True(t, resB.Moved)

			outC := <-cDone
			if force {
				require.NoError(t, outC.err)
				assert.False(t, outC.res.Moved)
				assert.True(t, outC.res.DuplicateSkipped)
				require.Len(t, outC.res.Warnings, 1)
				assert.Contains(t, outC.res.Warnings[0], "already claimed by /in/B.mkv")
			} else {
				require.Error(t, outC.err, "the interleaved loser keeps its duplicate-conflict verdict")
				assert.Contains(t, filepath.ToSlash(outC.err.Error()), target)
			}

			content, readErr := afero.ReadFile(fs, filepath.FromSlash(target))
			require.NoError(t, readErr)
			assert.Equal(t, []byte("b-bytes"), content,
				"deterministic fallback: the sorted-next primed claimant's bytes land")

			var videos []string
			require.NoError(t, afero.Walk(fs, "/dest", func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.HasSuffix(path, ".mkv") {
					videos = append(videos, filepath.ToSlash(path))
				}
				return nil
			}))
			assert.Equal(t, []string{target}, videos, "exactly one video lands, from the promoted standby")
			loserSrc, readErr := afero.ReadFile(fs, "/in/C.mkv")
			require.NoError(t, readErr)
			assert.Equal(t, []byte("c-bytes"), loserSrc, "the losing claimant's source is untouched")
		})
	}
}

// TestDuplicateTracker_WaitHonorsCancellation pins codex P2 (PR #241, F2):
// the terminal wait selects on the claimant's context — a mid-wait cancel
// aborts the wait, and the cancelled claimant LEAVES the claim queues
// (waiter AND standby slots), so it can never be promoted into
// post-cancellation execution and the bookkeeping never leaks.
func TestDuplicateTracker_WaitHonorsCancellation(t *testing.T) {
	const key = "/dest/lib/x.mkv"

	t.Run("cancelled mid-wait leaves the queue and the chain continues", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/C.mkv", TargetPath: key, WillMove: true},
		})
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", key))
		require.False(t, dup)

		ctxB, cancelB := context.WithCancel(context.Background())
		type outcome struct {
			prior duplicateClaim
			dup   bool
		}
		bDone := make(chan outcome, 1)
		go func() {
			prior, dup := tracker.observe(ctxB, dupPlanFor("/in/B.mkv", key))
			bDone <- outcome{prior, dup}
		}()
		waitForWaiter(t, tracker, key, "/in/B.mkv")
		owner, standby, _, _ := claimQueueState(t, tracker, key)
		assert.Equal(t, []string{"/in/B.mkv", "/in/C.mkv"}, standby, "B waits while still holding its standby slot")
		assert.Equal(t, "/in/A.mkv", owner)

		cancelB()
		out := <-bDone
		assert.False(t, out.dup, "the cancelled waiter's observe aborts instead of blocking forever")
		assert.Empty(t, out.prior.source)

		owner, standby, waiters, present := claimQueueState(t, tracker, key)
		require.True(t, present)
		assert.Equal(t, "/in/A.mkv", owner)
		assert.Empty(t, waiters, "the cancelled claimaint vacated its waiter slot")
		assert.Equal(t, []string{"/in/C.mkv"}, standby, "the cancelled claimant vacated its standby slot")

		// The owner then fails: the cancelled B must NOT promote — C is next.
		tracker.release(dupPlanFor("/in/A.mkv", key))
		owner, _, _, _ = claimQueueState(t, tracker, key)
		assert.Equal(t, "/in/C.mkv", owner, "the chain skips the cancelled claimant entirely")

		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", key))
		assert.False(t, dup)
		settleClaim(tracker, "/in/C.mkv", key)
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/D.mkv", key))
		require.True(t, dup)
		assert.Equal(t, "/in/C.mkv", prior.source, "verdicts keep flowing through the cleaned queue")
	})

	t.Run("cancellation racing a landed promotion never leaks the claim", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: key, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: key, WillMove: true},
		})
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", key))
		require.False(t, dup)

		ctxB, cancelB := context.WithCancel(context.Background())
		bDone := make(chan bool, 1)
		go func() {
			_, dup := tracker.observe(ctxB, dupPlanFor("/in/B.mkv", key))
			bDone <- dup
		}()
		waitForWaiter(t, tracker, key, "/in/B.mkv")

		// The owner fails — B promotes — and B's context dies while the
		// promotion is landing. Whichever select leg the waiter wake takes,
		// the bookkeeping converges: B owns the open claim (its cancellation
		// scrub never touches an owned entry; the worker boundary's release
		// hands the key onward) and claims never grow or vanish silently.
		tracker.release(dupPlanFor("/in/A.mkv", key))
		waitForClaimOwner(t, tracker, key, "/in/B.mkv")
		cancelB()
		assert.False(t, <-bDone, "no-duplicate either way: owner of the key or cleaned-out waiter")
		owner, standby, waiters, present := claimQueueState(t, tracker, key)
		require.True(t, present)
		assert.Equal(t, "/in/B.mkv", owner)
		assert.Empty(t, standby)
		assert.Empty(t, waiters)

		// The worker-boundary close-out for the dead owner hands onward.
		tracker.release(dupPlanFor("/in/B.mkv", key))
		_, _, _, present = claimQueueState(t, tracker, key)
		assert.False(t, present, "the key frees outright once the dead owner is closed out")
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", key))
		assert.False(t, dup)
		settleClaim(tracker, "/in/C.mkv", key)
	})

	t.Run("cancelWaiterLocked tolerates unknown keys and empty queues", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.cancelWaiterLocked("/dest/lib/absent.mkv", "/in/x.mkv")

		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", key))
		require.False(t, dup)
		tracker.cancelWaiterLocked(tracker.keyLocked(key), "/in/nobody.mkv")
		owner, standby, waiters, present := claimQueueState(t, tracker, key)
		require.True(t, present)
		assert.Equal(t, "/in/A.mkv", owner, "a scrub that matches nothing disturbs nothing")
		assert.Empty(t, standby)
		assert.Empty(t, waiters)
		settleClaim(tracker, "/in/A.mkv", key)
	})
}

// TestOrganize_WaitCancellation pins codex P2 (PR #241, F2) through the
// organizer: mid-wait cancellation aborts the claimant without any
// filesystem work, and a promoted claimant whose context is dead performs no
// mutation either — the recheck/entry guards turn both into the context
// error while the claim chain drains onward.
func TestOrganize_WaitCancellation(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"
	cmdFor := func(src string, tracker *DuplicateTracker, force bool) OrganizeCmd {
		name := filepath.Base(src)
		return dupBatchCmd(models.FileMatchInfo{
			MovieID: "ABC-123", Path: src, Name: name, Extension: ".mkv",
		}, tracker, force, false)
	}

	t.Run("cancelled mid-wait: no execution, queue cleaned, batch continues", func(t *testing.T) {
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
		})

		// B's observe parks on A's primed (mid-flight, unsettled) claim.
		ctxB, cancelB := context.WithCancel(context.Background())
		type applyOutcome struct {
			res *OrganizeResult
			err error
		}
		bDone := make(chan applyOutcome, 1)
		go func() {
			res, err := org.Organize(ctxB, cmdFor("/in/B.mkv", tracker, false))
			bDone <- applyOutcome{res, err}
		}()
		waitForWaiter(t, tracker, target, "/in/B.mkv")

		cancelB()
		out := <-bDone
		require.ErrorIs(t, out.err, context.Canceled,
			"a waiter cancelled mid-wait surfaces the context error, not a duplicate verdict")
		assert.Nil(t, out.res)
		bSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), bSrc, "the cancelled claimant executed nothing")
		destExists, statErr := afero.Exists(fs, filepath.FromSlash(target))
		require.NoError(t, statErr)
		assert.False(t, destExists, "no filesystem mutation happened on the cancelled claimant's behalf")

		owner, standby, waiters, present := claimQueueState(t, tracker, target)
		require.True(t, present)
		assert.Equal(t, "/in/A.mkv", owner, "the mid-flight owner is untouched")
		assert.Empty(t, standby, "the cancelled claimant vacated its standby slot")
		assert.Empty(t, waiters, "the cancelled claimant vacated its waiter slot")

		// The healthy owner's organize proceeds undisturbed afterwards.
		resA, err := org.Organize(context.Background(), cmdFor("/in/A.mkv", tracker, false))
		require.NoError(t, err)
		require.True(t, resA.Moved)
		content, readErr := afero.ReadFile(fs, filepath.FromSlash(target))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content,
			"the batch continues normally after the cancelled waiter's exit")
	})

	for _, force := range []bool{false, true} {
		mode := "normal mode"
		if force {
			mode = "force mode"
		}
		t.Run("promoted then cancelled: no filesystem mutation, hand-off continues ("+mode+")", func(t *testing.T) {
			org, fs := dupStandbyFixture(t)
			tracker := NewDuplicateTracker(false)
			tracker.PrimeBatch([]DuplicatePriming{
				{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
				{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
				{SourcePath: "/in/C.mkv", TargetPath: target, WillMove: true},
			})
			require.NoError(t, fs.Remove("/in/A.mkv"))

			// The sorted owner fails and the standby promotes B before B's
			// worker ever starts.
			_, err := org.Organize(context.Background(), cmdFor("/in/A.mkv", tracker, force))
			require.Error(t, err)
			waitForClaimOwner(t, tracker, target, "/in/B.mkv")

			// B arrives with an already-dead context (deadline/batch cancel
			// landed while the promotion did): the cancellation guards turn
			// it away with the context error before any filesystem work.
			ctxB, cancelB := context.WithCancel(context.Background())
			cancelB()
			resB, err := org.Organize(ctxB, cmdFor("/in/B.mkv", tracker, force))
			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, resB)
			destExists, statErr := afero.Exists(fs, filepath.FromSlash(target))
			require.NoError(t, statErr)
			assert.False(t, destExists, "a cancelled promoted claimant mutates nothing")
			bSrc, readErr := afero.ReadFile(fs, "/in/B.mkv")
			require.NoError(t, readErr)
			assert.Equal(t, []byte("b-bytes"), bSrc, "the cancelled claimant's source is untouched")

			// The worker boundary closes out the dead owner and the chain
			// drains to C, whose bytes land instead of blocking behind B.
			tracker.ReleaseAbandonedBy("/in/B.mkv")
			waitForClaimOwner(t, tracker, target, "/in/C.mkv")
			resC, err := org.Organize(context.Background(), cmdFor("/in/C.mkv", tracker, force))
			require.NoError(t, err)
			require.True(t, resC.Moved)
			content, readErr := afero.ReadFile(fs, filepath.FromSlash(target))
			require.NoError(t, readErr)
			assert.Equal(t, []byte("c-bytes"), content, "the key settles on the next live claimant")
		})
	}
}
