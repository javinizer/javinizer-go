package organizer

// PR #249 codex follow-up (F1 + F2) — link-install crumb discipline.
//
// F1: the alias-vs-foreign exclusion must key on the LstatIfPossible taken
// IMMEDIATELY pre-Remove, never the lane-head classification. The lane-head
// answer can be raced stale across the interim MkdirAll and the regular-file
// refusal gate:
//   - a lane-head ALIAS swapped for a FOREIGN plant before the Remove is
//     destroyed-by-Remove — the crumb must fire (pre-fix: suppressed by the
//     stale lane-head alias answer, silently destroying foreign bytes);
//   - a lane-head FOREIGN occupant swapped for a same-inode alias of the
//     source destroys NO foreign bytes — the crumb must stay silent (pre-fix:
//     fired on the stale lane-head foreign answer).
// Both swaps ride a wedge fs whose MkdirAll(targetDir) performs the swap
// exactly at the classification → probe boundary.
//
// F2: an authorized Remove that succeeded but whose link install then FAILED
// (hardlink EXDEV / softlink refusal) leaves the resident bytes destroyed
// forever — the FAILED result must still carry the displacement crumb, the
// partial-publish lineage of fsutil.MoveFileFsDestReplaced's
// ErrPublishCompleted discipline (pre-fix: the crumb was bound at install
// success and died with the error).

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingInstallLinker is a MemLinker whose installs all fail — the
// authorized Remove around it still runs, modeling the hardlink-EXDEV /
// softlink-refusal lanes whose resident displacement survives the failure.
type failingInstallLinker struct {
	MemLinker
	err error
}

func (f *failingInstallLinker) hardlink(_, _ string) error { return f.err }
func (f *failingInstallLinker) symlink(_, _ string) error  { return f.err }

// failingCopyLinker fails the authorized copy lane while reporting the
// fsutil F3 unioned displacement evidence (a staged publish landed and
// displaced the resident, then failed post-publish).
type failingCopyLinker struct {
	MemLinker
	destReplaced bool
	err          error
}

func (f *failingCopyLinker) copyFile(_ afero.Fs, _, _ string) (bool, error) {
	return f.destReplaced, f.err
}

// TestForceOverwriteAudit_CopyLaneFailure_KeepsUnionedDisplacementCrumb pins
// the copy lane's consumption of fsutil's F3 union: a copy whose publish
// displaced the resident but then failed post-publish carries the crumb on
// the FAILED result, same destruction-bound discipline as the link lane.
func TestForceOverwriteAudit_CopyLaneFailure_KeepsUnionedDisplacementCrumb(t *testing.T) {
	const dst = "/dest/ABC-123/ABC-123.mkv"
	l := &failingCopyLinker{destReplaced: true, err: errors.New("post-publish identity break")}
	strategy, fs, src := forceAuditStrategy(t, l)
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))

	result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeNone, true))
	require.Error(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Moved)
	require.Len(t, result.Warnings, 1,
		"the publish displaced the resident before failing — the unioned evidence rides the FAILED result")
	assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
}

// TestLinkInstallOccupantIsAlias_GuardBranches pins the confirm-or-silent
// edges of the pre-Remove alias exclusion: a nil occupant (the probe saw the
// name vacant), a source lookup failure, and a symlink SOURCE OBJECT (never
// an alias of a regular-inode occupant) all answer false, while matching and
// differing inodes answer the os.SameFile truth.
func TestLinkInstallOccupantIsAlias_GuardBranches(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/in/other.txt", []byte("other"), 0o644))

	srcInfo, err := base.Stat("/in/src.txt")
	require.NoError(t, err)
	otherInfo, err := base.Stat("/in/other.txt")
	require.NoError(t, err)

	assert.False(t, linkInstallOccupantIsAlias(base, "/in/src.txt", nil),
		"nil occupant (vacant pre-Remove probe) never claims alias exclusion")
	assert.False(t, linkInstallOccupantIsAlias(base, "/in/missing.txt", srcInfo),
		"a failed source lookup answers false — confirm-or-silent")
	// MemLinker's mem filesystems: SameFile on MemMapFs compares the recorded
	// objects; a distinct entry is no alias.
	assert.False(t, linkInstallOccupantIsAlias(base, "/in/src.txt", otherInfo))

	// Symlink SOURCE OBJECT: even where the chase would reach the occupant's
	// inode, the symlink's own lookup is never the destination's name-bearing
	// link. MemMapFs has no symlink primitive, so this leg uses a skinny
	// Lstater wrapper whose LstatIfPossible reports a symlink MODE for the
	// link name (the exact shape a symlink-object probe sees).
	symlinkFs := &srcSymlinkLstatFs{Fs: base, linkPath: "/in/src-link.txt", target: srcInfo}
	linkInfo, err := symlinkFs.Stat("/in/src.txt")
	require.NoError(t, err)
	assert.False(t, linkInstallOccupantIsAlias(symlinkFs, "/in/src-link.txt", linkInfo),
		"a source symlink object is never an alias of the occupant's inode")
}

// srcSymlinkLstatFs wraps an afero fs with an Lstater whose LstatIfPossible
// reports a SYMLINK MODE for the given name — the shape a no-follow probe of
// a symlink source object returns.
type srcSymlinkLstatFs struct {
	afero.Fs
	linkPath string
	target   os.FileInfo
}

func (w *srcSymlinkLstatFs) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if path == w.linkPath {
		return symlinkModeInfo{w.target}, true, nil
	}
	if lst, ok := w.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(path)
	}
	info, err := w.Fs.Stat(path)
	return info, false, err
}

type symlinkModeInfo struct{ os.FileInfo }

func (s symlinkModeInfo) Mode() os.FileMode { return s.FileInfo.Mode() | os.ModeSymlink }

