package organizer

import (
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// finishInPlaceInnerRename branches (#224 codex P2): a completed inner publish
// MUST NOT roll back the directory (file already lives at the new location);
// plain publish failures roll back to OldDir as before.
func TestFinishInPlaceInnerRename_Completed_PublishesNoRollback(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/new", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/new/x.mp4", []byte("v"), 0o644))

	strategy := &inPlaceStrategy{fs: fs}
	plan := &OrganizePlan{TargetDir: "/new", OldDir: "/old", TargetPath: "/new/x.mp4"}

	refusal := fmt.Errorf("publish link worked, cleanup refused: %w", fsutil.ErrPublishCompleted)
	err := strategy.finishInPlaceInnerRename(plan, refusal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory left at /new")
	assert.Contains(t, err.Error(), "ambiguous")

	// The directory stays at the NEW name — no rollback applied.
	exists, _ := afero.Exists(fs, "/new/x.mp4")
	assert.True(t, exists)
}

func TestFinishInPlaceInnerRename_Collision_RollsBack(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/new", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/new/x.mp4", []byte("v"), 0o644))

	strategy := &inPlaceStrategy{fs: fs}
	plan := &OrganizePlan{TargetDir: "/new", OldDir: "/old", TargetPath: "/new/x.mp4"}

	collision := fmt.Errorf("%w", fsutil.ErrPublishCollision)
	err := strategy.finishInPlaceInnerRename(plan, collision)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to rename file after directory rename")
	// Rollback applied: /new renamed to /old.
	exists, _ := afero.Exists(fs, "/old/x.mp4")
	assert.True(t, exists)
}

var _ = errors.New
