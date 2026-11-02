//go:build !windows

package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// renameUnstableFS models a filesystem that derives a directory's inode number
// from its path: a rename mints a new inode, and renaming back to a known path
// restores the one that path always had. mergerfs does this with the default
// inodecalc=hybrid-hash whenever the tree is reached through a container bind
// mount, which made every cleanup rename its own staging root and then refuse
// to delete it.
type renameUnstableFS struct {
	afero.Fs
	mu       sync.Mutex
	saved    map[string]uint64
	known    map[string]bool
	sequence uint64
}

func newRenameUnstableFS(base afero.Fs) *renameUnstableFS {
	return &renameUnstableFS{Fs: base, saved: map[string]uint64{}, known: map[string]bool{}}
}

func (f *renameUnstableFS) identity(name string) (os.FileInfo, error) {
	info, err := f.Fs.Stat(name)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info, nil
	}
	key := filepath.Clean(name)
	clone := *stat
	f.mu.Lock()
	f.known[key] = true
	if ino, minted := f.saved[key]; minted {
		clone.Ino = ino
	}
	f.mu.Unlock()
	return sysStatInfo{info, &clone}, nil
}

func (f *renameUnstableFS) Stat(name string) (os.FileInfo, error) { return f.identity(name) }

func (f *renameUnstableFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	info, err := f.identity(name)
	return info, true, err
}

func (f *renameUnstableFS) Rename(oldname, newname string) error {
	if err := f.Fs.Rename(oldname, newname); err != nil {
		return err
	}
	key := filepath.Clean(newname)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.known[key] {
		return nil
	}
	f.sequence++
	f.saved[key] = 1_000_000_000 + f.sequence
	f.known[key] = true
	return nil
}

func TestSweepArtifactStagingReclaimsWhenRenameChangesIdentity(t *testing.T) {
	parent := t.TempDir()
	fs := newRenameUnstableFS(afero.NewOsFs())
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, parent, nil)

	sweepArtifactStaging(fs, parent)

	exists, _ := afero.DirExists(fs, root)
	assert.False(t, exists, "an abandoned staging root must be reclaimed even when the rename changes its identity")
	entries, err := afero.ReadDir(fs, parent)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), artifactStageDirPrefix, "no staging residue left behind")
	}
}

func TestArtifactStageCleanupReclaimsWhenRenameChangesIdentity(t *testing.T) {
	parent := t.TempDir()
	fs := newRenameUnstableFS(afero.NewOsFs())
	root := seedStagingRoot(t, fs, parent, nil)
	stage := &artifactStage{fs: fs, root: root, finalRoot: filepath.Join(parent, "MIDV-516")}

	stage.cleanup()

	exists, _ := afero.DirExists(fs, root)
	assert.False(t, exists, "cleanup must delete the staged tree when the rename changes its identity")
	entries, err := afero.ReadDir(fs, parent)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), artifactStageDirPrefix, "no quarantine residue left behind")
	}
}

type refusingRenameFS struct{ afero.Fs }

func (f refusingRenameFS) Rename(string, string) error { return errors.New("rename refused") }

type copyingRenameFS struct{ afero.Fs }

func (f copyingRenameFS) Rename(oldname, newname string) error {
	if err := f.Fs.Rename(oldname, newname); err != nil {
		return err
	}
	return f.Fs.MkdirAll(oldname, 0o755)
}

type mintOnRenameFS struct{ *renameUnstableFS }

func (f mintOnRenameFS) Rename(oldname, newname string) error {
	if err := f.renameUnstableFS.Fs.Rename(oldname, newname); err != nil {
		return err
	}
	key := filepath.Clean(newname)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sequence++
	f.saved[key] = 2_000_000_000 + f.sequence
	f.known[key] = true
	return nil
}

func TestArtifactStageRestoreRefusesInvalidInput(t *testing.T) {
	assert.False(t, func() bool {
		_, ok := artifactStageRestoreOriginalAfterIdentityDrift(nil, "/q", "/o")
		return ok
	}(), "a nil filesystem cannot be restored")

	fs := afero.NewMemMapFs()
	for _, tc := range []struct{ quarantined, original string }{
		{"", "/o"},
		{"/q", ""},
	} {
		_, ok := artifactStageRestoreOriginalAfterIdentityDrift(fs, tc.quarantined, tc.original)
		assert.False(t, ok, "empty staging names must refuse")
	}
}

func TestArtifactStageRestoreRefusesWhenRenameFails(t *testing.T) {
	base := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, base, "/lib", nil)
	quarantined := root + artifactStageQuarantineMark + "deadbeef"
	require.NoError(t, base.Rename(root, quarantined))

	_, ok := artifactStageRestoreOriginalAfterIdentityDrift(refusingRenameFS{Fs: base}, quarantined, root)

	assert.False(t, ok, "a refused rename leaves the identity unproven")
}

func TestArtifactStageRestoreRefusesWhenQuarantineNameSurvives(t *testing.T) {
	base := afero.NewMemMapFs()
	root := seedStagingRoot(t, base, "/lib", nil)
	quarantined := root + artifactStageQuarantineMark + "feedface"
	require.NoError(t, base.Rename(root, quarantined))

	_, ok := artifactStageRestoreOriginalAfterIdentityDrift(copyingRenameFS{Fs: base}, quarantined, root)

	assert.False(t, ok, "a rename that leaves the quarantine entry behind is not a restore")
}

func TestArtifactStageRestoreRefusesDifferentRestoredIdentity(t *testing.T) {
	parent := t.TempDir()
	base := newRenameUnstableFS(afero.NewOsFs())
	root := seedStagingRoot(t, base, parent, nil)
	quarantined := root + artifactStageQuarantineMark + "0011aabb"
	require.NoError(t, base.Rename(root, quarantined))

	_, ok := artifactStageRestoreOriginalAfterIdentityDrift(mintOnRenameFS{renameUnstableFS: base}, quarantined, root)

	assert.False(t, ok, "a restored identity that differs from the sealed proof must refuse")
	restoredBack, err := afero.DirExists(base, quarantined)
	require.NoError(t, err)
	assert.True(t, restoredBack, "the refused tree goes back under the quarantine name")
}
