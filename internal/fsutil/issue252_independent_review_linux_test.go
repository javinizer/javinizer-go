//go:build linux

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type review252Copy func(afero.Fs, string, string, VerifiedSourceProof) (*BoundInstallIdentity, error)

func review252Copies() map[string]review252Copy {
	return map[string]review252Copy{
		"digest-mode": func(fs afero.Fs, s, d string, p VerifiedSourceProof) (*BoundInstallIdentity, error) {
			_, id, err := CopyFileNoReplaceVerifiedDigestMode(fs, s, d, p, 0o400)
			return id, err
		},
		"install": CopyFileNoReplaceVerifiedInstall,
	}
}

func TestReview252SourceMutationDuringProbe(t *testing.T) {
	for name, op := range review252Copies() {
		t.Run(name, func(t *testing.T) {
			for _, mutation := range []string{"rewrite", "swap"} {
				t.Run(mutation, func(t *testing.T) {
					root := t.TempDir()
					src, dst := filepath.Join(root, "src"), filepath.Join(root, "out", "dst")
					original := []byte("admitted bytes")
					foreign := []byte("foreign replacement with a different length")
					require.NoError(t, os.WriteFile(src, original, 0o600))
					admitted, err := os.Stat(src)
					require.NoError(t, err)
					proof := func(_ string, fi os.FileInfo) error {
						if !os.SameFile(admitted, fi) {
							return errors.New("foreign source")
						}
						return nil
					}
					oldProbe := publishNoClobberProbe
					t.Cleanup(func() { publishNoClobberProbe = oldProbe })
					probes := 0
					publishNoClobberProbe = func(fs afero.Fs, s, d string) error {
						probes++
						if mutation == "swap" {
							require.NoError(t, os.Rename(src, src+".admitted"))
						}
						require.NoError(t, os.WriteFile(src, foreign, 0o600))
						return oldProbe(fs, s, d)
					}
					installed, err := op(afero.NewOsFs(), src, dst, proof)
					require.Equal(t, 1, probes)
					got, re := os.ReadFile(src)
					require.NoError(t, re)
					require.Equal(t, foreign, got)
					if mutation == "rewrite" {
						require.ErrorIs(t, err, ErrTakeAsideForeign)
						require.Nil(t, installed)
						require.NoFileExists(t, dst)
						names, re := os.ReadDir(filepath.Dir(dst))
						require.NoError(t, re)
						require.Empty(t, names)
					} else {
						require.NoError(t, err)
						require.NotNil(t, installed)
						got, re = os.ReadFile(dst)
						require.NoError(t, re)
						require.Equal(t, original, got)
						if name == "digest-mode" {
							fi, re := os.Stat(dst)
							require.NoError(t, re)
							require.Equal(t, os.FileMode(0o400), fi.Mode().Perm())
						}
					}
				})
			}
		})
	}
}

func TestReview252ParentRetargetDuringProbe(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		t.Run(map[bool]string{false: "capable-new-parent", true: "unsupported-new-parent"}[unsupported], func(t *testing.T) {
			root := t.TempDir()
			a, b, link := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "out")
			require.NoError(t, os.Mkdir(a, 0o700))
			require.NoError(t, os.Mkdir(b, 0o700))
			require.NoError(t, os.Symlink(a, link))
			src, dst := filepath.Join(root, "src"), filepath.Join(link, "dst")
			original := []byte("admitted source bytes")
			require.NoError(t, os.WriteFile(src, original, 0o600))
			admitted, err := os.Stat(src)
			require.NoError(t, err)
			proof := func(_ string, fi os.FileInfo) error { require.True(t, os.SameFile(admitted, fi)); return nil }
			oldProbe := publishNoClobberProbe
			t.Cleanup(func() { publishNoClobberProbe = oldProbe })
			calls := 0
			planted := ""
			retained := ""
			publishNoClobberProbe = func(fs afero.Fs, s, d string) error {
				calls++
				if calls == 1 {
					require.NoError(t, oldProbe(fs, s, d))
					retained = filepath.Join(a, filepath.Base(d))
					planted = filepath.Join(b, filepath.Base(d))
					require.NoError(t, os.WriteFile(planted, []byte("foreign sentinel"), 0o600))
					require.NoError(t, os.Remove(link))
					require.NoError(t, os.Symlink(b, link))
					return nil
				}
				if unsupported {
					return errors.Join(ErrPublishNoReplaceUnsupported, syscall.EPERM)
				}
				return oldProbe(fs, s, d)
			}
			_, err = CopyFileNoReplaceVerifiedInstall(afero.NewOsFs(), src, dst, proof)
			require.Equal(t, 2, calls, "probe must recheck destination identity after retarget")
			got, re := os.ReadFile(planted)
			require.NoError(t, re)
			require.Equal(t, "foreign sentinel", string(got))
			retainedInfo, re := os.Stat(retained)
			require.NoError(t, re)
			require.Zero(t, retainedInfo.Size(), "retain original scratch rather than delete foreign sibling")
			got, re = os.ReadFile(src)
			require.NoError(t, re)
			require.Equal(t, original, got)
			if unsupported {
				require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
				require.NoFileExists(t, dst)
			} else {
				require.NoError(t, err)
				got, re = os.ReadFile(dst)
				require.NoError(t, re)
				require.Equal(t, original, got)
			}
			names, re := os.ReadDir(b)
			require.NoError(t, re)
			for _, name := range names {
				require.False(t, strings.Contains(name.Name(), ".nrstg"))
			}
		})
	}
}

