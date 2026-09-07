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
// Edges of the pre-Remove alias exemption: nil occupant (probe observed the
// name vacant), nil src snapshot (source lookup deferred/failed), a symlink
// SOURCE OBJECT (never an alias of a regular-inode occupant per
// classifyExistingDestination's symlink-over-name rule), and differing
// inodes all exclude; same-inode positively proves alias.
func TestLinkInstallOccupantIsAlias_GuardBranches(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/in/other.txt", []byte("other"), 0o644))

	srcInfo, err := base.Stat("/in/src.txt")
	require.NoError(t, err)
	otherInfo, err := base.Stat("/in/other.txt")
	require.NoError(t, err)

	assert.False(t, linkInstallOccupantIsSnapshotAlias(srcInfo, nil),
		"nil occupant (vacant probe) never excludes")
	assert.False(t, linkInstallOccupantIsSnapshotAlias(nil, srcInfo),
		"nil source snapshot (lookup failed/deferred) never excludes foreign bytes")
	assert.False(t, linkInstallOccupantIsSnapshotAlias(srcInfo, otherInfo))

	symlinkFs := &srcSymlinkLstatFs{Fs: base, linkPath: "/in/src-link.txt", target: srcInfo}
	symlinkSnapshot, _, err := symlinkFs.LstatIfPossible("/in/src-link.txt")
	require.NoError(t, err)
	require.NotNil(t, symlinkSnapshot)
	assert.False(t, linkInstallOccupantIsSnapshotAlias(symlinkSnapshot, srcInfo),
		"symlink source object never aliases the occupant inode")

}

func TestLinkInstallOccupant_SnapshotAliasPositiveOnHardlink(t *testing.T) {
	if testing.Short() {
		t.Skip("OsFs hardlink fixture")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	require.NoError(t, os.WriteFile(src, []byte("shared bytes"), 0o644))
	require.NoError(t, os.Link(src, dst))

	srcInfo, err := os.Lstat(src)
	require.NoError(t, err)
	occInfo, err := os.Lstat(dst)
	require.NoError(t, err)

	assert.True(t, linkInstallOccupantIsSnapshotAlias(srcInfo, occInfo),
		"positively-proven same-inode pairing (pre-frozen) excludes foreign crumb")
}

// nonLstaterFs masks afero.Lstater so the link lane drops to the Stat fallback,
// which covers the other side of the pre-Remove snapshot source lookup.
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

var errInjectedAbs = errors.New("injected absolute-resolution failure")

func TestForceOverwriteAudit_LinkSoft_AbsolutizationFailureIsClean(t *testing.T) {
	strategy, fs, _ := forceAuditStrategy(t, &MemLinker{})
	plan := forceAuditLinkPlan("uploads/incoming-01.mkv", "/dest/ABC-123/ABC-123.mkv", LinkModeSoft, true)

	orig := filepathAbsFn
	filepathAbsFn = func(string) (string, error) { return "", errInjectedAbs }
	t.Cleanup(func() { filepathAbsFn = orig })

	result, err := strategy.Execute(plan)
	require.Error(t, err, "an injected absolutization failure must surface")
	require.ErrorContains(t, err, "failed to resolve source path for symlink")
	require.ErrorIs(t, err, errInjectedAbs)
	require.NotNil(t, result)
	assert.False(t, result.Moved, "a doomed source path may not settle")
	_, statErr := fs.Stat(plan.TargetPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "nothing written at the destination")
}
