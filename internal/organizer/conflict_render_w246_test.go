package organizer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Issue #246: an unauthorized duplicate of an on-disk-OCCUPIED destination
// composed two identical bare-path renders into the failure message (the
// destination-occupation PlanConflict + the appended ConflictDuplicate), e.g.
// "organization validation failed: [/dest/X/Y.mp4 /dest/X/Y.mp4]". The render
// dedupes at composition level; every conflict's semantics stay unchanged.

const w246Dest = "/dest/ABC-123/ABC-123.mkv"

// w246Slash slash-normalizes produced paths/messages so assertions against the
// "/dest/..." literals stay separator-portable: on Windows the planner's
// filepath.Join renders the destination with '\'.
func w246Slash(s string) string { return filepath.ToSlash(s) }

func TestDistinctConflictRenders_DedupesIdenticalPaths(t *testing.T) {
	t.Run("identical renders collapse once, order stable", func(t *testing.T) {
		cs := []PlanConflict{
			{Path: w246Dest, Kind: ConflictFile},
			{Path: w246Dest, Kind: ConflictDuplicate},
			{Path: "/dest/other.mkv", Kind: ConflictFile},
		}
		assert.Equal(t, []string{w246Dest, "/dest/other.mkv"}, distinctConflictRenders(cs))
	})

	t.Run("empty and singleton pass through", func(t *testing.T) {
		assert.Empty(t, distinctConflictRenders(nil))
		assert.Equal(t, []string{w246Dest}, distinctConflictRenders([]PlanConflict{{Path: w246Dest, Kind: ConflictSymlink}}))
	})

	t.Run("joinPlanConflictPaths renders a doubled path once", func(t *testing.T) {
		cs := []PlanConflict{
			{Path: w246Dest, Kind: ConflictFile},
			{Path: w246Dest, Kind: ConflictDuplicate},
		}
		assert.Equal(t, w246Dest, joinPlanConflictPaths(cs))
	})
}

func TestValidatePlan_DedupesIdenticalConflictPaths(t *testing.T) {
	org, _ := dupBatchFixture(t)
	plan := &OrganizePlan{
		SourcePath: "/in/B.mkv",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mkv",
		TargetPath: w246Dest,
		WillMove:   true,
		Conflicts: []PlanConflict{
			{Path: w246Dest, Kind: ConflictFile},
			{Path: w246Dest, Kind: ConflictDuplicate},
		},
	}
	assert.Equal(t, []string{w246Dest}, org.validatePlan(plan), "the doubled render collapses to one issue line")
}

// Pin the dup-only conflict render: exact full message, destination once.
func TestOrganize_UnauthorizedDuplicateConflictRendersDestinationOnce(t *testing.T) {
	forceCasePosture(t, true)
	org, _ := dupBatchFixture(t)
	tracker := NewDuplicateTracker(true)

	_, err := org.Organize(context.Background(), dupBatchCmd(
		models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, false, true))
	require.NoError(t, err)

	_, dupErr := org.Organize(context.Background(), dupBatchCmd(
		models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, false, true))
	require.Error(t, dupErr)
	assert.Equal(t, "organization validation failed: ["+w246Dest+"]", w246Slash(dupErr.Error()))
	assert.Equal(t, 1, strings.Count(w246Slash(dupErr.Error()), w246Dest))
}

// Pin the occupied-dest conflict render: exact full message, destination once.
func TestOrganize_OccupiedDestinationConflictRendersDestinationOnce(t *testing.T) {
	forceCasePosture(t, true)
	org, fs := dupBatchFixture(t)
	require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0o755))
	require.NoError(t, afero.WriteFile(fs, w246Dest, []byte("foreign"), 0o644))

	_, occErr := org.Organize(context.Background(), dupBatchCmd(
		models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, NewDuplicateTracker(true), false, true))
	require.Error(t, occErr)
	assert.Equal(t, "organization validation failed: ["+w246Dest+"]", w246Slash(occErr.Error()))
	assert.Equal(t, 1, strings.Count(w246Slash(occErr.Error()), w246Dest))
}

// The #246 reproduction: destination occupied on disk AND batch-claimed. The
// unauthorized loser carries BOTH PlanConflicts (occupation + duplicate) with
// identical renders — the composed message must still print the path once.
func TestOrganize_DuplicateOfOccupiedDestinationRendersDestinationOnce(t *testing.T) {
	forceCasePosture(t, true)
	org, fs := dupBatchFixture(t)
	require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0o755))
	require.NoError(t, afero.WriteFile(fs, w246Dest, []byte("foreign"), 0o644))
	tracker := NewDuplicateTracker(true)

	// A claims the destination key with overwrite authorization (its
	// ConflictFile is suppressed), settling the claim on the occupied target.
	_, err := org.Organize(context.Background(), dupBatchCmd(
		models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"}, tracker, true, true))
	require.NoError(t, err)

	// B is unauthorized AND disk-occupied: pre-fix this rendered the doubled
	// "organization validation failed: ["+dest+" "+dest+"]".
	_, combErr := org.Organize(context.Background(), dupBatchCmd(
		models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"}, tracker, false, true))
	require.Error(t, combErr)
	assert.Equal(t, "organization validation failed: ["+w246Dest+"]", w246Slash(combErr.Error()))
	assert.Equal(t, 1, strings.Count(w246Slash(combErr.Error()), w246Dest),
		"occupation + duplicate conflicts render the destination once")
}
