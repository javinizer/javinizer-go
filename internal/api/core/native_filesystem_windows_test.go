//go:build windows

package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestNativeWindowsFileIdentityContracts(t *testing.T) {
	root := t.TempDir()
	opened, canonical, err := ValidateAndOpenPath(root, &SecurityNarrowConfig{AllowedDirectories: []string{root}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.Close() })
	openedInfo, err := opened.Stat()
	require.NoError(t, err)
	pathInfo, err := os.Stat(canonical)
	require.NoError(t, err)
	require.True(t, openedInfo.IsDir())
	require.True(t, os.SameFile(openedInfo, pathInfo))
	identity, err := getFileIdentity(pathInfo)
	require.ErrorIs(t, err, apperrors.ErrInodeExtraction)
	require.Equal(t, fileIdentity{}, identity, "FileInfo alone cannot establish Windows pre-open identity")

	first, err := os.CreateTemp(root, "identity-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := os.Open(first.Name())
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	other, err := os.CreateTemp(root, "other-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	firstID, err := getFileIdentityFromFd(first)
	require.NoError(t, err)
	secondID, err := getFileIdentityFromFd(second)
	require.NoError(t, err)
	otherID, err := getFileIdentityFromFd(other)
	require.NoError(t, err)
	require.Equal(t, firstID, secondID, "two handles to the same file must agree")
	require.NotEqual(t, firstID, otherID, "different live files must remain distinguishable")
	var native syscall.ByHandleFileInformation
	require.NoError(t, syscall.GetFileInformationByHandle(syscall.Handle(first.Fd()), &native))
	require.Equal(t, fileIdentity{native.VolumeSerialNumber, native.FileIndexHigh, native.FileIndexLow}, firstID)
	require.NoError(t, first.Close())
	closedID, err := getFileIdentityFromFd(first)
	require.ErrorIs(t, err, windows.ERROR_INVALID_HANDLE)
	require.Equal(t, fileIdentity{}, closedID)
}

func TestNativeWindowsReparseFallbackContracts(t *testing.T) {
	ownedRoot := t.TempDir()
	// Windows TempDir may contain an 8.3 ancestor (for example RUNNER~1).
	// Derive every fixture path and seam comparison from the real canonical
	// spelling used by public validation, while proving it is our owned root.
	root, err := filepath.EvalSymlinks(ownedRoot)
	require.NoError(t, err)
	ownedInfo, err := os.Stat(ownedRoot)
	require.NoError(t, err)
	canonicalInfo, err := os.Stat(root)
	require.NoError(t, err)
	require.True(t, os.SameFile(ownedInfo, canonicalInfo))
	existing := filepath.Join(root, "existing")
	require.NoError(t, os.Mkdir(existing, 0700))
	missing := filepath.Join(root, "missing")
	fs := afero.NewOsFs()
	for _, helper := range []struct {
		name    string
		resolve func(string, afero.Fs) (string, error)
	}{{"path", resolveReparseFallback}, {"parent", resolveReparseParentFallback}} {
		t.Run(helper.name, func(t *testing.T) {
			got, err := helper.resolve(existing+string(filepath.Separator)+".", fs)
			require.NoError(t, err)
			require.Equal(t, filepath.Clean(existing), got)
			got, err = helper.resolve(missing, fs)
			require.ErrorIs(t, err, apperrors.ErrPathUnresolvable)
			require.Empty(t, got)
			_, err = os.Stat(missing)
			require.ErrorIs(t, err, os.ErrNotExist, "fallback must not create the missing path")
		})
	}

	// The documented resolver seam models an external reparse failure only;
	// every existence decision still uses the real owned filesystem. This does
	// not claim to create or exercise an actual NTFS volume mount point.
	seamCalls := 0
	withEvalSymlinksFunc(t, func(path string) (string, error) {
		if filepath.Clean(path) == existing {
			seamCalls++
			return "", errors.New("unresolvable reparse point")
		}
		return filepath.EvalSymlinks(path)
	})
	validator := NewPathValidator(fs, []string{root}, nil)
	got, err := validator.ValidateDir(existing)
	require.NoError(t, err)
	require.Equal(t, existing, got)
	require.Positive(t, seamCalls, "public validation must exercise the reparse fallback")
	beforeDeny := seamCalls
	denied := NewPathValidator(fs, []string{root}, []string{existing})
	_, err = denied.ValidateDir(existing)
	require.ErrorIs(t, err, apperrors.ErrPathInDenylist, "reparse fallback must not bypass the configured denylist")
	require.Greater(t, seamCalls, beforeDeny)
	beforeParent := seamCalls
	leaf := filepath.Join(existing, "new", "child")
	got, err = validator.canonicalizePath(leaf)
	require.NoError(t, err)
	require.Equal(t, leaf, got, "parent fallback must preserve missing descendants")
	require.Greater(t, seamCalls, beforeParent, "parent walk must exercise the reparse fallback")
	beforeMissing := seamCalls
	_, err = validator.ValidateDir(leaf)
	require.ErrorIs(t, err, apperrors.ErrPathNotExist, "canonicalizing descendants must not make a missing directory valid")
	require.Greater(t, seamCalls, beforeMissing)
	_, err = os.Stat(leaf)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestNativeWindowsShortPathResolutionContracts(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		require.Empty(t, resolveShortPathName(""))
	})
	t.Run("embedded NUL", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid\x00name")
		require.Equal(t, path, resolveShortPathName(path))
	})
	t.Run("missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "absent file.txt")
		require.Equal(t, path, resolveShortPathName(path))
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
	t.Run("existing", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "owned long filename.txt")
		require.NoError(t, os.WriteFile(path, []byte("unchanged"), 0600))
		got := resolveShortPathName(path)
		before, err := os.Stat(path)
		require.NoError(t, err)
		after, err := os.Stat(got)
		require.NoError(t, err)
		require.True(t, os.SameFile(before, after), "resolved spelling must reference the original file")
		data, err := os.ReadFile(got)
		require.NoError(t, err)
		require.Equal(t, "unchanged", string(data))
		entries, err := os.ReadDir(root)
		require.NoError(t, err)
		require.Len(t, entries, 1, "normalization must not create filesystem entries")
	})
	t.Run("extended length owned path", func(t *testing.T) {
		root := t.TempDir()
		canonicalRoot, err := filepath.EvalSymlinks(root)
		require.NoError(t, err)
		rootInfo, err := os.Stat(root)
		require.NoError(t, err)
		canonicalInfo, err := os.Stat(canonicalRoot)
		require.NoError(t, err)
		require.True(t, os.SameFile(rootInfo, canonicalInfo))
		require.True(t, filepath.IsAbs(canonicalRoot))
		volume := filepath.VolumeName(canonicalRoot)
		require.Len(t, volume, 2, "fixture requires an owned local-drive TempDir")
		require.Equal(t, byte(':'), volume[1])

		subtree := `\\?\` + filepath.Join(canonicalRoot, "extended-path-contract")
		t.Cleanup(func() {
			require.NoError(t, os.RemoveAll(subtree))
			_, err := os.Stat(subtree)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
		component := strings.Repeat("owned-segment-", 6)
		require.Less(t, len(component), 100)
		directory := filepath.Join(subtree, component, component, component)
		path := filepath.Join(directory, "owned-marker.txt")
		encoded, err := windows.UTF16FromString(path)
		require.NoError(t, err)
		require.Greater(t, len(encoded)-1, windows.MAX_PATH)
		require.Less(t, len(encoded), 32767)
		require.NoError(t, os.MkdirAll(directory, 0700))
		require.NoError(t, os.WriteFile(path, []byte("long path contents unchanged"), 0600))
		before, err := os.Stat(path)
		require.NoError(t, err)

		// Prove the real API needs a larger buffer, without relying on 8.3 aliases.
		initial := make([]uint16, windows.MAX_PATH)
		needed, err := windows.GetLongPathName(&encoded[0], &initial[0], uint32(len(initial)))
		require.NoError(t, err)
		require.Greater(t, needed, uint32(len(initial)))
		referenceBuffer := make([]uint16, needed+1)
		n, err := windows.GetLongPathName(&encoded[0], &referenceBuffer[0], uint32(len(referenceBuffer)))
		require.NoError(t, err)
		require.Greater(t, n, uint32(windows.MAX_PATH))
		require.Less(t, n, uint32(len(referenceBuffer)))
		reference := windows.UTF16ToString(referenceBuffer[:n])
		require.True(t, strings.HasSuffix(reference, `\`+filepath.Join(component, component, component, "owned-marker.txt")))

		got := resolveShortPathName(path)
		require.Equal(t, reference, got, "resizing must return the complete native path")
		after, err := os.Stat(got)
		require.NoError(t, err)
		require.True(t, os.SameFile(before, after))
		contents, err := os.ReadFile(got)
		require.NoError(t, err)
		require.Equal(t, "long path contents unchanged", string(contents))
		for _, dir := range []string{subtree, filepath.Join(subtree, component), filepath.Join(subtree, component, component), directory} {
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "normalization must not create filesystem entries")
		}
	})
}
