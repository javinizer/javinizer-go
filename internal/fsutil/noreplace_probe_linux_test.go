//go:build linux

package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestNoClobberProbeLinuxFUSERefusal(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(map[bool]string{false: "copy", true: "cross-device move"}[move], func(t *testing.T) {
			dir := t.TempDir()
			src, dst := filepath.Join(dir, "movie.mp4"), filepath.Join(dir, "out", "movie.mp4")
			require.NoError(t, os.WriteFile(src, []byte("original payload"), 0o600))
			prevK, prevL := renameNoReplaceKernel, publishNoReplaceLink
			t.Cleanup(func() { renameNoReplaceKernel = prevK; publishNoReplaceLink = prevL })
			probes, links := 0, 0
			renameNoReplaceKernel = func(s, d string) error {
				if s == src {
					require.True(t, move)
					return syscall.EXDEV
				}
				require.True(t, strings.HasPrefix(filepath.Base(s), ".nrprobe."), "no payload staging or cleanup rename")
				require.Equal(t, filepath.Dir(dst), filepath.Dir(s))
				probes++
				return syscall.EINVAL
			}
			publishNoReplaceLink = func(s, d string) error {
				links++
				require.True(t, strings.HasPrefix(filepath.Base(s), ".nrprobe."))
				return syscall.EPERM
			}
			op := CopyFileNoReplace
			if move {
				op = MoveFileNoReplace
			}
			for i := 0; i < 2; i++ {
				err := op(afero.NewOsFs(), src, dst)
				require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
				require.NotErrorIs(t, err, syscall.EINVAL)
				require.False(t, isCrossDeviceError(err))
				for _, text := range []string{"renameat2(RENAME_NOREPLACE)", syscall.EINVAL.Error(), "link(2)", syscall.EPERM.Error(), "native-filesystem path", "/mnt/diskN/", "protected_hardlinks"} {
					require.ErrorContains(t, err, text)
				}
			}
			require.Equal(t, 1, probes)
			require.Equal(t, 1, links)
			got, err := os.ReadFile(src)
			require.NoError(t, err)
			require.Equal(t, "original payload", string(got))
			entries, err := os.ReadDir(filepath.Dir(dst))
			require.NoError(t, err)
			require.Empty(t, entries, "no .nrstg and successful bound cleanup leaves no probe")
		})
	}
}

func TestNoClobberProbeLinuxSameVolumeMoveUntouched(t *testing.T) {
	for _, incapable := range []bool{false, true} {
		t.Run(map[bool]string{false: "capable", true: "incapable"}[incapable], func(t *testing.T) {
			dir := t.TempDir()
			src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "dest")
			require.NoError(t, os.WriteFile(src, []byte("payload"), 0o600))
			stubNoClobberProbe(t, func(afero.Fs, string, string) error { t.Fatal("same-volume move must never probe"); return nil })
			if incapable {
				stubRenameNoReplaceKernelW16(t, syscall.EINVAL)
				stubPublishNoReplaceLinkW17L(t, syscall.EPERM)
			}
			err := MoveFileNoReplace(afero.NewOsFs(), src, dst)
			if incapable {
				require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
				require.NotErrorIs(t, err, syscall.EINVAL)
				require.FileExists(t, src)
				require.NoFileExists(t, dst)
			} else {
				require.NoError(t, err)
				require.NoFileExists(t, src)
				require.FileExists(t, dst)
			}
		})
	}
}