func TestReview252AdmissionPrecedesProbe(t *testing.T) {
	for name, op := range review252Copies() {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "src"), filepath.Join(root, "out", "dst")
			require.NoError(t, os.WriteFile(src, []byte("source"), 0o600))
			rejection := errors.New("rejected source")
			oldProbe := publishNoClobberProbe
			t.Cleanup(func() { publishNoClobberProbe = oldProbe })
			publishNoClobberProbe = func(afero.Fs, string, string) error {
				t.Fatal("source admission must run before new probe")
				return nil
			}
			installed, err := op(afero.NewOsFs(), src, dst, func(string, os.FileInfo) error { return rejection })
			require.ErrorIs(t, err, rejection)
			require.ErrorIs(t, err, ErrTakeAsideForeign)
			require.Nil(t, installed)
			names, re := os.ReadDir(filepath.Dir(dst))
			require.NoError(t, re)
			require.Empty(t, names)
		})
	}
}

type review252CountFile struct {
	afero.File
	reads int
	bytes int
}

func (f *review252CountFile) Read(b []byte) (int, error) {
	f.reads++
	n, err := f.File.Read(b)
	f.bytes += n
	return n, err
}
func TestReview252NoPayloadReadOnUnsupported(t *testing.T) {
	for _, install := range []bool{false, true} {
		t.Run(map[bool]string{false: "digest-tail", true: "install-tail"}[install], func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "src"), filepath.Join(root, "out", "dst")
			require.NoError(t, os.Mkdir(filepath.Dir(dst), 0o700))
			require.NoError(t, os.WriteFile(src, []byte(strings.Repeat("x", 1024)), 0o600))
			file, err := os.Open(src)
			require.NoError(t, err)
			defer file.Close()
			counted := &review252CountFile{File: file}
			oldRename, oldLink := renameNoReplaceKernel, publishNoReplaceLink
			t.Cleanup(func() { renameNoReplaceKernel, publishNoReplaceLink = oldRename, oldLink })
			renameNoReplaceKernel = func(string, string) error { return syscall.EINVAL }
			publishNoReplaceLink = func(string, string) error { return syscall.EPERM }
			if install {
				_, err = copyStreamNoReplaceInstallReproof(afero.NewOsFs(), counted, dst, nil)
			} else {
				_, _, err = copyStreamNoReplaceDigestReproof(afero.NewOsFs(), counted, dst, nil, 0o600)
			}
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
			t.Logf("source reads=%d source payload bytes=%d", counted.reads, counted.bytes)
			require.Zero(t, counted.bytes, "unsupported destination must consume no source payload bytes")
			require.Zero(t, counted.reads, "unsupported destination must not invoke source read")
			entries, err := os.ReadDir(filepath.Dir(dst))
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestReview252ProbeCompletedDoesNotMeanPayloadPublished(t *testing.T) {
	for name, op := range review252Copies() {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "src"), filepath.Join(root, "out", "dst")
			payload := []byte("unchanged source")
			require.NoError(t, os.WriteFile(src, payload, 0o600))
			admitted, err := os.Stat(src)
			require.NoError(t, err)
			proof := func(_ string, fi os.FileInfo) error { require.True(t, os.SameFile(admitted, fi)); return nil }
			oldRename, oldRemove := renameNoReplaceKernel, publishNoReplaceRemove
			t.Cleanup(func() { renameNoReplaceKernel, publishNoReplaceRemove = oldRename, oldRemove })
			probes := 0
			renameNoReplaceKernel = func(s, d string) error {
				if strings.HasPrefix(filepath.Base(s), ".nrprobe.") {
					probes++
					return syscall.EINVAL
				}
				return oldRename(s, d)
			}
			publishNoReplaceRemove = func(s string) error {
				if strings.HasPrefix(filepath.Base(s), ".nrprobe.") {
					return syscall.EPERM
				}
				return oldRemove(s)
			}
			for round := 0; round < 2; round++ {
				installed, err := op(afero.NewOsFs(), src, dst, proof)
				require.Error(t, err)
				require.Nil(t, installed)
				require.NoFileExists(t, dst)
				got, re := os.ReadFile(src)
				require.NoError(t, re)
				require.Equal(t, payload, got)
				t.Logf("final destination absent; completed=%t; error=%v", PublishCompleted(err), err)
				require.False(t, PublishCompleted(err), "scratch-only completion must not imply actual destination was published")
			}
			require.Equal(t, 2, probes, "probe-only partial completion must remain non-cacheable")
		})
	}
}
