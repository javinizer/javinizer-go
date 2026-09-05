package organizer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func forceCasePosture(t *testing.T, sensitive bool) {
	t.Helper()
	prev := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return sensitive, nil }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = prev
		fsutil.ResetCaseSensitivityCache()
	})
}

// resetProbeCaches clears BOTH process-wide probe caches so non-probing key
// derivations observe a fully uncached posture; cleanup restores a clean
// slate for later tests.
func resetProbeCaches(t *testing.T) {
	t.Helper()
	fsutil.ResetCaseSensitivityCache()
	fsutil.ResetNormalizationCache()
	t.Cleanup(func() {
		fsutil.ResetCaseSensitivityCache()
		fsutil.ResetNormalizationCache()
	})
}

func dupPlanFor(src, target string) *OrganizePlan {
	return &OrganizePlan{SourcePath: src, TargetPath: target, WillMove: true}
}

// settleClaim marks src's claim on target terminal-success — the owner half
// of the terminal-gated observation contract (#241 P2): observers of an
// owned key WAIT for the owner's terminal outcome, so a grouping assertion
// against an owned key settles its owner first.
func settleClaim(tracker *DuplicateTracker, src, target string) {
	tracker.settle(dupPlanFor(src, target))
}

// waitForWaiter polls until src is registered as a blocked waiter on the
// canonical key of target, so concurrent-observation pins proceed only once
// the waiter is provably inside observe's terminal wait.
func waitForWaiter(t *testing.T, tracker *DuplicateTracker, target, src string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		tracker.mu.Lock()
		entry, ok := tracker.claims[tracker.keyLocked(target)]
		found := false
		if ok && !entry.settled {
			for _, w := range entry.waiters {
				if w == src {
					found = true
					break
				}
			}
		}
		tracker.mu.Unlock()
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiter %s never registered on %s", src, target)
		}
		time.Sleep(time.Millisecond)
	}
}

// statFaultFs fails every Stat with a non-IsNotExist error, exercising the
// indeterminate-error branch of PlanSourceExists.
type statFaultFs struct {
	afero.Fs
	err error
}

func (f *statFaultFs) Stat(string) (os.FileInfo, error) { return nil, f.err }

func TestDuplicateTracker_Grouping(t *testing.T) {
	t.Run("identical target distinct sources", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "first claim registers cleanly")
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/A.mkv", prior.source)
	})

	t.Run("casefold variants group when root proven insensitive", func(t *testing.T) {
		forceCasePosture(t, false)
		tracker := NewDuplicateTracker(false)
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/Movie.mkv"))
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/Movie.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/movie.mkv"))
		require.True(t, dup, "case variants of one name are proven-equal on insensitive roots")
		assert.Equal(t, "/in/A.mkv", prior.source)
	})

	t.Run("case variants stay distinct when root proven sensitive", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/Movie.mkv"))
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/movie.mkv"))
		assert.False(t, dup, "never proven equal, so byte-distinct names keep distinct keys")
	})

	t.Run("lexical spelling variants of one path group", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/./x.mkv"))
		assert.True(t, dup, "cleaned spellings of one destination are the same canonical key")
	})

	t.Run("multipart suffixes stay distinct", func(t *testing.T) {
		forceCasePosture(t, false)
		tracker := NewDuplicateTracker(false)
		tracker.observe(context.Background(), dupPlanFor("/in/m-cd1.mkv", "/dest/lib/m-cd1.mkv"))
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/m-cd2.mkv", "/dest/lib/m-cd2.mkv"))
		assert.False(t, dup, "cdN part suffixes name distinct destinations")
	})

	t.Run("same source re-observe is idempotent", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "retries and dry-run re-plans of one file never self-conflict")
	})

	t.Run("third duplicate reports the first claim", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/A.mkv", prior.source, "first claim wins — mirrors first-publish-wins at execute")
	})

	t.Run("plans that move nothing are never registered", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		still := &OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/in/A.mkv", WillMove: false}
		_, dup := tracker.observe(context.Background(), still)
		assert.False(t, dup)
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/in/A.mkv"))
		assert.False(t, dup, "WillMove=false targets are owned by destination-occupation checks")
	})

	t.Run("empty and nil plans are never registered", func(t *testing.T) {
		forceCasePosture(t, true)
		var nilTracker *DuplicateTracker
		_, dup := nilTracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "nil tracker disables detection")
		tracker := NewDuplicateTracker(false)
		_, dup = tracker.observe(context.Background(), nil)
		assert.False(t, dup)
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", ""))
		assert.False(t, dup)
	})

	t.Run("unprimed observes keep first-come registration", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		// No PrimeBatch at all: the first observer owns the key (single-file
		// and unprimed callers), deterministic batches never take this leg.
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup)
		settleClaim(tracker, "/in/B.mkv", "/dest/lib/x.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/B.mkv", prior.source, "unprimed = first-come, documenting the legacy fallback")
	})

	t.Run("concurrent observations are race-free and total", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		var wg sync.WaitGroup
		var mu sync.Mutex
		claimsPerKey := map[string]int{}
		for i := 0; i < 64; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("/dest/lib/movie-%d.mkv", i%8)
				plan := dupPlanFor(fmt.Sprintf("/in/src-%d.mkv", i), key)
				_, dup := tracker.observe(context.Background(), plan)
				if !dup {
					mu.Lock()
					claimsPerKey[key]++
					mu.Unlock()
					// Terminal-success close-out: waiters on this key wake to
					// their duplicate verdict instead of blocking forever.
					tracker.settle(plan)
				}
			}(i)
		}
		wg.Wait()
		for key, claims := range claimsPerKey {
			assert.Equal(t, 1, claims, "%s must have exactly one winning claim", key)
		}
	})
}

