package organizer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
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
	require.NoError(t, fs.MkdirAll(filepath.FromSlash("/new"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.FromSlash("/new/x.mp4"), []byte("v"), 0o644))

	strategy := &inPlaceStrategy{fs: fs}
	plan := &OrganizePlan{TargetDir: "/new", OldDir: "/old", TargetPath: "/new/x.mp4"}
	result := &OrganizeResult{
		NewPath:          "/new/x.mp4",
		InPlaceRenamed:   true,
		OldDirectoryPath: "/old",
		NewDirectoryPath: "/new",
	}

	refusal := fmt.Errorf("publish link worked, cleanup refused: %w", fsutil.ErrPublishCompleted)
	err := strategy.finishInPlaceInnerRename(plan, result, refusal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory left at /new")
	assert.Contains(t, err.Error(), "ambiguous")

	// The directory stays at the NEW name — no rollback applied, and the
	// journal-bearing in-place fields stand untouched.
	exists, _ := afero.Exists(fs, filepath.FromSlash("/new/x.mp4"))
	assert.True(t, exists)
	assert.True(t, result.InPlaceRenamed)
	assert.Equal(t, "/new", filepath.ToSlash(result.NewDirectoryPath))
}

func TestFinishInPlaceInnerRename_Collision_RollsBack(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.FromSlash("/new"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.FromSlash("/new/x.mp4"), []byte("v"), 0o644))

	strategy := &inPlaceStrategy{fs: fs}
	plan := &OrganizePlan{TargetDir: "/new", OldDir: "/old", TargetPath: "/new/x.mp4"}
	result := &OrganizeResult{
		NewPath:          "/new/x.mp4",
		FileName:         "x.mp4",
		InPlaceRenamed:   true,
		OldDirectoryPath: "/old",
		NewDirectoryPath: "/new",
	}

	collision := fmt.Errorf("%w", fsutil.ErrPublishCollision)
	err := strategy.finishInPlaceInnerRename(plan, result, collision)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to rename file after directory rename")
	assert.NotContains(t, err.Error(), "survived")
	// Rollback applied: /new renamed to /old.
	exists, _ := afero.Exists(fs, filepath.FromSlash("/old/x.mp4"))
	assert.True(t, exists)
	// codex P1 (PR #241): a landed rollback means NOTHING survived on disk —
	// the journal-bearing in-place fields clear so the organizer finalizes
	// this leg pre-publication (completed-noop).
	assert.False(t, result.InPlaceRenamed)
	assert.Empty(t, result.OldDirectoryPath)
	assert.Empty(t, result.NewDirectoryPath)
	assert.Equal(t, "/new/x.mp4", filepath.ToSlash(result.NewPath),
		"NewPath keeps naming the intended target (display) — the organizer's pre-publication mark journals none of it")
}

// rollbackRefusedFs fails exactly ONE rename of the (old, new) pair, arming a
// refused in-place directory rollback deterministically and cross-platform.
type rollbackRefusedFs struct {
	afero.Fs
	old, new string
	fired    atomic.Bool
}

func (p *rollbackRefusedFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(p.old) && filepath.Clean(newname) == filepath.Clean(p.new) && p.fired.CompareAndSwap(false, true) {
		return &os.PathError{Op: "rename", Path: oldname, Err: syscall.EACCES}
	}
	return p.Fs.Rename(oldname, newname)
}

