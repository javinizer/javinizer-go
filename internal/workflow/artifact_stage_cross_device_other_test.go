//go:build !windows

package workflow

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func nativeArtifactCrossDeviceError(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

func TestNativeArtifactCrossDeviceError(t *testing.T) {
	require.True(t, nativeArtifactCrossDeviceError(syscall.EXDEV))
	require.True(t, nativeArtifactCrossDeviceError(&os.LinkError{Op: "rename", Err: syscall.EXDEV}))
	require.False(t, nativeArtifactCrossDeviceError(nil))
	require.False(t, nativeArtifactCrossDeviceError(os.ErrPermission))
	require.False(t, nativeArtifactCrossDeviceError(syscall.EEXIST))
}
