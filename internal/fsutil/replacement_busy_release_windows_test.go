//go:build windows

package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// A normal Windows read handle shares read/write but denies deletion. This is
// the same real handle contract used by a competing marker inspection. No
// scheduling, sleep, fake driver error or private release callback is needed.
func TestReplacementBusyExplicitReleaseSurvivesNativeReader(t *testing.T) {
	fs := afero.NewOsFs()
	dest := filepath.Join(t.TempDir(), "poster.jpg")
	release, token, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	t.Cleanup(release)
	marker := fsutil.ReplacementBusyPath(dest)
	reader, err := os.Open(marker)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	// A live owner remains protected, even when another process can read it.
	_, _, err = fsutil.AcquireReplacementBusyEx(fs, dest)
	require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
	// Prove the actual kernel refusal, not merely an assumed sharing mode.
	err = os.Rename(marker, marker+".unexpected-move")
	require.ErrorIs(t, err, windows.ERROR_SHARING_VIOLATION)
	release()
	require.NoError(t, reader.Close())
	// Once the real reader closes, the explicit release must be recoverable
	// without waiting for this process's PID to die or aging any timestamp.
	successorRelease, successorToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	t.Cleanup(successorRelease)
	require.NotEqual(t, token, successorToken)
	release() // the old capability must never remove the successor's marker
	current, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, successorToken, string(current))
	successorRelease()
	require.NoFileExists(t, marker)
}