func TestApplyDuplicatePreflight(t *testing.T) {
	forceCasePosture(t, true)
	tracker := NewDuplicateTracker(false)
	tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
	settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")

	t.Run("unauthorized duplicate joins plan conflicts", func(t *testing.T) {
		plan := dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv")
		warnings, skip := applyDuplicatePreflight(context.Background(), plan, tracker, false)
		assert.Nil(t, warnings)
		assert.False(t, skip)
		require.Len(t, plan.Conflicts, 1)
		assert.Equal(t, ConflictDuplicate, plan.Conflicts[0].Kind)
		assert.Equal(t, "/dest/lib/x.mkv", plan.Conflicts[0].Path)
		assert.Equal(t, "duplicate", plan.Conflicts[0].kindName())
	})

	t.Run("authorized duplicate demotes to warning and skips execution", func(t *testing.T) {
		plan := dupPlanFor("/in/C.mkv", "/dest/lib/x.mkv")
		warnings, skip := applyDuplicatePreflight(context.Background(), plan, tracker, true)
		assert.Empty(t, plan.Conflicts, "authorization demotes the duplicate out of the conflict pipeline")
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "/dest/lib/x.mkv")
		assert.Contains(t, warnings[0], "/in/A.mkv")
		assert.Contains(t, warnings[0], "overwrite authorized")
		assert.True(t, skip, "#240 finding A: an authorized duplicate never moves bytes — only the winner lands")
	})

	t.Run("no duplicate yields nothing", func(t *testing.T) {
		plan := dupPlanFor("/in/D.mkv", "/dest/lib/y.mkv")
		warnings, skip := applyDuplicatePreflight(context.Background(), plan, tracker, false)
		assert.Nil(t, warnings)
		assert.False(t, skip)
		warnings, skip = applyDuplicatePreflight(context.Background(), plan, nil, false)
		assert.Nil(t, warnings, "nil tracker disables detection")
		assert.False(t, skip)
		assert.Empty(t, plan.Conflicts)
	})
}

// dupBatchFixture builds an organizer whose template maps every source of one
// movie onto ONE destination file — the intra-batch collision shape.
func dupBatchFixture(t *testing.T) (*Organizer, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	require.NoError(t, fs.MkdirAll("/in", 0755))
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0644))
	return org, fs
}

func dupBatchCmd(match models.FileMatchInfo, tracker *DuplicateTracker, force, dryRun bool) OrganizeCmd {
	return OrganizeCmd{
		Match:            match,
		Movie:            &models.Movie{ID: "ABC-123"},
		DestDir:          "/dest",
		MoveFiles:        true,
		ForceUpdate:      force,
		DryRun:           dryRun,
		DuplicateTracker: tracker,
	}
}

func TestOrganize_DryRunDuplicatePreflight_ConflictEquivalence(t *testing.T) {
	forceCasePosture(t, true)
	org, _ := dupBatchFixture(t)
	// Non-probing tracker: the exact policy the apply phase constructs for dry
	// runs (#240 finding B) — key derivation must never touch the filesystem.
	tracker := NewDuplicateTracker(true)
	movie := &models.Movie{ID: "ABC-123"}

	// Case (a): batch duplicate — file B plans onto the destination file A's
	// dry-run already claimed, while nothing exists there on disk yet.
	cmdA := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, true)
	resultA, err := org.Organize(context.Background(), cmdA)
	require.NoError(t, err)
	require.NotNil(t, resultA)

	cmdB := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, false, true)
	_, dupErr := org.Organize(context.Background(), cmdB)
	require.Error(t, dupErr)
	assert.True(t, strings.HasPrefix(dupErr.Error(), "organization validation failed: ["), dupErr.Error())
	assert.Contains(t, dupErr.Error(), resultA.NewPath)

	// Case (b): destination occupation — file B targets a path occupied on
	// disk by an unrelated file at plan time. Both cases must surface through
	// the identical failure pipeline (validatePlan's issue rendering, byte for
	// byte).
	const occupiedErrPrefix = "organization validation failed: ["
	org2, fs2 := dupBatchFixture(t)
	require.NoError(t, fs2.MkdirAll("/dest/ABC-123", 0755))
	require.NoError(t, afero.WriteFile(fs2, "/dest/ABC-123/ABC-123.mkv", []byte("foreign"), 0644))
	_, occErr := org2.Organize(context.Background(), OrganizeCmd{
		Match:     models.FileMatchInfo{MovieID: movie.ID, Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"},
		Movie:     movie,
		DestDir:   "/dest",
		MoveFiles: true,
		DryRun:    true,
	})
	require.Error(t, occErr)
	assert.True(t, strings.HasPrefix(occErr.Error(), occupiedErrPrefix))
	assert.Equal(t, occErr.Error(), dupErr.Error(), "duplicate preflight short-circuits into the identical conflict outcome as destination occupation")
}

