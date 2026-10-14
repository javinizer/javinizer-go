package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type weakLinkedDestinationLookupFS struct {
	afero.Fs
	source string
}

func (f *weakLinkedDestinationLookupFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	info, err := f.Fs.Stat(path)
	if err == nil && path != f.source {
		info = syslessInfo{FileInfo: info}
	}
	return info, false, err
}

func TestRejectedNativeHardLinkUnlinksOnlyAdmittedObject(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src, dst := filepath.Join(root, "source.mp4"), filepath.Join(root, "installed.mp4")
	require.NoError(t, afero.WriteFile(fs, src, []byte("owned bytes"), 0o600))
	file, err := fs.Open(src)
	require.NoError(t, err)
	info, err := file.Stat()
	require.NoError(t, err)
	dev, ino, strong := streamedHandleIdentity(file, info)
	require.True(t, strong)
	require.NoError(t, file.Close())
	admission := VerifiedSourceProofFromBoundInstallIdentity(fs, newBoundInstallIdentity(info, dev, ino, strong))
	proof := func(path string, info os.FileInfo) error {
		if path == dst {
			return errors.New("post-link admission rejected")
		}
		return admission(path, info)
	}
	installed, err := LinkFileNoReplaceVerifiedInstall(fs, src, dst, os.Link, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.NotErrorIs(t, err, ErrPublishCompleted)
	require.NotErrorIs(t, err, ErrPublishSuccessorUnproven)
	assert.Nil(t, installed)
	_, err = fs.Stat(dst)
	require.ErrorIs(t, err, os.ErrNotExist)
	body, err := afero.ReadFile(fs, src)
	require.NoError(t, err)
	assert.Equal(t, "owned bytes", string(body))
}

func TestRejectedNativeHardLinkRetainsSameMetadataSuccessor(t *testing.T) {
	fs := afero.NewOsFs()
	parent := t.TempDir()
	src, dst := filepath.Join(parent, "source.mp4"), filepath.Join(parent, "installed.mp4")
	require.NoError(t, afero.WriteFile(fs, src, []byte("owned bytes"), 0o600))
	file, err := fs.Open(src)
	require.NoError(t, err)
	info, err := file.Stat()
	require.NoError(t, err)
	dev, ino, strong := streamedHandleIdentity(file, info)
	require.True(t, strong)
	require.NoError(t, file.Close())
	proof := VerifiedSourceProofFromBoundInstallIdentity(fs, newBoundInstallIdentity(info, dev, ino, strong))
	installed, err := LinkFileNoReplaceVerifiedInstall(fs, src, dst, func(old, next string) error {
		if err := os.Link(old, next); err != nil {
			return err
		}
		if err := fs.Rename(next, next+".retained"); err != nil {
			return err
		}
		if err := afero.WriteFile(fs, next, []byte("alien bytes"), 0o600); err != nil {
			return err
		}
		return fs.Chtimes(next, info.ModTime(), info.ModTime())
	}, proof)
	require.ErrorIs(t, err, ErrPublishCompleted)
	require.ErrorIs(t, err, ErrPublishSuccessorUnproven)
	assert.Nil(t, installed)
	body, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	assert.Equal(t, "alien bytes", string(body))
	body, err = afero.ReadFile(fs, dst+".retained")
	require.NoError(t, err)
	assert.Equal(t, "owned bytes", string(body))
}

func TestRejectedNativeHardLinkRetainsSuccessorWithWeakLookup(t *testing.T) {
	base := afero.NewOsFs()
	parent := t.TempDir()
	src, dst := filepath.Join(parent, "source.mp4"), filepath.Join(parent, "installed.mp4")
	require.NoError(t, afero.WriteFile(base, src, []byte("owned bytes"), 0o600))
	file, err := base.Open(src)
	require.NoError(t, err)
	info, err := file.Stat()
	require.NoError(t, err)
	dev, ino, strong := streamedHandleIdentity(file, info)
	require.True(t, strong)
	require.NoError(t, file.Close())
	fs := &weakLinkedDestinationLookupFS{Fs: base, source: src}
	proof := VerifiedSourceProofFromBoundInstallIdentity(fs, newBoundInstallIdentity(info, dev, ino, strong))
	installed, err := LinkFileNoReplaceVerifiedInstall(fs, src, dst, func(old, next string) error {
		if err := os.Link(old, next); err != nil {
			return err
		}
		if err := base.Rename(next, next+".retained"); err != nil {
			return err
		}
		if err := afero.WriteFile(base, next, []byte("alien bytes"), 0o600); err != nil {
			return err
		}
		return base.Chtimes(next, info.ModTime(), info.ModTime())
	}, proof)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPublishCompleted))
	assert.Nil(t, installed)
	body, readErr := afero.ReadFile(base, dst)
	require.NoError(t, readErr)
	assert.Equal(t, "alien bytes", string(body))
	body, readErr = afero.ReadFile(base, dst+".retained")
	require.NoError(t, readErr)
	assert.Equal(t, "owned bytes", string(body))
}
