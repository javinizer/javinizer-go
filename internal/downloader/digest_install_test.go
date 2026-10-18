package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

type digestInstallTestFS struct {
	afero.Fs
	openErr    error
	beforeOpen func()
	wrap       func(afero.File) afero.File
	openPath   string
}

func (f *digestInstallTestFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func (f *digestInstallTestFS) Open(path string) (afero.File, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	if f.beforeOpen != nil {
		f.beforeOpen()
	}
	if f.openPath != "" {
		path = f.openPath
	}
	file, err := restoreOpenReplacementSource(f.Fs, path)
	if err == nil && f.wrap != nil {
		file = f.wrap(file)
	}
	return file, err
}

type digestInstallTestFile struct {
	afero.File
	stat       func() (os.FileInfo, error)
	beforeRead func()
	readErr    error
	invalidFD  bool
}

func (f *digestInstallTestFile) Stat() (os.FileInfo, error) {
	if f.stat != nil {
		return f.stat()
	}
	return f.File.Stat()
}

func (f *digestInstallTestFile) Read(p []byte) (int, error) {
	if f.beforeRead != nil {
		callback := f.beforeRead
		f.beforeRead = nil
		callback()
	}
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.File.Read(p)
}

func (f *digestInstallTestFile) Fd() uintptr {
	if f.invalidFD {
		return ^uintptr(0)
	}
	if fd, ok := f.File.(interface{ Fd() uintptr }); ok {
		return fd.Fd()
	}
	return ^uintptr(0)
}

type digestInstallTestInfo struct {
	os.FileInfo
	mode os.FileMode
	size int64
}

func (i digestInstallTestInfo) Mode() os.FileMode { return i.mode }
func (i digestInstallTestInfo) Size() int64       { return i.size }

type digestInstallMissingKeyInfo struct{ os.FileInfo }

func (digestInstallMissingKeyInfo) Sys() interface{} { return nil }

func TestDigestVerifiedInstall(t *testing.T) {
	cases := []string{"strong", "weak", "nil-identity", "link-identity", "open-error", "stat-error", "nil-info", "nonregular", "handle-successor", "handle-aba", "missing-handle-key", "weak-successor", "metadata-change", "read-error", "post-stat-error", "post-read-successor"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			base := afero.NewOsFs()
			path := filepath.Join(t.TempDir(), "output")
			own := []byte("owneddata")
			if name == "post-read-successor" && runtime.GOOS == "windows" {
				t.Skip("Windows no-follow source handles prevent destination renames during the read")
			}
			source := path + ".input"
			require.NoError(t, afero.WriteFile(base, source, own, 0o600))
			installed, err := fsutil.CopyFileNoReplaceVerifiedInstall(base, source, path, func(string, os.FileInfo) error { return nil })
			require.NoError(t, err)
			info, err := base.Stat(path)
			require.NoError(t, err)
			_, _, strong := fsutil.BoundObjectIdentity(base, path, info)
			if !strong {
				t.Skip("native strong identity unavailable")
			}
			fault := errors.New("injected digest fault")
			fs := &digestInstallTestFS{Fs: base}
			swap := func(payload []byte) {
				require.NoError(t, base.Rename(path, path+".owned"))
				require.NoError(t, afero.WriteFile(base, path, payload, 0o600))
				require.NoError(t, base.Chtimes(path, info.ModTime(), info.ModTime()))
			}
			if name == "weak" || name == "weak-successor" {
				installed = fsutil.NewWeakBoundInstallIdentity(info)
			}
			if name == "link-identity" {
				installed = fsutil.NewBoundInstallLinkIdentity(nil, path+".target")
			}
			if name == "nil-identity" {
				installed = nil
			}
			if name == "open-error" {
				fs.openErr = fault
			}
			if name == "handle-aba" {
				foreign := path + ".foreign"
				require.NoError(t, afero.WriteFile(base, foreign, []byte("foreign!!"), 0o600))
				require.NoError(t, base.Chtimes(foreign, info.ModTime(), info.ModTime()))
				fs.openPath = foreign
			}
			if name == "handle-successor" {
				fs.beforeOpen = func() { swap([]byte("foreign!!")) }
			}
			if name == "weak-successor" {
				fs.beforeOpen = func() { swap([]byte("foreign longer payload")) }
			}
			fs.wrap = func(file afero.File) afero.File {
				wrapped := &digestInstallTestFile{File: file, invalidFD: name == "missing-handle-key"}
				calls := 0
				wrapped.stat = func() (os.FileInfo, error) {
					calls++
					if name == "stat-error" || name == "post-stat-error" && calls == 2 {
						return nil, fault
					}
					if name == "nil-info" {
						return nil, nil
					}

					current, err := file.Stat()
					if err != nil {
						return nil, err
					}
					if name == "missing-handle-key" {
						return digestInstallMissingKeyInfo{FileInfo: current}, nil
					}
					if name == "nonregular" {
						return digestInstallTestInfo{FileInfo: current, mode: os.ModeDir, size: current.Size()}, nil
					}
					if name == "metadata-change" {
						return digestInstallTestInfo{FileInfo: current, mode: current.Mode(), size: current.Size() + 1}, nil
					}
					return current, nil
				}
				if name == "read-error" {
					wrapped.readErr = fault
				}
				if name == "post-read-successor" {
					wrapped.beforeRead = func() { swap([]byte("foreign!!")) }
				}
				return wrapped
			}
			digest, err := digestVerifiedInstall(fs, path, installed)
			if name == "strong" || name == "weak" {
				require.NoError(t, err)
				sum := sha256.Sum256(own)
				require.Equal(t, hex.EncodeToString(sum[:]), digest)
			} else {
				require.Error(t, err)
				require.Empty(t, digest)
			}
			if name == "handle-aba" {
				payload, err := afero.ReadFile(base, path)
				require.NoError(t, err)
				require.Equal(t, own, payload)
				payload, err = afero.ReadFile(base, path+".foreign")
				require.NoError(t, err)
				require.Equal(t, "foreign!!", string(payload))
			}
		})
	}
}
