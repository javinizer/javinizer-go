package organizer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

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

func dupPlanFor(src, target string) *OrganizePlan {
	return &OrganizePlan{SourcePath: src, TargetPath: target, WillMove: true}
}

func TestDuplicateTracker_Grouping(t *testing.T) {
	t.Run("identical target distinct sources", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker()
		_, dup := tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "first claim registers cleanly")
		prior, dup := tracker.observe(dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/A.mkv", prior.source)
	})

	t.Run("casefold variants group when root proven insensitive", func(t *testing.T) {
		forceCasePosture(t, false)
		tracker := NewDuplicateTracker()
		tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/Movie.mkv"))
		prior, dup := tracker.observe(dupPlanFor("/in/B.mkv", "/dest/lib/movie.mkv"))
		require.True(t, dup, "case variants of one name are proven-equal on insensitive roots")
		assert.Equal(t, "/in/A.mkv", prior.source)
	})

	t.Run("case variants stay distinct when root proven sensitive", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker()
		tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/Movie.mkv"))
		_, dup := tracker.observe(dupPlanFor("/in/B.mkv", "/dest/lib/movie.mkv"))
		assert.False(t, dup, "never proven equal, so byte-distinct names keep distinct keys")
	})

	t.Run("lexical spelling variants of one path group", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker()
		tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		_, dup := tracker.observe(dupPlanFor("/in/B.mkv", "/dest/lib/./x.mkv"))
		assert.True(t, dup, "cleaned spellings of one destination are the same canonical key")
	})

	t.Run("multipart suffixes stay distinct", func(t *testing.T) {
		forceCasePosture(t, false)
		tracker := NewDuplicateTracker()
		tracker.observe(dupPlanFor("/in/m-cd1.mkv", "/dest/lib/m-cd1.mkv"))
		_, dup := tracker.observe(dupPlanFor("/in/m-cd2.mkv", "/dest/lib/m-cd2.mkv"))
		assert.False(t, dup, "cdN part suffixes name distinct destinations")
	})

	t.Run("same source re-observe is idempotent", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker()
		tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		_, dup := tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "retries and dry-run re-plans of one file never self-conflict")
	})

	t.Run("third duplicate reports the first claim", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker()
		tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		tracker.observe(dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv"))
		prior, dup := tracker.observe(dupPlanFor("/in/C.mkv", "/dest/lib/x.mkv"))
		require.True(t, dup)
		assert.Equal(t, "/in/A.mkv", prior.source, "first claim wins — mirrors first-publish-wins at execute")
	})

	t.Run("plans that move nothing are never registered", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker()
		still := &OrganizePlan{SourcePath: "/in/A.mkv", TargetPath: "/in/A.mkv", WillMove: false}
		_, dup := tracker.observe(still)
		assert.False(t, dup)
		_, dup = tracker.observe(dupPlanFor("/in/B.mkv", "/in/A.mkv"))
		assert.False(t, dup, "WillMove=false targets are owned by destination-occupation checks")
	})

	t.Run("empty and nil plans are never registered", func(t *testing.T) {
		forceCasePosture(t, true)
		var nilTracker *DuplicateTracker
		_, dup := nilTracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))
		assert.False(t, dup, "nil tracker disables detection")
		tracker := NewDuplicateTracker()
		_, dup = tracker.observe(nil)
		assert.False(t, dup)
		_, dup = tracker.observe(dupPlanFor("/in/A.mkv", ""))
		assert.False(t, dup)
	})

	t.Run("concurrent observations are race-free and total", func(t *testing.T) {
		forceCasePosture(t, true)
		tracker := NewDuplicateTracker()
		var wg sync.WaitGroup
		var mu sync.Mutex
		claimsPerKey := map[string]int{}
		for i := 0; i < 64; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("/dest/lib/movie-%d.mkv", i%8)
				_, dup := tracker.observe(dupPlanFor(fmt.Sprintf("/in/src-%d.mkv", i), key))
				if !dup {
					mu.Lock()
					claimsPerKey[key]++
					mu.Unlock()
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
	tracker := NewDuplicateTracker()
	tracker.observe(dupPlanFor("/in/A.mkv", "/dest/lib/x.mkv"))

	t.Run("unauthorized duplicate joins plan conflicts", func(t *testing.T) {
		plan := dupPlanFor("/in/B.mkv", "/dest/lib/x.mkv")
		warnings := applyDuplicatePreflight(plan, tracker, false)
		assert.Nil(t, warnings)
		require.Len(t, plan.Conflicts, 1)
		assert.Equal(t, ConflictDuplicate, plan.Conflicts[0].Kind)
		assert.Equal(t, "/dest/lib/x.mkv", plan.Conflicts[0].Path)
		assert.Equal(t, "duplicate", plan.Conflicts[0].kindName())
	})

	t.Run("authorized duplicate demotes to warning", func(t *testing.T) {
		plan := dupPlanFor("/in/C.mkv", "/dest/lib/x.mkv")
		warnings := applyDuplicatePreflight(plan, tracker, true)
		assert.Empty(t, plan.Conflicts, "authorization demotes the duplicate out of the conflict pipeline")
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "/dest/lib/x.mkv")
		assert.Contains(t, warnings[0], "/in/A.mkv")
		assert.Contains(t, warnings[0], "overwrite authorized")
	})

	t.Run("no duplicate yields nothing", func(t *testing.T) {
		plan := dupPlanFor("/in/D.mkv", "/dest/lib/y.mkv")
		assert.Nil(t, applyDuplicatePreflight(plan, tracker, false))
		assert.Nil(t, applyDuplicatePreflight(plan, nil, false), "nil tracker disables detection")
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
	tracker := NewDuplicateTracker()
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
	tracker := NewDuplicateTracker()

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
		tracker := NewDuplicateTracker()
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
		assert.Contains(t, err.Error(), "/dest/ABC-123/ABC-123.mkv")
		content, readErr := afero.ReadFile(fs, "/in/B.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), content, "the losing duplicate's source is untouched")
	})

	t.Run("authorized duplicate executes and reports the warning", func(t *testing.T) {
		forceCasePosture(t, true)
		org, fs := dupBatchFixture(t)
		tracker := NewDuplicateTracker()
		_, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, true))
		require.NoError(t, err)
		result, err := org.Organize(context.Background(), dupBatchCmd(
			models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, true, false))
		require.NoError(t, err)
		require.Len(t, result.Warnings, 1, "live authorized result carries the persisted-warning payload")
		assert.Contains(t, result.Warnings[0], "overwrite authorized")
		assert.True(t, result.Moved)
		content, readErr := afero.ReadFile(fs, "/dest/ABC-123/ABC-123.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("b-bytes"), content)
	})
}
