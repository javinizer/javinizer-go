package organizer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// TestDuplicateTracker_TerminalWaiting pins the #241 P2 terminal gate: an
// observer of an OWNED key waits for the owner's terminal outcome — a
// settled owner keeps the key (the duplicate verdict arrives unchanged),
// while a released owner hands the key to its sorted-first waiter.
func TestDuplicateTracker_TerminalWaiting(t *testing.T) {
	t.Run("observer blocks until the settled owner keeps its key", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		require.False(t, dup)

		type outcome struct {
			prior duplicateClaim
			dup   bool
		}
		results := make(chan outcome, 1)
		go func() {
			prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
			results <- outcome{prior, dup}
		}()
		waitForWaiter(t, tracker, "/dest/lib/x.mkv", "/in/B.mkv")
		select {
		case out := <-results:
			t.Fatalf("the waiter resolved before any terminal outcome: %+v", out)
		default:
		}

		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		out := <-results
		require.True(t, out.dup, "owner success leaves the waiter's duplicate verdict unchanged")
		assert.Equal(t, "/in/A.mkv", out.prior.source)
	})

	t.Run("settle tolerates nils, register-nothing plans, non-owners, and repetition", func(t *testing.T) {
		forceCasePosture(t, true)
		var nilTracker *DuplicateTracker
		nilTracker.settle(nil)
		nilTracker.settle(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))

		tracker := NewDuplicateTracker(false)
		tracker.settle(nil)
		tracker.settle(&OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/in/A.mkv", WillMove: false})
		tracker.settle(dupPlanFor("/in/A.mkv", ""))
		tracker.settle(dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))

		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		require.False(t, dup)
		tracker.settle(dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")

		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup, "the settled owner keeps its key through every tolerated no-op settle")
		assert.Equal(t, "/in/A.mkv", prior.source)
	})

	t.Run("ReleaseAbandonedBy frees only open claims of the abandoned source", func(t *testing.T) {
		forceCasePosture(t, true)
		var nilTracker *DuplicateTracker
		nilTracker.ReleaseAbandonedBy("/in/A.mkv")

		tracker := NewDuplicateTracker(false)
		tracker.ReleaseAbandonedBy("")
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		require.False(t, dup)
		tracker.ReleaseAbandonedBy("/in/foreign.mkv")

		type outcome struct {
			prior duplicateClaim
			dup   bool
		}
		results := make(chan outcome, 1)
		go func() {
			prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
			results <- outcome{prior, dup}
		}()
		waitForWaiter(t, tracker, "/dest/lib/x.mkv", "/in/B.mkv")
		select {
		case out := <-results:
			t.Fatalf("the foreign abandon disturbed the waiter's owner: %+v", out)
		default:
		}

		tracker.ReleaseAbandonedBy("/in/A.mkv")
		promoted := <-results
		assert.False(t, promoted.dup, "the abandoned owner's open claim hands the key to its waiter")

		settleClaim(tracker, "/in/B.mkv", "/dest/lib/x.mkv")
		tracker.ReleaseAbandonedBy("/in/B.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup, "a settled claim is final — the abandon safety net never touches it")
		assert.Equal(t, "/in/B.mkv", prior.source)
	})
}

// panicOnceDestFs panics on the FIRST MkdirAll under /dest — a deterministic
// mid-execute owner panic (#241 P2): the organizer's deferred close-out must
// release the canonical key (waiters promote) and re-panic. The match is
// separator-agnostic: produced paths arrive native-spelled ("\dest\..." from
// filepath.Join on Windows), so the trap compares the slash-normalized form.
type panicOnceDestFs struct {
	afero.Fs
	armed *atomic.Bool
}

func (p *panicOnceDestFs) MkdirAll(path string, perm os.FileMode) error {
	if strings.HasPrefix(filepath.ToSlash(path), "/dest") && p.armed.CompareAndSwap(true, false) {
		panic("mkdir boom")
	}
	return p.Fs.MkdirAll(path, perm)
}

