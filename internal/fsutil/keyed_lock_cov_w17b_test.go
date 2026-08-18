package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProbeCaseSensitiveW17B_SensitiveRootPreservesUppercaseUserFile(t *testing.T) {
	root := t.TempDir()
	ops := &w17BProbeFS{
		caseSensitive: true,
		seedAlternate: true,
		files:         make(map[string]w17BProbeEntry),
	}

	got, err := probeCaseSensitive(ops.ops(), root)
	require.NoError(t, err)
	require.False(t, got, "the pre-existing alternate spelling is visible to stat")
	require.Len(t, ops.opened, 1)
	require.Len(t, ops.removed, 1, "cleanup must remove only the created spelling")
	require.Equal(t, ops.opened[0], ops.removed[0])
	require.NotEqual(t, ops.userPath, ops.removed[0])
	require.Equal(t, "user-owned probe", ops.files[ops.key(ops.userPath)].content)
}

func TestProbeCaseSensitiveW17B_InsensitiveRootCleansCreatedFile(t *testing.T) {
	root := t.TempDir()
	ops := &w17BProbeFS{
		caseSensitive: false,
		files:         make(map[string]w17BProbeEntry),
	}

	got, err := probeCaseSensitive(ops.ops(), root)
	require.NoError(t, err)
	require.False(t, got, "the alternate spelling resolves to the created file")
	require.Len(t, ops.opened, 1)
	require.Equal(t, []string{ops.opened[0]}, ops.removed)
	require.Empty(t, ops.files, "the normal insensitive probe leaves no temporary file")
}

type w17BProbeFS struct {
	caseSensitive bool
	seedAlternate bool
	files         map[string]w17BProbeEntry
	opened        []string
	removed       []string
	userPath      string
}

type w17BProbeEntry struct {
	path    string
	content string
}

func (f *w17BProbeFS) ops() caseProbeOps {
	return caseProbeOps{
		openFile: f.openFile,
		stat:     f.stat,
		readDir:  f.readDir,
		remove:   f.remove,
	}
}

func (f *w17BProbeFS) openFile(name string, _ int, _ os.FileMode) (caseProbeFile, error) {
	if f.seedAlternate && len(f.opened) == 0 {
		f.userPath = filepath.Join(filepath.Dir(name), strings.ToUpper(filepath.Base(name)))
		f.files[f.key(f.userPath)] = w17BProbeEntry{path: f.userPath, content: "user-owned probe"}
	}
	f.opened = append(f.opened, name)
	key := f.key(name)
	if _, exists := f.files[key]; exists {
		return nil, os.ErrExist
	}
	f.files[key] = w17BProbeEntry{path: name}
	return w17BProbeFile{}, nil
}

func (f *w17BProbeFS) stat(name string) (os.FileInfo, error) {
	entry, ok := f.files[f.key(name)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return w17BProbeInfo{path: entry.path}, nil
}

func (f *w17BProbeFS) readDir(string) ([]os.DirEntry, error) { return nil, nil }

func (f *w17BProbeFS) remove(name string) error {
	f.removed = append(f.removed, name)
	key := f.key(name)
	if _, ok := f.files[key]; !ok {
		return os.ErrNotExist
	}
	delete(f.files, key)
	return nil
}

func (f *w17BProbeFS) key(name string) string {
	if f.caseSensitive {
		return name
	}
	return strings.ToLower(name)
}

type w17BProbeFile struct{}

func (w17BProbeFile) Close() error { return nil }

type w17BProbeInfo struct{ path string }

func (i w17BProbeInfo) Name() string     { return filepath.Base(i.path) }
func (w17BProbeInfo) Size() int64        { return 0 }
func (w17BProbeInfo) Mode() os.FileMode  { return 0o600 }
func (w17BProbeInfo) ModTime() time.Time { return time.Time{} }
func (w17BProbeInfo) IsDir() bool        { return false }
func (w17BProbeInfo) Sys() any           { return nil }