func TestForceOverwriteAudit_LinkInstallFailure_KeepsDisplacementCrumb(t *testing.T) {
	const dst = "/dest/ABC-123/ABC-123.mkv"

	t.Run("force hardlink install fails EXDEV after the resident Remove — failure crumb", func(t *testing.T) {
		l := &failingInstallLinker{err: &os.LinkError{Op: "link", Old: "/in/A.mkv", New: dst, Err: syscall.EXDEV}}
		strategy, fs, src := forceAuditStrategy(t, l)
		require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))

		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeHard, true))
		require.Error(t, err)
		assert.True(t, errors.Is(err, syscall.EXDEV), "the hard-link refusal is the surfaced failure")
		require.NotNil(t, result)
		assert.False(t, result.Moved, "a failed install is never a completed move")
		require.Len(t, result.Warnings, 1,
			"the authorized Remove already destroyed the resident bytes — the crumb rides the FAILED result")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))

		exists, statErr := afero.Exists(fs, dst)
		require.NoError(t, statErr)
		assert.False(t, exists, "the resident entry is gone — the destruction the crumb discloses is real")
		retained, readErr := afero.ReadFile(fs, src)
		require.NoError(t, readErr, "the source is retained on the failed lane (#224 keep-both)")
		assert.Equal(t, []byte("winner-bytes"), retained)
	})

	t.Run("failure crumb never fires when the Remove displaced nothing", func(t *testing.T) {
		l := &failingInstallLinker{err: errors.New("install refused")}
		strategy, fs, src := forceAuditStrategy(t, l)

		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeHard, true))
		require.Error(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result.Warnings,
			"vacant destination — nothing was destroyed, so no crumb rides the failure")
		content, readErr := afero.ReadFile(fs, src)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("force softlink install fails after the resident Remove — failure crumb", func(t *testing.T) {
		l := &failingInstallLinker{err: errors.New("softlink refused")}
		strategy, fs, src := forceAuditStrategy(t, l)
		require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))

		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeSoft, true))
		require.Error(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Warnings, 1,
			"the soft-link lane carries the same destruction-bound crumb")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
	})
}

// linkInstallSwapWedgeFs performs a one-shot destination swap at the exact
// classification → pre-Remove-probe boundary: the lane-head classification
// has already observed the fixture occupant when the strategy's interim
// MkdirAll(targetDir) fires the swap, so only a pre-Remove-bound probe can
// see the swapped object.
type linkInstallSwapWedgeFs struct {
	afero.Fs
	targetDir string
	swap      func() error
	fired     bool
	swapErr   error
}

func (w *linkInstallSwapWedgeFs) MkdirAll(path string, perm os.FileMode) error {
	if !w.fired && filepath.Clean(path) == filepath.Clean(w.targetDir) {
		w.fired = true
		w.swapErr = w.swap()
	}
	return w.Fs.MkdirAll(path, perm)
}

func TestForceOverwriteAudit_LinkInstallAliasExclusion_BoundToPreRemoveProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("os-level hardlink fixture")
	}

	setup := func(t *testing.T) (fs *linkInstallSwapWedgeFs, src, dst string) {
		t.Helper()
		dir := t.TempDir()
		src = filepath.Join(dir, "in", "ABC-123.mkv")
		dst = filepath.Join(dir, "dest", "ABC-123", "ABC-123.mkv")
		require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0o644))
		base := afero.NewOsFs()
		wedge := &linkInstallSwapWedgeFs{Fs: base, targetDir: filepath.Dir(dst)}
		return wedge, src, dst
	}
	strategyFor := func(fs afero.Fs) *organizeStrategy {
		return newOrganizeStrategy(fs, &Config{
			FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
		}, nil, &MemLinker{})
	}

	t.Run("lane-head alias swapped for a FOREIGN plant before the Remove: crumb fires", func(t *testing.T) {
		wedge, src, dst := setup(t)
		require.NoError(t, os.Link(src, dst),
			"lane-head classification observes a hardlink ALIAS — the stale answer would suppress the crumb")
		// Swap at the classification → probe boundary: the alias is replaced
		// by FOREIGN resident bytes moments before the pre-Remove probe.
		wedge.swap = func() error {
			if err := os.Remove(dst); err != nil {
				return err
			}
			return os.WriteFile(dst, []byte("foreign-plant"), 0o644)
		}

		result, err := strategyFor(wedge).Execute(forceAuditLinkPlan(src, dst, LinkModeHard, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.True(t, wedge.fired && wedge.swapErr == nil, "the swap really landed inside the accepted probe window")
		assert.Empty(t, result.Warnings[1:],
			"exactly one warning — the displacement crumb")
		require.Len(t, result.Warnings, 1,
			"the Remove destroyed FOREIGN bytes the lane-head alias answer would have hidden")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
	})

	t.Run("lane-head foreign occupant swapped for a source alias before the Remove: crumb stays silent", func(t *testing.T) {
		wedge, src, dst := setup(t)
		require.NoError(t, os.WriteFile(dst, []byte("foreign-resident"), 0o644),
			"lane-head classification observes a FOREIGN occupant — the stale answer would fire the crumb")
		// Swap: the foreign occupant vacates and the destination now aliases
		// the source's own inode — its unlink destroys no foreign bytes.
		wedge.swap = func() error {
			if err := os.Remove(dst); err != nil {
				return err
			}
			return os.Link(src, dst)
		}

		result, err := strategyFor(wedge).Execute(forceAuditLinkPlan(src, dst, LinkModeHard, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.True(t, wedge.fired && wedge.swapErr == nil, "the swap really landed inside the accepted probe window")
		assert.Empty(t, result.Warnings,
			"removing a same-inode alias at Remove time destroyed no foreign bytes — the crumb stays silent")
	})
}
