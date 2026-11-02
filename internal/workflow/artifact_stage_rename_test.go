//go:build !windows

package workflow

import (
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
