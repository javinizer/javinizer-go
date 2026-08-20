package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The user-owned alternate spelling is seeded from the generated probe name;
// the test does not rely on a fixed probe basename.
func TestProbeCaseSensitiveW17B_SensitiveRootPreservesUppercaseUserFile(t *testing.T) {
	root := t.TempDir()
	ops := &w17BProbeFS{
		caseSensitive: true,
		seedAlternate: true,
		files:         make(map[string]w17BProbeEntry),
	}
	// Codex P2 (w31): identity binding on the probe — the seeded user file
	// is a DISTINCT object from the created probe, so the root stays
	// case-sensitive and the user's bytes are preserved.
	prev := probeSameFile
	probeSameFile = func(a, b os.FileInfo) bool { return a == b }
	t.Cleanup(func() { probeSameFile = prev })

	got, err := probeCaseSensitive(ops.ops(), root)
	require.NoError(t, err)
	require.True(t, got, "a distinct user-owned alternate spelling must not downgrade the root to insensitive")
	require.Len(t, ops.opened, 1)
	require.Len(t, ops.removed, 1, "cleanup unlinks once, through the take-aside scratch name")
	require.Equal(t, ops.opened[0]+probeCleanupScratchSuffix, ops.removed[0],
		"the ONLY unlink targets the verified object's scratch name (wave-39 bound cleanup)")
	require.NotEqual(t, ops.userPath, ops.removed[0])
	require.Equal(t, "user-owned probe", ops.files[ops.key(ops.userPath)].content)
}

// The generated name may vary per attempt, but cleanup must still target only
// the exact spelling successfully created by O_EXCL.
func TestProbeCaseSensitiveW17B_InsensitiveRootCleansCreatedFile(t *testing.T) {
	root := t.TempDir()
	ops := &w17BProbeFS{
		caseSensitive: false,
		files:         make(map[string]w17BProbeEntry),
	}
	// w31 binding: the insensitive fake resolves both spellings to one
	// keyed entry — the same struct value — so value equality models the
	// shared inode.
	prev := probeSameFile
	probeSameFile = func(a, b os.FileInfo) bool { return a == b }
	t.Cleanup(func() { probeSameFile = prev })

	got, err := probeCaseSensitive(ops.ops(), root)
	require.NoError(t, err)
	require.False(t, got, "the alternate spelling resolves to the created file")
	require.Len(t, ops.opened, 1)
	require.Equal(t, []string{ops.opened[0] + probeCleanupScratchSuffix}, ops.removed)
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
		rename:   f.rename,
		remove:   f.remove,
	}
}

func (f *w17BProbeFS) openFile(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
	if flag&os.O_CREATE == 0 {
		// Wave-34 bind leg: the verified scratch is re-opened O_RDONLY to
		// re-prove THE created identity by descriptor — serve it from the fake
		// inode table (identity = the entry's pre-move path payload, the same
		// value stat answers). A vanished scratch answers ENOENT, which the
		// bind leg reads as a completed cleanup. Not a create attempt: it is
		// neither seeded as the user alternate nor recorded in opened.
		entry, ok := f.files[f.key(name)]
		if !ok {
			return nil, os.ErrNotExist
		}
		return w17BProbeFile{info: w17BProbeInfo{path: entry.path}}, nil
	}
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
	return w17BProbeFile{info: w17BProbeInfo{path: name}}, nil
}

func (f *w17BProbeFS) stat(name string) (os.FileInfo, error) {
	entry, ok := f.files[f.key(name)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return w17BProbeInfo{path: entry.path}, nil
}

// rename models the cleanup take-aside's no-replace move (wave-39): the
// entry's IDENTITY (its path payload) survives the move — the fake models
// an inode as an immutable struct value — while an occupied target refuses
// instead of being displaced.
func (f *w17BProbeFS) rename(oldPath, newPath string) error {
	oldKey, newKey := f.key(oldPath), f.key(newPath)
	entry, ok := f.files[oldKey]
	if !ok {
		return os.ErrNotExist
	}
	if _, occupied := f.files[newKey]; occupied {
		return os.ErrExist
	}
	delete(f.files, oldKey)
	f.files[newKey] = entry
	return nil
}

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

// w17BProbeFile carries the created probe's identity the wave-38 way: Stat
// answers the same fake FileInfo the fake filesystem's stat op later answers
// for the created name (a shared struct value models the shared inode).
type w17BProbeFile struct {
	info os.FileInfo
}

func (w17BProbeFile) Close() error { return nil }

func (f w17BProbeFile) Stat() (os.FileInfo, error) { return f.info, nil }

type w17BProbeInfo struct{ path string }

func (i w17BProbeInfo) Name() string     { return filepath.Base(i.path) }
func (w17BProbeInfo) Size() int64        { return 0 }
func (w17BProbeInfo) Mode() os.FileMode  { return 0o600 }
func (w17BProbeInfo) ModTime() time.Time { return time.Time{} }
func (w17BProbeInfo) IsDir() bool        { return false }
func (w17BProbeInfo) Sys() any           { return nil }
