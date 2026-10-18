package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

type lateSweepIdentityFailureFS struct {
	afero.Fs
	root           string
	proofOpened    bool
	proofProbes    int
	refused        bool
	removeAttempts int
}

func (f *lateSweepIdentityFailureFS) Open(path string) (afero.File, error) {
	if path == artifactStageProofPath(f.root) {
		f.proofOpened = true
	}
	return f.Fs.Open(path)
}

func (f *lateSweepIdentityFailureFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if filepath.Clean(path) == filepath.Clean(f.root) && f.proofOpened {
		f.proofProbes++
		if f.proofProbes == 2 {
			f.refused = true
			return nil, false, errors.New("final directory identity unavailable")
		}
	}
	if l, ok := f.Fs.(afero.Lstater); ok {
		return l.LstatIfPossible(path)
	}
	info, err := f.Fs.Stat(path)
	return info, false, err
}

func (f *lateSweepIdentityFailureFS) RemoveAll(path string) error {
	f.removeAttempts++
	return f.Fs.RemoveAll(path)
}

type quarantineRootRenameFailureFS struct {
	afero.Fs
	root     string
	attempts int
}

func (f *quarantineRootRenameFailureFS) Rename(old, next string) error {
	if old == f.root {
		f.attempts++
		return errors.New("quarantine rename denied")
	}
	return f.Fs.Rename(old, next)
}

func TestCleanupRetainsRootWhenQuarantineRenameDenied(t *testing.T) {
	base := afero.NewMemMapFs()
	root := seedStagingRoot(t, base, "/library", nil)
	setSweepSeams(t, fsutil.ProcessDead, nil)
	fs := &quarantineRootRenameFailureFS{Fs: base, root: root}
	stage := &artifactStage{fs: fs, root: root}
	stage.cleanup()
	assert.Equal(t, 1, fs.attempts)
	assert.Equal(t, root, stage.root)
	body, err := afero.ReadFile(base, filepath.Join(root, ".source", "MIRD-281.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(body))
	assert.True(t, readArtifactStageProof(base, root))
	sweepArtifactStaging(base, "/library")
	exists, err := afero.DirExists(base, root)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSweepRetainsNativeRootWhenFinalIdentityLookupFails(t *testing.T) {
	base := afero.NewOsFs()
	parent := t.TempDir()
	root := seedStagingRoot(t, base, parent, nil)
	quarantined := artifactStageQuarantineName(root)
	require.NoError(t, base.Rename(root, quarantined))
	setSweepSeams(t, fsutil.ProcessDead, nil)
	fs := &lateSweepIdentityFailureFS{Fs: base, root: quarantined}
	sweepArtifactStaging(fs, parent)
	require.True(t, fs.refused)
	assert.Equal(t, 2, fs.proofProbes)
	assert.Zero(t, fs.removeAttempts)
	body, err := afero.ReadFile(base, filepath.Join(quarantined, ".source", "MIRD-281.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(body))
	assert.True(t, readArtifactStageProof(base, quarantined))
	sweepArtifactStaging(base, parent)
	exists, err := afero.DirExists(base, quarantined)
	require.NoError(t, err)
	assert.False(t, exists)
}
