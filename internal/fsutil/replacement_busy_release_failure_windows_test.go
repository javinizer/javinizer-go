//go:build windows

package fsutil

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

type busyReleaseFailureFS struct {
	afero.Fs
	marker string
	fault  *releaseStatusFile
}

func (fs *busyReleaseFailureFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	handle, err := fs.Fs.OpenFile(path, flag, mode)
	if err != nil || path != fs.marker || flag != os.O_RDWR || fs.fault == nil {
		return handle, err
	}
	fs.fault.File = handle
	return fs.fault, nil
}

// The reader causes a real Windows sharing refusal. Only the subsequent
// creation-bound release write uses an ordinary filesystem fault adapter.
func TestReplacementBusyReleaseReportsStatusFailureAndRetainsRetry(t *testing.T) {
	for _, visible := range []bool{false, true} {
		name := "write-refused"
		if visible {
			name = "visible-sync-failure"
		}
		t.Run(name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "poster.jpg")
			fs := &busyReleaseFailureFS{Fs: afero.NewOsFs(), marker: ReplacementBusyPath(dest)}
			release, token, err := AcquireReplacementBusyEx(fs, dest)
			require.NoError(t, err)
			t.Cleanup(release)
			reader, err := os.Open(fs.marker)
			require.NoError(t, err)
			t.Cleanup(func() { _ = reader.Close() })
			require.ErrorIs(t, os.Rename(fs.marker, fs.marker+".moved"), windows.ERROR_SHARING_VIOLATION)

			fault := errors.New("release status persistence failed")
			status := &releaseStatusFile{writeErr: fault}
			if visible {
				status = &releaseStatusFile{writeN: 1, syncErr: fault}
			}
			fs.fault = status
			var logs bytes.Buffer
			restore := logging.SetOutput(&logs)
			t.Cleanup(restore)
			release()
			require.Equal(t, 1, status.writes)
			require.Equal(t, 1, status.closes)
			require.Contains(t, logs.String(), "release status (visible="+map[bool]string{false: "false", true: "true"}[visible]+")")
			require.Contains(t, logs.String(), fault.Error())
			data, err := os.ReadFile(fs.marker)
			require.NoError(t, err)
			if visible {
				require.Equal(t, token[:len(token)-1]+"1", string(data))
				require.Equal(t, 1, status.syncs)
			} else {
				require.Equal(t, token, string(data), "a refused status write preserves the live owner")
				require.Zero(t, status.syncs)
				_, _, err = AcquireReplacementBusyEx(fs.Fs, dest)
				require.ErrorIs(t, err, ErrReplacementBusy)
				fs.fault = nil
				release()
				data, err = os.ReadFile(fs.marker)
				require.NoError(t, err)
				require.Equal(t, token[:len(token)-1]+"1", string(data), "failed release remains retryable")
			}

			fs.fault = nil
			require.NoError(t, reader.Close())
			successorRelease, successorToken, err := AcquireReplacementBusyEx(fs.Fs, dest)
			require.NoError(t, err, "visible status permits a new owner despite the reported persistence error")
			t.Cleanup(successorRelease)
			release()
			data, err = os.ReadFile(fs.marker)
			require.NoError(t, err)
			require.Equal(t, successorToken, string(data), "the prior capability preserves its successor")
			successorRelease()
			require.NoFileExists(t, fs.marker)
		})
	}
}
