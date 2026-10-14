package fsutil

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Round-50 coverage: the post-publish indeterminate asideLstat leg on
// copyStreamNoReplaceInstallReproof — a destination that reports a non-NotExist
// lookup error after PublishStagedBoundInfo returns a nil identity (virtual
// leg) is classified as a publish-completed doubt row, never silently dropped.

func TestCoverMoveNoReplaceVerifiedInstall_IndeterminateInstalledLookup(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	require.NoError(t, fs.MkdirAll("/out", 0o755))
	wrapped := &lstatIndeterminateOnDestCreateFs{Fs: fs, victim: "/out/x.mp4"}
	proof := VerifiedSourceProof(func(src string, info os.FileInfo) error { return nil })
	_, err := CopyFileNoReplaceVerifiedInstall(wrapped, "/in/a.mp4", "/out/x.mp4", proof)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPublishCompleted), "the indeterminate lookup ascribes to the publish-completed doubt class")
	assert.Contains(t, err.Error(), "indeterminate")
}

type lstatIndeterminateOnDestCreateFs struct {
	afero.Fs
	victim string
}

func (f *lstatIndeterminateOnDestCreateFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		// Only fail once the staged rename created the destination — otherwise
		// the pre-create probe would misclassify the destination as occupied.
		if _, err := f.Fs.Stat(name); err == nil {
			return nil, false, errors.New("simulated indeterminate lstat")
		}
	}
	return f.Fs.(interface {
		LstatIfPossible(string) (os.FileInfo, bool, error)
	}).LstatIfPossible(name)
}