func TestOrganize_DryRunAuthorizedDuplicate_WarnsPersistably(t *testing.T) {
	forceCasePosture(t, true)
	org, _ := dupBatchFixture(t)
	tracker := NewDuplicateTracker(true) // dry runs construct the non-probing variant

	cmdA := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, true, true)
	_, err := org.Organize(context.Background(), cmdA)
	require.NoError(t, err)

	cmdB := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, true, true)
	resultB, err := org.Organize(context.Background(), cmdB)
	require.NoError(t, err, "authorized duplicates warn, never block")
	require.Len(t, resultB.Warnings, 1)
	assert.Contains(t, resultB.Warnings[0], "duplicate destination within batch")
	assert.Contains(t, resultB.Warnings[0], "/in/A.mkv")
}

func TestOrganize_LiveDuplicatePreflight(t *testing.T) {
	forceCasePosture(t, true)

	t.Run("unauthorized duplicate fails through the same pipeline", func(t *testing.T) {
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		// First file dry-run claims the destination without moving anything.
		_, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, true))
		require.NoError(t, err)
		// The live second file targets an on-disk VACANT path — pre-#224-E
		// this would succeed silently; the plan-only preflight must stop it.
		_, err = org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, false, false))
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "organization validation failed: ["), err.Error())
		assert.Contains(t, filepath.ToSlash(err.Error()), "/dest/ABC-123/ABC-123.mkv")
		content, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), content, "the losing duplicate's source is untouched")
	})

	t.Run("authorized duplicate warns and never moves bytes", func(t *testing.T) {
		forceCasePosture(t, true)
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		_, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, true))
		require.NoError(t, err)
		result, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, true, false))
		require.NoError(t, err)
		require.Len(t, result.Warnings, 1, "live authorized result carries the persisted-warning payload")
		assert.Contains(t, result.Warnings[0], "overwrite authorized")
		assert.False(t, result.Moved, "#240 finding A: an authorized duplicate skips execution")
		assert.True(t, result.DuplicateSkipped, "codex P1 (PR #241): the skip is the journal no-op marker")
		assert.True(t, result.ShouldGenerateMetadata, "the skip mirrors the dry-run result shape")
		destExists, statErr := afero.Exists(fs, "/dest/ABC-123/ABC-123.mkv")
		require.NoError(t, statErr)
		assert.False(t, destExists, "the loser's bytes never reach a claimed destination")
		assert.Equal(t, "/dest/ABC-123/ABC-123.mkv", filepath.ToSlash(result.NewPath),
			"visible winner/skip semantics preserved: NewPath still names the shared destination for API/CLI history")
		content, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), content, "the skipped duplicate's source is untouched")
	})
}