// TestOrganize_OwnerPanicReleasesWaiters pins the #241 P2 no-deadlock
// guarantee end to end: a primed owner panicking mid-execute releases its
// canonical key through the organizer's deferred close-out, so the blocked
// waiter promotes and ITS organize proceeds — in both authorization modes.
func TestOrganize_OwnerPanicReleasesWaiters(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"

	for _, force := range []bool{false, true} {
		mode := "normal mode"
		if force {
			mode = "force mode"
		}
		t.Run(mode, func(t *testing.T) {
			baseFS := afero.NewMemMapFs()
			require.NoError(t, baseFS.MkdirAll("/in", 0o755))
			require.NoError(t, afero.WriteFile(baseFS, "/in/A.mkv", []byte("a-bytes"), 0o644))
			require.NoError(t, afero.WriteFile(baseFS, "/in/B.mkv", []byte("b-bytes"), 0o644))
			armed := &atomic.Bool{}
			armed.Store(true)
			fsys := &panicOnceDestFs{Fs: baseFS, armed: armed}
			org := NewOrganizer(fsys, &Config{
				FolderFormat:  "<ID>",
				FileFormat:    "<ID>",
				RenameFile:    true,
				OperationMode: operationmode.OperationModeOrganize,
			}, nil, nil)
			tracker := NewDuplicateTracker(false)
			tracker.PrimeBatch([]DuplicatePriming{
				{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
				{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
			})
			cmdB := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, force, false)

			type applyOutcome struct {
				res *OrganizeResult
				err error
			}
			bDone := make(chan applyOutcome, 1)
			go func() {
				res, err := org.Organize(context.Background(), cmdB)
				bDone <- applyOutcome{res, err}
			}()
			// The waiter is provably blocked before the owner's panic lands.
			waitForWaiter(t, tracker, target, "/in/B.mkv")

			aPanicked := make(chan any, 1)
			go func() {
				defer func() { aPanicked <- recover() }()
				_, _ = org.Organize(context.Background(), dupBatchCmd(
					models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, force, false))
			}()
			r := <-aPanicked
			require.NotNil(t, r, "the owner panic re-panics after the deferred claim close-out")

			outB := <-bDone
			require.NoError(t, outB.err, "the promoted waiter's organize succeeds — no deadlock behind the dead owner")
			assert.True(t, outB.res.Moved)
			assert.Empty(t, outB.res.Warnings)
			content, err := afero.ReadFile(baseFS, filepath.FromSlash(target))
			require.NoError(t, err)
			assert.Equal(t, []byte("b-bytes"), content, "the promoted claimant landed its bytes")
			loserSrc, err := afero.ReadFile(baseFS, "/in/A.mkv")
			require.NoError(t, err)
			assert.Equal(t, []byte("a-bytes"), loserSrc, "the panicked owner's source is untouched")
		})
	}
}

// TestOrganize_WinnerSuccessLoserStillConflicts pins the #241 P2 unchanged
// terminal-success verdict through a concurrent organize: the blocked loser's
// duplicate conflict still arrives once the winner settles, in both
// authorization modes.
func TestOrganize_WinnerSuccessLoserStillConflicts(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"

	t.Run("normal mode", func(t *testing.T) {
		org, _ := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
		})
		bDone := make(chan error, 1)
		go func() {
			_, err := org.Organize(context.Background(), dupBatchCmd(
				models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, false, false))
			bDone <- err
		}()
		waitForWaiter(t, tracker, target, "/in/B.mkv")
		resA, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, false))
		require.NoError(t, err)
		require.True(t, resA.Moved)

		loserErr := <-bDone
		require.Error(t, loserErr, "the blocked loser's duplicate conflict resolves after the winner settles")
		assert.Contains(t, filepath.ToSlash(loserErr.Error()), target)
	})

	t.Run("force mode", func(t *testing.T) {
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
		})
		type applyOutcome struct {
			res *OrganizeResult
			err error
		}
		bDone := make(chan applyOutcome, 1)
		go func() {
			res, err := org.Organize(context.Background(), dupBatchCmd(
				models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, true, false))
			bDone <- applyOutcome{res, err}
		}()
		waitForWaiter(t, tracker, target, "/in/B.mkv")
		resA, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, true, false))
		require.NoError(t, err)
		require.True(t, resA.Moved)

		outB := <-bDone
		require.NoError(t, outB.err, "the blocked loser resolves to the unchanged authorized-skip verdict")
		assert.False(t, outB.res.Moved)
		assert.True(t, outB.res.DuplicateSkipped)
		require.Len(t, outB.res.Warnings, 1)
		content, readErr := afero.ReadFile(fs, filepath.FromSlash(target))
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content, "only the winner's bytes land")
	})
}