// TestFinishInPlaceInnerRename_RollbackRefused_RenameSurvives pins codex P1
// (PR #241): when the post-failure directory rollback is REFUSED, the rename
// survives on disk — the result keeps its rename marker and its directory
// fields, and NewPath/FileName re-name where the file's bytes actually are
// (the OLD name inside the renamed directory), so the settle+journal of the
// surviving mutation reverts it by exact inverse.
func TestFinishInPlaceInnerRename_RollbackRefused_RenameSurvives(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll(filepath.FromSlash("/new"), 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.FromSlash("/new/old.mkv"), []byte("v"), 0o644))
	poison := &rollbackRefusedFs{Fs: base, old: "/new", new: "/old"}

	strategy := &inPlaceStrategy{fs: poison}
	src := filepath.FromSlash("/old/old.mkv")
	plan := &OrganizePlan{
		SourcePath: src,
		TargetDir:  filepath.FromSlash("/new"),
		OldDir:     filepath.FromSlash("/old"),
		TargetPath: filepath.FromSlash("/new/x.mp4"),
		TargetFile: "x.mp4",
	}
	plan.Match.Name = "old.mkv"
	result := &OrganizeResult{
		NewPath:          filepath.FromSlash("/new/x.mp4"),
		FileName:         "x.mp4",
		InPlaceRenamed:   true,
		OldDirectoryPath: filepath.FromSlash("/old"),
		NewDirectoryPath: filepath.FromSlash("/new"),
	}

	collision := fmt.Errorf("%w", fsutil.ErrPublishCollision)
	err := strategy.finishInPlaceInnerRename(plan, result, collision)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to rename file after directory rename")
	assert.Contains(t, err.Error(), "survived")
	assert.True(t, poison.fired.Load(), "the rollback rename was attempted and refused")

	// The directory rename SURVIVED: /new stands with the file at its old name.
	exists, _ := afero.Exists(base, filepath.FromSlash("/new/old.mkv"))
	assert.True(t, exists)
	exists, _ = afero.Exists(base, filepath.FromSlash("/old"))
	assert.False(t, exists)

	assert.True(t, result.InPlaceRenamed, "the surviving rename marker stands")
	assert.Equal(t, "/old", filepath.ToSlash(result.OldDirectoryPath))
	assert.Equal(t, "/new", filepath.ToSlash(result.NewDirectoryPath))
	assert.Equal(t, "/new/old.mkv", filepath.ToSlash(result.NewPath),
		"NewPath names where the bytes actually went")
	assert.Equal(t, "old.mkv", result.FileName)
}

// TestFinishInPlaceInnerRename_RollbackRefused_EmptyMatchName_UsesSourceBase
// pins the one remaining branch of the rollback-refused arm NOT covered by the
// non-empty-name pin above: when plan.Match.Name is EMPTY the surviving file
// is re-named from filepath.Base(plan.SourcePath). Before this pin,
// strategy_inplace.go's `oldFileName == ""` fallback block was only reachable
// from raced end-to-end organize flows (which file eats the one-shot refusal,
// whether the matcher populated Match.Name), so full-suite patch coverage
// flickered PASSED/FAILED on identical code (codecov: 1 miss + 1 partial at
// lines 430-432). Direct invocation drives the exact production function, so
// the fallback block reads count >= 1 on every run of every shape.
func TestFinishInPlaceInnerRename_RollbackRefused_EmptyMatchName_UsesSourceBase(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll(filepath.FromSlash("/new"), 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.FromSlash("/new/original.mkv"), []byte("v"), 0o644))
	poison := &rollbackRefusedFs{Fs: base, old: "/new", new: "/old"}

	strategy := &inPlaceStrategy{fs: poison}
	src := filepath.FromSlash("/old/original.mkv")
	plan := &OrganizePlan{
		SourcePath: src,
		TargetDir:  filepath.FromSlash("/new"),
		OldDir:     filepath.FromSlash("/old"),
		TargetPath: filepath.FromSlash("/new/x.mp4"),
		TargetFile: "x.mp4",
		// Match left zero-valued: Name == "" exercises the SourcePath fallback.
	}
	result := &OrganizeResult{
		NewPath:          filepath.FromSlash("/new/x.mp4"),
		FileName:         "x.mp4",
		InPlaceRenamed:   true,
		OldDirectoryPath: filepath.FromSlash("/old"),
		NewDirectoryPath: filepath.FromSlash("/new"),
	}

	collision := fmt.Errorf("%w", fsutil.ErrPublishCollision)
	err := strategy.finishInPlaceInnerRename(plan, result, collision)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "survived")
	assert.True(t, poison.fired.Load(), "the rollback rename was attempted and refused")

	// The empty-Name fallback names the survivor by its SOURCE basename inside
	// the renamed directory — exactly where the bytes sit on disk.
	assert.Equal(t, "/new/original.mkv", filepath.ToSlash(result.NewPath))
	assert.Equal(t, "original.mkv", result.FileName)
	assert.True(t, result.InPlaceRenamed, "the surviving rename marker stands")

	exists, _ := afero.Exists(base, filepath.FromSlash("/new/original.mkv"))
	assert.True(t, exists)
}

var _ = errors.New