// TestDuplicateTracker_PrimingDeterministicOwnership pins #240 finding A:
// PrimeBatch pre-assigns each canonical key's winner in sorted order BEFORE
// any worker observes, so goroutine arrival order can no longer pick the
// batch winner — the first sorted item wins its key every time.
func TestDuplicateTracker_PrimingDeterministicOwnership(t *testing.T) {
	for _, order := range [][]string{
		{"/in/A.mkv", "/in/B.mkv", "/in/C.mkv"}, // primed order arrival
		{"/in/C.mkv", "/in/B.mkv", "/in/A.mkv"}, // fully reversed arrival
		{"/in/B.mkv", "/in/C.mkv", "/in/A.mkv"}, // loser interleaved first
	} {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/C.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		// Observations run concurrently in the given arrival order; losers
		// block on the owner's terminal outcome (#241 P2), so the test settles
		// A the moment its (only immediately-completing) observation returns.
		type outcome struct {
			src   string
			prior duplicateClaim
			dup   bool
		}
		outcomes := make(chan outcome, 3)
		var wg sync.WaitGroup
		for _, src := range order {
			wg.Add(1)
			go func(src string) {
				defer wg.Done()
				prior, dup := tracker.observe(context.Background(), dupPlanFor(src, "/dest/lib/x.mkv"))
				outcomes <- outcome{src: src, prior: prior, dup: dup}
			}(src)
		}
		got := make(map[string]outcome, 3)
		for len(got) < 3 {
			o := <-outcomes
			got[o.src] = o
			if o.src == "/in/A.mkv" {
				settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
			}
		}
		wg.Wait()
		for src, o := range got {
			if src == "/in/A.mkv" {
				assert.False(t, o.dup, "the sorted-first item wins even when it observes last (order %v)", order)
			} else {
				require.True(t, o.dup, "primed losers see the winner even when they observe first (order %v)", order)
				assert.Equal(t, "/in/A.mkv", o.prior.source)
			}
		}
		// The settled winner keeps its key: even a fresh spelling of a loser
		// observes the same owner.
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv#retry", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/A.mkv", prior.source)
	}

	t.Run("forced out-of-order concurrent observes keep the primed winner", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/C.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		// Each goroutine waits for its own start gate; main opens the gates in
		// reverse sorted order, so C and B are IN observe (blocked on the
		// owner's terminal outcome, #241 P2) before A starts.
		type outcome struct {
			src   string
			prior duplicateClaim
			dup   bool
		}
		gates := map[string]chan struct{}{}
		results := make(chan outcome, 3)
		for _, src := range []string{"/in/A.mkv", "/in/B.mkv", "/in/C.mkv"} {
			gates[src] = make(chan struct{})
		}
		var wg sync.WaitGroup
		for _, src := range []string{"/in/A.mkv", "/in/B.mkv", "/in/C.mkv"} {
			wg.Add(1)
			go func(src string) {
				defer wg.Done()
				<-gates[src]
				prior, dup := tracker.observe(context.Background(), dupPlanFor(src, "/dest/lib/x.mkv"))
				results <- outcome{src: src, prior: prior, dup: dup}
			}(src)
		}
		for _, src := range []string{"/in/C.mkv", "/in/B.mkv", "/in/A.mkv"} {
			close(gates[src])
		}
		bySrc := map[string]outcome{}
		for len(bySrc) < 3 {
			o := <-results
			bySrc[o.src] = o
			if o.src == "/in/A.mkv" {
				// Only the winner completes before the terminal outcome; its
				// settle then wakes the blocked losers to their verdicts.
				settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
			}
		}
		wg.Wait()
		close(results)
		assert.False(t, bySrc["/in/A.mkv"].dup, "the primed winner never conflicts")
		require.True(t, bySrc["/in/B.mkv"].dup)
		require.True(t, bySrc["/in/C.mkv"].dup)
		assert.Equal(t, "/in/A.mkv", bySrc["/in/B.mkv"].prior.source)
		assert.Equal(t, "/in/A.mkv", bySrc["/in/C.mkv"].prior.source)
	})

	t.Run("a primed run observes exactly one winner per key", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		primings := make([]DuplicatePriming, 0, 64)
		for i := 0; i < 64; i++ {
			primings = append(primings, DuplicatePriming{
				SourcePath: fmt.Sprintf("/in/src-%02d.mkv", i),
				TargetPath: fmt.Sprintf("/dest/lib/movie-%d.mkv", i%8),
				WillMove:   true,
			})
		}
		tracker.PrimeBatch(primings)
		var wg sync.WaitGroup
		var mu sync.Mutex
		winners := map[string][]string{}
		for i := 0; i < 64; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				src := fmt.Sprintf("/in/src-%02d.mkv", i)
				key := fmt.Sprintf("/dest/lib/movie-%d.mkv", i%8)
				plan := dupPlanFor(src, key)
				_, dup := tracker.observe(context.Background(), plan)
				if !dup {
					mu.Lock()
					winners[key] = append(winners[key], src)
					mu.Unlock()
					// Owner terminal-success: blocked waiters of this key wake
					// to their duplicate verdict (#241 P2).
					tracker.settle(plan)
				}
			}(i)
		}
		wg.Wait()
		require.Len(t, winners, 8)
		for key, srcs := range winners {
			require.Len(t, srcs, 1, "%s must have exactly one winner", key)
			// Sorted priming order is src-00..src-63, so key movie-N's first
			// sorted claimant is src-N — the deterministic owner every time.
			d := int(key[len("/dest/lib/movie-")] - '0')
			assert.Equal(t, fmt.Sprintf("/in/src-%02d.mkv", d), srcs[0], "the sorted-first priming owns %s", key)
		}
	})
}

// TestDuplicateTracker_PrimeBatchOncePerRun pins the run-lifecycle contract:
// planning happens exactly once per run — later PrimeBatch calls never
// re-assign ownership.
func TestDuplicateTracker_PrimeBatchOncePerRun(t *testing.T) {
	t.Run("second prime batch is ignored", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/C.mkv", TargetPath: "/dest/lib/y.mkv", WillMove: true},
		})
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/A.mkv", prior.source, "the first batch's owner survives the ignored re-prime")
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/D.mkv", "/dest/lib/y.mkv"))
		assert.False(t, dup, "the ignored batch never registered /dest/lib/y.mkv")
	})

	t.Run("nil tracker tolerates priming", func(t *testing.T) {
		var tracker *DuplicateTracker
		tracker.PrimeBatch([]DuplicatePriming{{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true}})
	})

	t.Run("empty-target primings register nothing while non-moving primings park", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/in/A.mkv", WillMove: false},
			{SourcePath: "/in/B.mkv", TargetPath: "", WillMove: true},
		})
		// codex P1 (PR #241): the stationary resident parked its key — a mover
		// computing the same destination takes the resident-claimed verdict
		// instead of owning the key outright. codex P2 (PR #241 F1): the parked
		// claim is PENDING until the resident's own worker settles it, so the
		// grouping assertion below sets the resident's terminal success first
		// (the blocking gate itself is pinned in duplicates_resident_w241).
		tracker.settle(&OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/in/A.mkv", WillMove: false})
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", "/in/A.mkv"))
		require.True(t, dup, "a parked resident owns its destination for the run")
		assert.Equal(t, "/in/A.mkv", prior.source)
		// The resident itself never conflicts with its own destination.
		_, dup = tracker.observe(context.Background(), &OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/in/A.mkv", WillMove: false})
		assert.False(t, dup)
		// The empty-target priming claimed nothing, so B still falls through.
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/free.mkv"))
		assert.False(t, dup)
	})

	t.Run("duplicate primings within one batch keep the sorted-first owner", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/A-reshuffled.mkv", TargetPath: "/dest/lib/./x.mkv", WillMove: true},
		})
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/A.mkv", prior.source, "same-key re-registration inside one batch keeps the first sorted claim")
	})
}

