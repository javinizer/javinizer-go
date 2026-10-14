package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type busyRetryFaultFS struct {
	afero.Fs
	marker        string
	kind          string
	failures      int
	beforeScratch func() error
}

func (f *busyRetryFaultFS) fail(path, kind string) bool {
	if f.kind == kind && filepath.Clean(path) == f.marker && f.failures > 0 {
		f.failures--
		return true
	}
	return false
}
func (f *busyRetryFaultFS) Open(path string) (afero.File, error) {
	if f.fail(path, "read") {
		return nil, os.ErrPermission
	}
	return f.Fs.Open(path)
}
func (f *busyRetryFaultFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	if f.beforeScratch != nil && strings.HasPrefix(filepath.Clean(path), f.marker+".takeover-") {
		hook := f.beforeScratch
		f.beforeScratch = nil
		if err := hook(); err != nil {
			return nil, err
		}
	}
	if f.kind == "reserve" && strings.HasPrefix(filepath.Clean(path), f.marker+".takeover-") && f.failures > 0 {
		f.failures--
		return nil, os.ErrPermission
	}
	return f.Fs.OpenFile(path, flag, mode)
}
func (f *busyRetryFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func TestReplacementBusyReleaseAllowsSameObjectTimestampFinalization(t *testing.T) {
	base := afero.NewOsFs()
	dst := filepath.Join(t.TempDir(), "fanart.jpg")
	release, err := AcquireReplacementBusy(base, dst)
	require.NoError(t, err)
	marker := ReplacementBusyPath(dst)
	original, err := base.Stat(marker)
	require.NoError(t, err)
	finalized := original.ModTime().Add(time.Second)
	require.NoError(t, base.Chtimes(marker, finalized, finalized))
	release()
	_, err = base.Stat(marker)
	require.True(t, os.IsNotExist(err))
}

func TestReplacementBusyReleaseRetriesTransientFailures(t *testing.T) {
	for _, kind := range []string{"read", "reserve"} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewOsFs()
			dst := filepath.Join(t.TempDir(), "fanart.jpg")
			fs := &busyRetryFaultFS{Fs: base, marker: ReplacementBusyPath(dst), kind: kind}
			release, err := AcquireReplacementBusy(fs, dst)
			require.NoError(t, err)
			fs.failures = 1
			release()
			_, err = base.Stat(fs.marker)
			require.NoError(t, err)
			var group sync.WaitGroup
			for i := 0; i < 4; i++ {
				group.Add(1)
				go func() { defer group.Done(); release() }()
			}
			group.Wait()
			_, err = base.Stat(fs.marker)
			require.True(t, os.IsNotExist(err))
			release()
		})
	}
}

func TestReplacementBusyRetryPreservesSuccessorAfterObservation(t *testing.T) {
	base := afero.NewOsFs()
	dst := filepath.Join(t.TempDir(), "fanart.jpg")
	fs := &busyRetryFaultFS{Fs: base, marker: ReplacementBusyPath(dst), kind: "read"}
	release, token, err := AcquireReplacementBusyEx(fs, dst)
	require.NoError(t, err)
	original, err := base.Stat(fs.marker)
	require.NoError(t, err)
	fs.failures = 1
	release()
	fs.beforeScratch = func() error {
		if err := base.Rename(fs.marker, fs.marker+".held"); err != nil {
			return err
		}
		if err := afero.WriteFile(base, fs.marker, []byte(token), 0o600); err != nil {
			return err
		}
		return base.Chtimes(fs.marker, original.ModTime(), original.ModTime())
	}
	release()
	content, err := afero.ReadFile(base, fs.marker)
	require.NoError(t, err)
	require.Equal(t, token, string(content))
}

func TestReplacementBusyReleaseRequiresCreationIdentity(t *testing.T) {
	base := afero.NewMemMapFs()
	marker, token := filepath.FromSlash("/fanart.jpg.dlbusy"), "unbound-token"
	require.NoError(t, afero.WriteFile(base, marker, []byte(token), 0o600))
	require.False(t, releaseReplacementBusy(base, marker, token, nil))
	content, err := afero.ReadFile(base, marker)
	require.NoError(t, err)
	require.Equal(t, token, string(content))
}

func TestReplacementBusyRetryPreservesCopiedTokenSuccessor(t *testing.T) {
	base := afero.NewOsFs()
	dst := filepath.Join(t.TempDir(), "fanart.jpg")
	fs := &busyRetryFaultFS{Fs: base, marker: ReplacementBusyPath(dst), kind: "read"}
	release, token, err := AcquireReplacementBusyEx(fs, dst)
	require.NoError(t, err)
	original, err := base.Stat(fs.marker)
	require.NoError(t, err)
	fs.failures = 1
	release()
	require.NoError(t, base.Rename(fs.marker, fs.marker+".held"))
	require.NoError(t, afero.WriteFile(base, fs.marker, []byte(token), 0o600))
	require.NoError(t, base.Chtimes(fs.marker, original.ModTime(), original.ModTime()))
	release()
	content, err := afero.ReadFile(base, fs.marker)
	require.NoError(t, err)
	require.Equal(t, token, string(content))
}

func TestReplacementBusyRetryPreservesChangedToken(t *testing.T) {
	base := afero.NewOsFs()
	dst := filepath.Join(t.TempDir(), "fanart.jpg")
	fs := &busyRetryFaultFS{Fs: base, marker: ReplacementBusyPath(dst), kind: "read"}
	release, token, err := AcquireReplacementBusyEx(fs, dst)
	require.NoError(t, err)
	original, err := base.Stat(fs.marker)
	require.NoError(t, err)
	fs.failures = 1
	release()
	tail := "0"
	if strings.HasSuffix(token, tail) {
		tail = "1"
	}
	changed := token[:len(token)-1] + tail
	require.NoError(t, afero.WriteFile(base, fs.marker, []byte(changed), 0o600))
	require.NoError(t, base.Chtimes(fs.marker, original.ModTime(), original.ModTime()))
	release()
	release()
	content, err := afero.ReadFile(base, fs.marker)
	require.NoError(t, err)
	require.Equal(t, changed, string(content))
}

func TestReplacementBusyRetryPreservesNewOwner(t *testing.T) {
	base := afero.NewOsFs()
	dst := filepath.Join(t.TempDir(), "fanart.jpg")
	fs := &busyRetryFaultFS{Fs: base, marker: ReplacementBusyPath(dst), kind: "read"}
	release, ownToken, err := AcquireReplacementBusyEx(fs, dst)
	require.NoError(t, err)
	fs.failures = 1
	release()
	require.NoError(t, base.Rename(fs.marker, fs.marker+".held"))
	foreignRelease, foreignToken, err := AcquireReplacementBusyEx(base, dst)
	require.NoError(t, err)
	defer foreignRelease()
	require.NotEqual(t, ownToken, foreignToken)
	release()
	release()
	content, err := afero.ReadFile(base, fs.marker)
	require.NoError(t, err)
	require.Equal(t, foreignToken, string(content))
}
