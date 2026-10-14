//go:build windows

package workflow

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// Windows native rename returns a Win32 error rather than the POSIX EXDEV
// sentinel. Unlike the production copy fallback, this probe must positively
// identify another volume rather than treating generic EINVAL as evidence.
func nativeArtifactCrossDeviceError(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}

func TestNativeArtifactCrossDeviceError(t *testing.T) {
	require.True(t, nativeArtifactCrossDeviceError(syscall.EXDEV))
	require.True(t, nativeArtifactCrossDeviceError(windows.ERROR_NOT_SAME_DEVICE))
	require.True(t, nativeArtifactCrossDeviceError(&os.LinkError{Op: "rename", Err: windows.ERROR_NOT_SAME_DEVICE}))
	require.False(t, nativeArtifactCrossDeviceError(nil))
	require.False(t, nativeArtifactCrossDeviceError(os.ErrPermission))
	require.False(t, nativeArtifactCrossDeviceError(syscall.EINVAL))
}
