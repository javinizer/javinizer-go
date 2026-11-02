//go:build !windows

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renameUnstableFS models a filesystem that derives a directory's inode number
// from its path, so a rename mints a new inode for the same object. mergerfs
// does this with the default inodecalc=hybrid-hash whenever the tree is
// reached through a container bind mount.
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

func TestArtifactStageCleanupRetainsProofWhenRenameChangesIdentity(t *testing.T) {
	parent := t.TempDir()
	fs := newRenameUnstableFS(afero.NewOsFs())
	root := seedStagingRoot(t, fs, parent, nil)
	stage := &artifactStage{fs: fs, root: root, finalRoot: filepath.Join(parent, "MIDV-516")}

	stage.cleanup()

	entries, err := afero.ReadDir(fs, parent)
	require.NoError(t, err)
	quarantined := ""
	for _, entry := range entries {
		if entry.IsDir() && strings.Contains(entry.Name(), artifactStageQuarantineMark) {
			quarantined = filepath.Join(parent, entry.Name())
		}
	}
	require.NotEmpty(t, quarantined, "a drifted tree must stay under a quarantine name, not be deleted")

	exists, err := afero.DirExists(fs, quarantined)
	require.NoError(t, err)
	assert.True(t, exists, "a tree whose identity cannot be re-proven must be retained, never deleted")

	_, proofErr := fs.Stat(quarantined + ".proof")
	assert.NoError(t, proofErr,
		"the completed proof must survive retention so a later cleanup can still reclaim the tree")
}
