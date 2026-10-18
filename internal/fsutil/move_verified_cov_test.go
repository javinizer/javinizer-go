package fsutil

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Round-50 coverage: CopyFileNoReplaceVerifiedInstall defensive legs —
// mkdir failure, openVerifiedSource failure, and the open-handle Stat error.

// move_verified.go:244-245 — destination directory creation refuses.
func TestCoverCopyVerifiedInstall_MkdirAllFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	wrapped := &failFs{Fs: fs, failMkdirAll: "/out"}
	proof := func(src string, info os.FileInfo) error { return nil }
	_, err := CopyFileNoReplaceVerifiedInstall(wrapped, "/in/a.mp4", "/out/x.mp4", proof)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verified copy: create destination directory")
}

// move_verified.go:248-249 — verified source open refuses. The preflight
// probe catches a missing source earlier ("probe source"); to exercise the
// open failure itself we let the lstat probe answer but the Open call fail.
func TestCoverCopyVerifiedInstall_OpenSourceFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	require.NoError(t, fs.MkdirAll("/out", 0o755))
	wrapped := &failFs{Fs: fs, failOpen: "/in/a.mp4"}
	proof := func(src string, info os.FileInfo) error { return nil }
	_, err := CopyFileNoReplaceVerifiedInstall(wrapped, "/in/a.mp4", "/out/x.mp4", proof)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verified copy: open source")
}

// move_verified.go:265-266 — the opened source handle Stat fails.
type wCovStatFailFile struct {
	afero.File
	err error
}

func (f *wCovStatFailFile) Stat() (os.FileInfo, error) { return nil, f.err }

type wCovStatFailFS struct {
	afero.Fs
	target string
	err    error
}

func (s *wCovStatFailFS) Open(name string) (afero.File, error) {
	f, err := s.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if name == s.target {
		return &wCovStatFailFile{File: f, err: s.err}, nil
	}
	return f, nil
}

func TestCoverCopyVerifiedInstall_SrcStatFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	require.NoError(t, fs.MkdirAll("/out", 0o755))
	wrapped := &wCovStatFailFS{Fs: fs, target: "/in/a.mp4", err: errors.New("stat denied")}
	proof := func(src string, info os.FileInfo) error { return nil }
	_, err := CopyFileNoReplaceVerifiedInstall(wrapped, "/in/a.mp4", "/out/x.mp4", proof)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inspect the open source handle")
}

// move_verified.go:267-269 — admission proof refuses the verified handle.
func TestCoverCopyVerifiedInstall_ProofRejectsHandle(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0o644))
	require.NoError(t, fs.MkdirAll("/out", 0o755))
	proof := func(src string, info os.FileInfo) error { return errors.New("not our file") }
	_, err := CopyFileNoReplaceVerifiedInstall(fs, "/in/a.mp4", "/out/x.mp4", proof)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTakeAsideForeign), "admission refusal surfaces the foreign take-aside class")
}