// countingProbeSeams installs probe stubs that count any probe that runs
// (returning the given definitive postures), and resets both process caches
// so every subtest starts from a truly unknown posture.
func countingProbeSeams(t *testing.T, caseSensitive, normInsensitive bool) (caseCalls, normCalls *int) {
	t.Helper()
	resetProbeCaches(t)
	cc, nc := 0, 0
	prevCase, prevNorm := fsutil.CaseSensitiveProbe, fsutil.NormalizationProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { cc++; return caseSensitive, nil }
	fsutil.NormalizationProbe = func(string) (bool, error) { nc++; return normInsensitive, nil }
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = prevCase
		fsutil.NormalizationProbe = prevNorm
	})
	return &cc, &nc
}

// TestDuplicateTracker_NonProbingDryRun pins #240 finding B: a non-probing
// (dry-run) tracker derives keys with ZERO filesystem probes — postures come
// from previously-known caches or the conservative distinction-preserving
// fallback documented on NewDuplicateTracker.
func TestDuplicateTracker_NonProbingDryRun(t *testing.T) {
	t.Run("uncached root falls back conservatively with zero probes", func(t *testing.T) {
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		tracker := NewDuplicateTracker(true)
		target := filepath.Join(dir, "Movie.mkv")
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", target))
		assert.False(t, dup)
		settleClaim(tracker, "/in/A.mkv", target)
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", target))
		require.True(t, dup, "identical spellings group under every posture")
		// The uncached fallback preserves case distinctions rather than
		// aliasing byte-distinct names on a guess.
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", filepath.Join(dir, "movie.mkv")))
		assert.False(t, dup, "uncached dry-run roots keep case variants distinct (conservative)")
		assert.Equal(t, 0, *caseCalls, "case probe never ran")
		assert.Equal(t, 0, *normCalls, "normalization probe never ran")
	})

	t.Run("previously-cached postures still fold with zero new probes", func(t *testing.T) {
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		// Populate the process caches definitively (this is the ONLY probing
		// moment, owned by an earlier live run in production terms).
		fsutil.IsCaseSensitiveRoot(dir)
		fsutil.IsNormalizationInsensitiveRoot(dir)
		require.Equal(t, 1, *caseCalls)
		require.Equal(t, 1, *normCalls)
		tracker := NewDuplicateTracker(true)
		target := filepath.Join(dir, "Movie.mkv")
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", target))
		settleClaim(tracker, "/in/A.mkv", target)
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", filepath.Join(dir, "movie.mkv")))
		require.True(t, dup, "cached-insensitive postures fold case variants without new probes")
		assert.Equal(t, 1, *caseCalls, "no new case probe during non-probing derivation")
		assert.Equal(t, 1, *normCalls, "no new normalization probe during non-probing derivation")
	})

	t.Run("probing tracker behavior is unchanged for live runs", func(t *testing.T) {
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		tracker := NewDuplicateTracker(false) // live policy
		target := filepath.Join(dir, "Movie.mkv")
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", target))
		settleClaim(tracker, "/in/A.mkv", target)
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", filepath.Join(dir, "movie.mkv")))
		require.True(t, dup, "live runs probe and fold case variants")
		assert.Equal(t, 1, *caseCalls, "one case probe per root per process")
		assert.Equal(t, 1, *normCalls, "one normalization probe per root per process")
	})
}

