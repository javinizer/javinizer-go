//go:build windows

package history

import (
	"context"
	"errors"
	"github.com/javinizer/javinizer-go/internal/models"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestPrimaryUndoWindowsPublicationRequestsNoReplaceWriteThrough(t *testing.T) {
	move := primaryUndoMoveFile
	t.Cleanup(func() { primaryUndoMoveFile = move })
	for _, fault := range []error{nil, windows.ERROR_ALREADY_EXISTS, windows.ERROR_ACCESS_DENIED} {
		primaryUndoMoveFile = func(src, dst *uint16, flags uint32) error {
			require.Equal(t, uint32(windows.MOVEFILE_WRITE_THROUGH), flags)
			require.NotEmpty(t, windows.UTF16PtrToString(src))
			require.NotEmpty(t, windows.UTF16PtrToString(dst))
			return fault
		}
		err := primaryUndoPublishNoReplace(afero.NewOsFs(), filepath.Join(t.TempDir(), "stage"), filepath.Join(t.TempDir(), "original"))
		if errors.Is(fault, os.ErrExist) {
			require.ErrorIs(t, err, fsutil.ErrPublishCollision)
		} else {
			require.ErrorIs(t, err, fault)
		}
	}
	require.Error(t, primaryUndoPublishNoReplace(afero.NewOsFs(), "\x00", "dest"))
	require.Error(t, primaryUndoPublishNoReplace(afero.NewOsFs(), "src", "\x00"))
}
func TestPrimaryUndoWindowsCrossDeviceClassification(t *testing.T) {
	require.True(t, primaryUndoCrossDevice(windows.ERROR_NOT_SAME_DEVICE))
	require.True(t, primaryUndoCrossDevice(syscall.EXDEV))
	require.False(t, primaryUndoCrossDevice(windows.ERROR_ACCESS_DENIED))
	require.False(t, primaryUndoCrossDevice(syscall.EINVAL))
}
func TestPrimaryUndoWindowsFailsClosedBeforeAdmission(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "original")
	require.NoError(t, os.WriteFile(source, []byte("synthetic"), 0400))
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, NewPath: source, OriginalPath: destination}
	require.ErrorContains(t, beginPrimaryUndo(context.Background(), afero.NewOsFs(), nil, op), "manual recovery")
	got, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "synthetic", string(got))
	require.NoFileExists(t, destination)
	require.Empty(t, op.GeneratedFiles)
	require.ErrorContains(t, resumePrimaryUndo(context.Background(), afero.NewOsFs(), nil, op, &models.PrimaryUndo{}), "manual recovery")
	got, err = os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "synthetic", string(got))
	require.NoFileExists(t, destination)
	require.Empty(t, op.GeneratedFiles)
}

func TestPrimaryUndoWindowsSameVolumeRenameBypassesAdmissionGate(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "library", "movie.mkv")
	dest := filepath.Join(root, "incoming", "movie.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(source), 0755))
	require.NoError(t, os.WriteFile(source, []byte("synthetic"), 0600))
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, NewPath: source, OriginalPath: dest}
	repo := newP3OpRepo()
	require.NoError(t, repo.Create(context.Background(), op))
	canonicalDir, err := fsutil.CanonicalizePath(filepath.Dir(dest))
	require.NoError(t, err)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Logf("native same-volume inverse: cwd=%q source=%q destination=%q canonical-parent=%q", cwd, source, dest, canonicalDir)
	result, err := revertPrimaryFileFS(context.Background(), afero.NewOsFs(), repo, op, func(string, string) {})
	require.NoError(t, err)
	if result != nil {
		t.Fatalf("native same-volume inverse failed: reason=%s error=%s", result.Reason, result.Error)
	}
	require.NoFileExists(t, source)
	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "synthetic", string(got))
	require.Empty(t, op.GeneratedFiles)
}

func TestPrimaryUndoWindowsIdentityUsesAll128BitsAndFailsClosed(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "identity")
	require.NoError(t, err)
	defer file.Close()
	get := primaryUndoGetFileInfo
	t.Cleanup(func() { primaryUndoGetFileInfo = get })
	high := byte(1)
	primaryUndoGetFileInfo = func(_ windows.Handle, class uint32, buffer *byte, size uint32) error {
		require.Equal(t, uint32(windows.FileIdInfo), class)
		info := (*primaryUndoFileIDInfo)(unsafe.Pointer(buffer))
		require.Equal(t, uint32(unsafe.Sizeof(*info)), size)
		info.VolumeSerialNumber = 7
		info.FileID[0] = 1
		info.FileID[15] = high
		return nil
	}
	first, _, err := primaryUndoIdentity(file)
	require.NoError(t, err)
	high = 2
	second, _, err := primaryUndoIdentity(file)
	require.NoError(t, err)
	require.False(t, primaryUndoSameObject(first, second))
	primaryUndoGetFileInfo = func(windows.Handle, uint32, *byte, uint32) error { return windows.ERROR_NOT_SUPPORTED }
	_, _, err = primaryUndoIdentity(file)
	require.ErrorContains(t, err, "strong native")
}
