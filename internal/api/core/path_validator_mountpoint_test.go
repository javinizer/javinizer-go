package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withEvalSymlinksFunc temporarily overrides the evalSymlinksFunc seam and
// restores it via t.Cleanup.
func withEvalSymlinksFunc(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := evalSymlinksFunc
	evalSymlinksFunc = fn
	t.Cleanup(func() { evalSymlinksFunc = orig })
}

// skipUnlessWindows skips the test on non-Windows platforms. The
// Stat-fallback that canonicalizePath uses to tolerate NTFS volume mount
// points is gated on runtime.GOOS == "windows", so the fallback only fires
// on Windows. On darwin/linux CI an unresolvable path must surface
// ErrPathUnresolvable regardless of whether Stat succeeds.
func skipUnlessWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skipf("Stat-fallback is Windows-only (NTFS mount points); skipping on %s", runtime.GOOS)
	}
}

// TestCanonicalizePath_NTFSMountPoint_Simulated simulates the Windows NTFS
// volume mount point scenario (reparse tag IO_REPARSE_TAG_MOUNT_POINT): the
// path genuinely exists, but filepath.EvalSymlinks returns a non-NotExist
// error. canonicalizePath must fall back to the cleaned absolute path because
// Stat confirms the path is present and accessible. The fallback is gated on
// runtime.GOOS == "windows", so this test only runs on Windows.
func TestCanonicalizePath_NTFSMountPoint_Simulated(t *testing.T) {
	skipUnlessWindows(t)
	fs := afero.NewMemMapFs()

	mountDir := "/mnt/ExtDrive"
	require.NoError(t, fs.MkdirAll(mountDir, 0o755))

	withEvalSymlinksFunc(t, func(string) (string, error) {
		return "", errors.New("reparse point not resolvable")
	})

	v := NewPathValidator(fs, nil, nil)

	got, err := v.canonicalizePath(mountDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(mountDir), got)
}

// TestCanonicalizePath_NTFSMountPoint_NonExistentStillErrors ensures the
// Stat-fallback does NOT rescue a genuinely non-existent path when
// EvalSymlinks returns a non-NotExist error: Stat fails, so canonicalizePath
// still surfaces ErrPathUnresolvable. This preserves the security posture
// (no blanket "ignore EvalSymlinks errors"). Runs on all platforms: the
// non-existent path is rejected everywhere regardless of the Windows gate.
func TestCanonicalizePath_NTFSMountPoint_NonExistentStillErrors(t *testing.T) {
	fs := afero.NewMemMapFs()

	withEvalSymlinksFunc(t, func(string) (string, error) {
		return "", errors.New("reparse point not resolvable")
	})

	v := NewPathValidator(fs, nil, nil)

	_, err := v.canonicalizePath("/mnt/does/not/exist")
	require.Error(t, err)
	assert.ErrorIs(t, err, apperrors.ErrPathUnresolvable)
}

// TestCanonicalizePath_NTFSMountPoint_ParentWalk simulates a mount point
// appearing in a parent segment of an otherwise non-existent leaf path: the
// nearest existing ancestor resolves via the Stat-fallback (Stat succeeds)
// and the missing leaf segments are appended to the cleaned parent. The seam
// returns os.ErrNotExist for the non-existent leaf (so canonicalizePath enters
// the parent-walk) and a non-NotExist reparse error for the existing parent
// (simulating the mount point on the ancestor). The fallback is gated on
// runtime.GOOS == "windows", so this test only runs on Windows.
func TestCanonicalizePath_NTFSMountPoint_ParentWalk(t *testing.T) {
	skipUnlessWindows(t)
	fs := afero.NewMemMapFs()

	existingParent := "/mnt/ExtDrive"
	require.NoError(t, fs.MkdirAll(existingParent, 0o755))

	withEvalSymlinksFunc(t, func(p string) (string, error) {
		if p == existingParent {
			return "", errors.New("reparse point not resolvable")
		}
		return "", os.ErrNotExist
	})

	v := NewPathValidator(fs, nil, nil)

	leaf := filepath.Join(existingParent, "new", "child")
	got, err := v.canonicalizePath(leaf)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(leaf), got)
}

// TestCanonicalizePath_NonWindows_StatSucceedingStillErrors confirms that on
// non-Windows platforms an unresolvable path does NOT fall back to
// filepath.Clean even when Stat confirms the path exists. This is the
// CodeRabbit-requested security guard: on non-Windows the original
// ErrPathUnresolvable behavior is preserved so canonicalization cannot be
// bypassed. The Windows-only fallback test (TestCanonicalizePath_NTFSMountPoint_Simulated)
// covers the opposite branch under runtime.GOOS == "windows".
func TestCanonicalizePath_NonWindows_StatSucceedingStillErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skipf("non-Windows guard test; skipping on %s", runtime.GOOS)
	}
	fs := afero.NewMemMapFs()

	path := "/mnt/ExtDrive"
	require.NoError(t, fs.MkdirAll(path, 0o755))

	withEvalSymlinksFunc(t, func(string) (string, error) {
		return "", errors.New("reparse point not resolvable")
	})

	v := NewPathValidator(fs, nil, nil)

	_, err := v.canonicalizePath(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, apperrors.ErrPathUnresolvable)
}