// TestOrganize_ForceUpdatePrimedDuplicate_OnlyWinnerBytesLand pins #240
// finding A end-to-end: under ForceUpdate only the deterministic winner's
// bytes land, regardless of goroutine order.
func TestOrganize_ForceUpdatePrimedDuplicate_OnlyWinnerBytesLand(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"

	primedFixture := func(t *testing.T) (*Organizer, afero.Fs, *DuplicateTracker, OrganizeCmd, OrganizeCmd) {
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true}, // sorted first: /in/A.mkv < /in/B.mkv
			{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
		})
		cmdA := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, true, false)
		cmdB := dupBatchCmd(models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, true, false)
		return org, fs, tracker, cmdA, cmdB
	}
	assertWinnerBytes := func(t *testing.T, fs afero.Fs) {
		content, err := afero.ReadFile(fs, target)
		require.NoError(t, err)
		assert.Equal(t, []byte("a-bytes"), content, "only the sorted-first winner's bytes land")
		loser, err := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, err)
		assert.Equal(t, []byte("b-bytes"), loser, "the primed loser keeps its source untouched")
		_, err = fs.Stat("/in/A.mkv")
		assert.Error(t, err, "the winner actually moved out of its source")
	}

	t.Run("loser observes first", func(t *testing.T) {
		org, fs, tracker, cmdA, cmdB := primedFixture(t)
		// #241 P2: the loser observing mid-owner-flight now WAITS for the
		// owner's terminal outcome; its verdict only resolves once A's
		// organize settles.
		type applyOutcome struct {
			res *OrganizeResult
			err error
		}
		bDone := make(chan applyOutcome, 1)
		go func() {
			res, err := org.Organize(context.Background(), cmdB)
			bDone <- applyOutcome{res, err}
		}()
		waitForWaiter(t, tracker, target, "/in/B.mkv")
		resultA, err := org.Organize(context.Background(), cmdA)
		require.NoError(t, err)
		assert.True(t, resultA.Moved)
		outB := <-bDone
		require.NoError(t, outB.err)
		assert.False(t, outB.res.Moved)
		require.Len(t, outB.res.Warnings, 1)
		assert.Contains(t, outB.res.Warnings[0], "already claimed by /in/A.mkv")
		assertWinnerBytes(t, fs)
	})

	t.Run("winner observes first", func(t *testing.T) {
		org, fs, _, cmdA, cmdB := primedFixture(t)
		resultA, err := org.Organize(context.Background(), cmdA)
		require.NoError(t, err)
		assert.True(t, resultA.Moved)
		resultB, err := org.Organize(context.Background(), cmdB)
		require.NoError(t, err)
		assert.False(t, resultB.Moved)
		require.Len(t, resultB.Warnings, 1)
		assertWinnerBytes(t, fs)
	})

	t.Run("concurrent", func(t *testing.T) {
		org, fs, _, cmdA, cmdB := primedFixture(t)
		start := make(chan struct{})
		results := make(chan *OrganizeResult, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, cmd := range []OrganizeCmd{cmdA, cmdB} {
			wg.Add(1)
			go func(cmd OrganizeCmd) {
				defer wg.Done()
				<-start
				res, err := org.Organize(context.Background(), cmd)
				results <- res
				errs <- err
			}(cmd)
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		var resA, resB *OrganizeResult
		for res := range results {
			if res.OriginalPath == "/in/A.mkv" {
				resA = res
			} else {
				resB = res
			}
		}
		for err := range errs {
			require.NoError(t, err)
		}
		assert.True(t, resA.Moved, "the winner moves under any schedule")
		assert.False(t, resB.Moved, "the loser skips under any schedule")
		require.Len(t, resB.Warnings, 1)
		assertWinnerBytes(t, fs)
	})
}

// TestOrganizer_PlanOrganize pins the read-only planning seam #240 finding A
// primes from: identical planner selection as Organize, no duplicate
// preflight, no validation, no filesystem mutation.
func TestOrganizer_PlanOrganize(t *testing.T) {
	t.Run("matches Organize's planning half with zero side effects", func(t *testing.T) {
		org, fs := dupBatchFixture(t)
		match := models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}
		plan, err := org.PlanOrganize(context.Background(), OrganizeCmd{
			Match: match, Movie: &models.Movie{ID: "ABC-123"}, DestDir: "/dest", MoveFiles: true,
		})
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.Equal(t, "/in/A.mkv", plan.SourcePath)
		assert.Equal(t, "/dest/ABC-123/ABC-123.mkv", filepath.ToSlash(plan.TargetPath))
		assert.True(t, plan.WillMove)
		destExists, statErr := afero.Exists(fs, "/dest")
		require.NoError(t, statErr)
		assert.False(t, destExists, "planning performs no filesystem mutation")
	})

	t.Run("canceled context aborts before planning", func(t *testing.T) {
		org, _ := dupBatchFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		plan, err := org.PlanOrganize(ctx, OrganizeCmd{})
		assert.Nil(t, plan)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("plan errors propagate", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		org := NewOrganizer(fs, &Config{
			FolderFormat:  "<IF:SERIES><SERIES>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}, nil, nil)
		_, err := org.PlanOrganize(context.Background(), OrganizeCmd{
			Match:   models.FileMatchInfo{Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv", MovieID: "ABC-123"},
			Movie:   &models.Movie{ID: "ABC-123", Series: "x"},
			DestDir: "/dest",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unclosed <IF> block")
	})

	t.Run("ForceRenameFile shares Organize's planner override", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/source", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/source/old.mkv", []byte("video"), 0o644))
		org := NewOrganizer(fs, &Config{
			FileFormat:    "<ID>",
			RenameFile:    false, // disabled globally; the command forces it
			OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		}, nil, nil)
		match := models.FileMatchInfo{Path: "/source/old.mkv", Name: "old.mkv", Extension: ".mkv", MovieID: "ABC-123"}
		movie := &models.Movie{ID: "ABC-123"}

		plan, err := org.PlanOrganize(context.Background(), OrganizeCmd{
			Match: match, Movie: movie, DestDir: "/source", MoveFiles: true, ForceRenameFile: true,
			OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		})
		require.NoError(t, err)
		assert.Equal(t, "/source/ABC-123.mkv", filepath.ToSlash(plan.TargetPath), "the forced planner clone renames")

		plan, err = org.PlanOrganize(context.Background(), OrganizeCmd{
			Match: match, Movie: movie, DestDir: "/source", MoveFiles: true,
			OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		})
		require.NoError(t, err)
		assert.Equal(t, "/source/old.mkv", filepath.ToSlash(plan.TargetPath), "without the override the planner keeps the source name")
	})
}

// TestDuplicateTracker_Release pins the codex r2 P2 guard semantics: an
// OWNED claim frees its key when the owning plan proves inexecutable, while
// losers, foreign sources, and register-nothing plans release nothing. The
// #241 P2 terminal gate layers on top: only an UNSETTLED owner release fails
// the entry (waiters promote; wait-free keys free outright), and a settled
// claim's verdict is final — releasing it changes nothing.
func TestDuplicateTracker_Release(t *testing.T) {
	t.Run("nil tracker, nil plan, and register-nothing plans release nothing", func(t *testing.T) {
		forceCasePosture(t, true)
		var nilTracker *DuplicateTracker
		nilTracker.release(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		nilTracker.release(nil)

		tracker := NewDuplicateTracker(false)
		tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		tracker.observe(context.Background(), dupPlanFor("/in/C.mkv", "/dest/lib/y.mkv"))
		tracker.release(nil)
		tracker.release(&OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/in/A.mkv", WillMove: false})
		tracker.release(&OrganizePlan{SourcePath: "/in/C.mkv", TargetPath: "", WillMove: true})
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		settleClaim(tracker, "/in/C.mkv", "/dest/lib/y.mkv")
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		assert.True(t, dup, "untouched key still owned by A")
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/D.mkv", "/dest/lib/y.mkv"))
		assert.True(t, dup, "untouched key still owned by C")
	})

	t.Run("only the recorded owner frees the key", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		tracker.release(dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "the owner's own observation is idempotent")
		settleClaim(tracker, "/in/A.mkv", "/dest/lib/x.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/D.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup, "a loser's release must not free the owner's key")
		assert.Equal(t, "/in/A.mkv", prior.source)
		tracker.release(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv#unclaimed"))
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/E.mkv", "/dest/lib/z.mkv"))
		assert.False(t, dup)
		settleClaim(tracker, "/in/E.mkv", "/dest/lib/z.mkv")

		// #241 P2: releasing an already-SETTLED claim is a no-op — the
		// winner's terminal outcome is final.
		tracker.release(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		prior, dup = tracker.observe(context.Background(), dupPlanFor("/in/F.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup, "releasing a settled winner changes nothing")
		assert.Equal(t, "/in/A.mkv", prior.source)
	})

	t.Run("unsettled owner release frees an unwatched key for the next claimant", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup)
		tracker.release(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		_, dup = tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "owner release lets the next claimant register the freed key")
		settleClaim(tracker, "/in/B.mkv", "/dest/lib/x.mkv")
		prior, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup, "the freed key re-registers to its new claimant")
		assert.Equal(t, "/in/B.mkv", prior.source)
	})

	t.Run("unsettled owner release promotes the sorted-first waiter", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
			{SourcePath: "/in/C.mkv", TargetPath: "/dest/lib/x.mkv", WillMove: true},
		})
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup)
		type outcome struct {
			src   string
			prior duplicateClaim
			dup   bool
		}
		results := make(chan outcome, 2)
		for _, src := range []string{"/in/B.mkv", "/in/C.mkv"} {
			go func(src string) {
				prior, dup := tracker.observe(context.Background(), dupPlanFor(src, "/dest/lib/x.mkv"))
				results <- outcome{src: src, prior: prior, dup: dup}
			}(src)
		}
		waitForWaiter(t, tracker, "/dest/lib/x.mkv", "/in/B.mkv")
		waitForWaiter(t, tracker, "/dest/lib/x.mkv", "/in/C.mkv")

		tracker.release(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		promoted := <-results
		assert.Equal(t, "/in/B.mkv", promoted.src, "#241 P2: the sorted-first waiter takes over the freed key")
		assert.False(t, promoted.dup, "the promoted waiter proceeds as owner")

		// The remaining waiter carried over to the promoted claim (no
		// duplicate re-registration) and still sees a duplicate verdict once
		// the new owner settles.
		settleClaim(tracker, "/in/B.mkv", "/dest/lib/x.mkv")
		last := <-results
		assert.Equal(t, "/in/C.mkv", last.src)
		require.True(t, last.dup)
		assert.Equal(t, "/in/B.mkv", last.prior.source)
	})
}

// TestOrganize_PrimedStaleWinnerReleasesClaim is the codex r2 P2
// stale-source winner scenario end to end: the sorted-first claimant's
// source disappears BETWEEN priming and apply, so its primed claim must be
// released when its plan fails — the later valid claimant then falls
// through and lands the destination instead of dying on the stale owner's
// key. Determinism (sorted priming order) and PrimeBatch idempotence are
// unchanged; the disappeared-source case is handled explicitly.
func TestOrganize_PrimedStaleWinnerReleasesClaim(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"

	primedStaleFixture := func(t *testing.T) (*Organizer, afero.Fs, *DuplicateTracker) {
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true}, // sorted first: /in/A.mkv < /in/B.mkv
			{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
		})
		require.NoError(t, fs.Remove("/in/A.mkv"), "the winner's source vanishes between priming and apply")
		return org, fs, tracker
	}
	assertExactlyOneVideoFromB := func(t *testing.T, fs afero.Fs) {
		content, err := afero.ReadFile(fs, target)
		require.NoError(t, err)
		assert.Equal(t, []byte("b-bytes"), content, "the valid claimant's bytes reach the destination")
		var videos []string
		require.NoError(t, afero.Walk(fs, "/dest", func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, ".mkv") {
				videos = append(videos, filepath.ToSlash(path))
			}
			return nil
		}))
		assert.Equal(t, []string{target}, videos, "destination ends with exactly one video, from the valid claimant")
	}

	t.Run("normal mode: the valid claimant proceeds to move", func(t *testing.T) {
		org, fs, tracker := primedStaleFixture(t)
		_, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source file does not exist", "the stale winner fails validation and releases")

		resultB, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, false, false))
		require.NoError(t, err, "no duplicate-conflict false positive against the released stale claim")
		assert.True(t, resultB.Moved)
		assert.Empty(t, resultB.Warnings, "the new owner is no duplicate of the vanished claimant")
		assertExactlyOneVideoFromB(t, fs)
	})

	t.Run("force mode: no winner-skip-yet-success inconsistency", func(t *testing.T) {
		org, fs, tracker := primedStaleFixture(t)
		_, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, true, false))
		require.Error(t, err, "ForceUpdate skips validation, so the disappeared source fails at execute — and releases")

		resultB, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, true, false))
		require.NoError(t, err)
		assert.True(t, resultB.Moved, "the valid claimant actually moves — never a skipped 'success'")
		assert.Empty(t, resultB.Warnings, "the new owner earns success with no duplicate warning")
		assertExactlyOneVideoFromB(t, fs)
	})

	for _, force := range []bool{false, true} {
		name := "normal mode"
		if force {
			name = "force mode"
		}
		t.Run("concurrent "+name+": the blocked waiter promotes and moves after the stale owner fails", func(t *testing.T) {
			org, fs, tracker := primedStaleFixture(t)
			type applyOutcome struct {
				res *OrganizeResult
				err error
			}
			bDone := make(chan applyOutcome, 1)
			go func() {
				res, err := org.Organize(context.Background(), dupBatchCmd(
					models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, force, false))
				bDone <- applyOutcome{res, err}
			}()
			// #241 P2: B's observe waits on the stale owner's terminal outcome
			// instead of conflict-racing or ghost-skipping — prove the waiter is
			// genuinely blocked, THEN fail the owner so the waiter promotes.
			waitForWaiter(t, tracker, target, "/in/B.mkv")

			_, err := org.Organize(context.Background(), dupBatchCmd(
				models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, force, false))
			require.Error(t, err)

			out := <-bDone
			require.NoError(t, out.err, "the promoted claimant's organize succeeds")
			assert.True(t, out.res.Moved)
			assert.Empty(t, out.res.Warnings, "a promoted owner is no duplicate")
			assertExactlyOneVideoFromB(t, fs)
		})
	}
}

// TestOrganize_ConflictBranchReleasesStaleOwner covers the release on the
// conflict terminal (reached under ForceUpdate, which skips validatePlan):
// an inexecutable primed owner frees its key there too, and only there —
// a loser's ConflictDuplicate never matches the owner key.
func TestOrganize_ConflictBranchReleasesStaleOwner(t *testing.T) {
	forceCasePosture(t, true)
	const target = "/dest/ABC-123/ABC-123.mkv"

	t.Run("force + directory conflict frees the owner's key", func(t *testing.T) {
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
		})
		// A directory occupies what must be a file path: ForceUpdate
		// suppresses only ConflictFile, so the plan short-circuits at the
		// conflict terminal with ConflictDirectory.
		require.NoError(t, fs.MkdirAll(target, 0755))
		_, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, true, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts detected")
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", target))
		assert.False(t, dup, "the conflict-failed owner released; the next claimant falls through")
	})

	t.Run("a loser's duplicate conflict never releases the owner's key", func(t *testing.T) {
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker(false)
		tracker.PrimeBatch([]DuplicatePriming{
			{SourcePath: "/in/A.mkv", TargetPath: target, WillMove: true},
			{SourcePath: "/in/B.mkv", TargetPath: target, WillMove: true},
		})
		// The winner applies first and settles (#241 P2): the loser's
		// terminal-gated duplicate verdict is then immediate.
		resA, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, false))
		require.NoError(t, err)
		require.True(t, resA.Moved)
		_, err = org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, false, false))
		require.Error(t, err, "the unauthorized loser fails through the identical conflict pipeline")
		assert.Contains(t, filepath.ToSlash(err.Error()), target)
		_, dup := tracker.observe(context.Background(), dupPlanFor("/in/B.mkv", target))
		require.True(t, dup, "the owner keeps its key through the loser's failure")
		srcContent, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), srcContent, "the losing duplicate's source is untouched")
	})
}

// TestOrganizer_PlanSourceExists pins the codex r2 P2 priming guard's
// filesystem check: only a genuinely missing source withholds the claim;
// indeterminate Stat errors leave the decision to validation/execution.
func TestOrganizer_PlanSourceExists(t *testing.T) {
	t.Run("present source claims", func(t *testing.T) {
		org, _ := dupBatchFixture(t)
		assert.True(t, org.PlanSourceExists(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv")))
	})

	t.Run("vanished source withholds the claim", func(t *testing.T) {
		org, _ := dupBatchFixture(t)
		assert.False(t, org.PlanSourceExists(dupPlanFor("/in/gone.mkv", "/dest/lib/x.mkv")))
	})

	t.Run("non-missing Stat errors still claim", func(t *testing.T) {
		// A momentary IO fault is NOT IsNotExist — mirroring validatePlan's
		// rule, the claim decision defers to validation/execution.
		org, _ := dupBatchFixture(t)
		faulty := &statFaultFs{Fs: afero.NewMemMapFs(), err: errors.New("transient io fault")}
		org = NewOrganizer(faulty, org.config, nil, nil)
		assert.True(t, org.PlanSourceExists(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv")))
	})
}
